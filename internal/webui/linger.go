//go:build windows

package webui

import (
	"sort"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The dashboard renders whatever the current reading contains, so a reading
// that is only there on some polls takes its row with it. On the machine this
// was written against, one row out of 125 behaved that way: the second
// adapter's copy engine appeared for five or six polls, vanished for five or
// six, and did it again — the WDDM counter instance behind it only exists while
// something is actually using that engine, so the source correctly reports
// nothing and the row correctly disappears. The panel changed height every few
// seconds and the page moved under whoever was reading it.
//
// The fix is to remember a row for a while after its reading stops arriving.
//
// It lives here, on the rendered panels, and not in the collector. A missing
// value is deliberately left out of MQTT, JSON, Prometheus and InfluxDB rather
// than published as a stale one or as a zero; that is a documented invariant
// with tests behind it. Holding a value on screen is a display decision and has
// to stay one, so this works on the finished []groupStatus and never touches
// the snapshot the exporters are handed.

// lingerPolls is how many polls a row survives without a reading.
//
// A count of polls rather than a number of seconds, so that slowing the poll
// rate down stretches the window with it instead of dropping rows between two
// readings.
//
// Twenty is set against the measurement: the flicker that prompted this had
// gaps of five to six seconds, which at the default 500 ms poll is ten to
// twelve polls. Twenty leaves room above the worst gap seen without keeping a
// genuinely departed device on screen longer than the ten seconds it takes
// somebody to notice it should be gone.
const lingerPolls = 20

// lingeredRow is one remembered row and when its reading last arrived.
type lingeredRow struct {
	row  row
	seen time.Time
}

// lingerStore remembers each panel's rows between polls.
//
// Keyed by group and then by the reading's own key, which is the identifier the
// exporters use and therefore the one thing about a row that does not change
// when the display language does. Keying on the label would lose every row the
// moment somebody switched to German.
type lingerStore struct {
	mu     sync.Mutex
	groups map[string]map[string]lingeredRow
}

func newLingerStore() *lingerStore {
	return &lingerStore{groups: map[string]map[string]lingeredRow{}}
}

// keep fills the gaps in one poll's panels from what the previous polls held.
//
// now is the time of the reading rather than of the request, and interval is
// the poll interval in force. Between them they make the window a count of
// polls that is evaluated against the collector's clock, not the browser's: two
// people watching the same dashboard, or nobody watching it for an hour, must
// not change when a row expires.
func (l *lingerStore) keep(groups []groupStatus, now time.Time, interval time.Duration) []groupStatus {
	if interval <= 0 {
		return groups
	}
	window := time.Duration(lingerPolls) * interval

	l.mu.Lock()
	defer l.mu.Unlock()

	for i, group := range groups {
		// Switching a sensor group off, or a source falling over, is an
		// instruction or an announcement — not a missed reading. Either way the
		// panel says so itself, and rows lingering underneath that message
		// would contradict it. So the memory goes and the panel empties at
		// once.
		if !group.Enabled || !group.Available {
			delete(l.groups, group.Key)
			continue
		}

		remembered, ok := l.groups[group.Key]
		if !ok {
			remembered = map[string]lingeredRow{}
			l.groups[group.Key] = remembered
		}

		present := make(map[string]bool, len(group.Rows))
		for _, r := range group.Rows {
			present[r.key] = true
			remembered[r.key] = lingeredRow{row: r, seen: now}
		}

		rows := group.Rows
		for key, held := range remembered {
			if present[key] {
				continue
			}
			if now.Sub(held.seen) > window {
				delete(remembered, key)
				continue
			}
			// The last value that was actually measured, flagged so the page
			// can dim it. Showing a dash instead would only move the flicker
			// from the row to the value; showing the number unflagged would
			// present a reading nobody is taking as if it were live.
			stale := held.row
			stale.Stale = true
			rows = append(rows, stale)
		}

		groups[i].Rows = sortRows(rows)
	}

	// Panels that stopped being sent at all — a battery that is no longer
	// reported — must not keep their memory for ever.
	live := make(map[string]bool, len(groups))
	for _, group := range groups {
		live[group.Key] = true
	}
	for key := range l.groups {
		if !live[key] {
			delete(l.groups, key)
		}
	}

	return groups
}

// sortRows puts the rows back in the order rowsFor produces.
//
// A returning row has to land in its own place. Appending it to the end would
// move every row below it and simply relocate the defect: the panel would keep
// its height and shuffle its contents instead.
//
// The order is the one rowsFor arrives at — by instance, and within an instance
// by measurement id, which is how Set.Entities already sorted them before
// rowsFor grouped by instance.
func sortRows(rows []row) []row {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Instance != rows[j].Instance {
			return metrics.LessInstance(rows[i].Instance, rows[j].Instance)
		}
		return rows[i].defID < rows[j].defID
	})
	return rows
}
