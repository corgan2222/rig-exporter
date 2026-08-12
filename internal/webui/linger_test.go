//go:build windows

package webui

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The poll interval these tests reason in. Every duration below is a multiple
// of it, because the window is counted in polls rather than in seconds.
const testPoll = 500 * time.Millisecond

// gpuGroup is one enabled, available graphics panel carrying the given rows.
func gpuGroup(rows ...row) groupStatus {
	return groupStatus{
		Key:       string(metrics.GroupGPU),
		Label:     "Graphics card",
		Enabled:   true,
		Available: true,
		Rows:      rows,
	}
}

// reading is one display row, identified the way the store identifies it.
func reading(defID, instance, value string) row {
	return row{
		key:      defID + "_" + instance,
		defID:    defID,
		Label:    defID + " " + instance,
		Short:    defID,
		Instance: instance,
		Value:    value,
	}
}

// labels lists a group's rows, with a marker on the ones flagged stale, so a
// failure message shows the order and the flags at once.
func labels(g groupStatus) []string {
	out := make([]string, 0, len(g.Rows))
	for _, r := range g.Rows {
		text := r.Label + "=" + r.Value
		if r.Stale {
			text += " (stale)"
		}
		out = append(out, text)
	}
	return out
}

func find(t *testing.T, groups []groupStatus, key string) groupStatus {
	t.Helper()
	for _, g := range groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("group %q is missing from %+v", key, groups)
	return groupStatus{}
}

// This is the measured defect. On the machine this was written against, one row
// out of 125 — the second adapter's copy engine — came and went on a five to six
// second cycle, because the WDDM counter instance behind it only exists while
// something is using that engine. The panel changed height every few seconds and
// the page moved under the reader.
//
// A row that was there one tick ago has to still be there this tick.
func TestARowThatMissesOneTickStaysOnThePage(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	full := gpuGroup(
		reading("gpu_engine_3d", "1", "4 %"),
		reading("gpu_engine_copy", "1", "0 %"),
		reading("gpu_temperature", "1", "51 °C"),
	)
	// The same panel one poll later, with the copy engine simply not reported.
	without := gpuGroup(
		reading("gpu_engine_3d", "1", "7 %"),
		reading("gpu_temperature", "1", "52 °C"),
	)

	store.keep([]groupStatus{full}, at, testPoll)
	got := find(t, store.keep([]groupStatus{without}, at.Add(testPoll), testPoll), string(metrics.GroupGPU))

	if len(got.Rows) != 3 {
		t.Fatalf("a single missed tick changed the panel height: %v", labels(got))
	}

	// In its own place, not appended to the end — a row that reappears at the
	// bottom moves everything below it, which is the same defect one level down.
	want := []string{"gpu_engine_3d 1=7 %", "gpu_engine_copy 1=0 % (stale)", "gpu_temperature 1=52 °C"}
	for i, w := range want {
		if labels(got)[i] != w {
			t.Errorf("row %d = %q, want %q\nwhole panel: %v", i, labels(got)[i], w, labels(got))
		}
	}
}

// What the lingering row shows is the reading that was last actually taken,
// flagged as no longer live. A stale number presented as current is its own kind
// of wrong, and a dash would make the value column flicker instead of the row.
func TestALingeringRowKeepsItsLastValueAndSaysItIsStale(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	store.keep([]groupStatus{gpuGroup(reading("gpu_engine_copy", "1", "37 %"))}, at, testPoll)
	got := find(t, store.keep([]groupStatus{gpuGroup()}, at.Add(testPoll), testPoll), string(metrics.GroupGPU))

	if len(got.Rows) != 1 {
		t.Fatalf("the row did not linger at all: %v", labels(got))
	}
	if got.Rows[0].Value != "37 %" {
		t.Errorf("value = %q, want the last reading %q", got.Rows[0].Value, "37 %")
	}
	if !got.Rows[0].Stale {
		t.Error("a value that is no longer being measured is presented as if it were live")
	}
}

// A row that comes back is live again, not stale for ever.
func TestAReturningRowStopsBeingStale(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	store.keep([]groupStatus{gpuGroup(reading("gpu_engine_copy", "1", "1 %"))}, at, testPoll)
	store.keep([]groupStatus{gpuGroup()}, at.Add(testPoll), testPoll)
	got := find(t, store.keep(
		[]groupStatus{gpuGroup(reading("gpu_engine_copy", "1", "9 %"))},
		at.Add(2*testPoll), testPoll), string(metrics.GroupGPU))

	if len(got.Rows) != 1 {
		t.Fatalf("panel = %v", labels(got))
	}
	if got.Rows[0].Stale || got.Rows[0].Value != "9 %" {
		t.Errorf("a row that is being measured again is still flagged stale: %v", labels(got))
	}
}

// Lingering is not for ever. A device that is genuinely gone has to leave, and
// the window is counted in polls so that changing the poll rate moves it.
func TestARowThatStaysAwayIsDroppedAfterTheWindow(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	store.keep([]groupStatus{gpuGroup(reading("gpu_engine_copy", "1", "0 %"))}, at, testPoll)

	// Still there one poll short of the window.
	inside := at.Add(time.Duration(lingerPolls) * testPoll)
	if got := find(t, store.keep([]groupStatus{gpuGroup()}, inside, testPoll), string(metrics.GroupGPU)); len(got.Rows) != 1 {
		t.Fatalf("the row went before its window was up: %v", labels(got))
	}

	// Gone once past it.
	outside := at.Add(time.Duration(lingerPolls+1) * testPoll)
	if got := find(t, store.keep([]groupStatus{gpuGroup()}, outside, testPoll), string(metrics.GroupGPU)); len(got.Rows) != 0 {
		t.Errorf("the row outstayed its window: %v", labels(got))
	}
}

// The window is a number of polls, not a fixed number of seconds: somebody who
// slows the poll rate down wants the row to linger for the same number of
// readings, not to vanish between two of them.
func TestTheWindowFollowsThePollRate(t *testing.T) {
	store := newLingerStore()
	at := time.Now()
	slow := 4 * time.Second

	store.keep([]groupStatus{gpuGroup(reading("gpu_engine_copy", "1", "0 %"))}, at, slow)

	// A gap that would be far past the window at the default rate is only two
	// polls at this one, so the row is still there.
	got := find(t, store.keep([]groupStatus{gpuGroup()}, at.Add(2*slow), slow), string(metrics.GroupGPU))
	if len(got.Rows) != 1 {
		t.Errorf("two polls at a slow rate dropped the row: %v", labels(got))
	}
}

// Switching a sensor group off is an instruction, not a missed reading. The
// panel has to empty at once — a user who unticks the graphics card and watches
// its readings hang around for another ten seconds is looking at a bug.
func TestSwitchingAGroupOffDropsItsRowsAtOnce(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	store.keep([]groupStatus{gpuGroup(
		reading("gpu_engine_copy", "1", "0 %"),
		reading("gpu_temperature", "1", "51 °C"),
	)}, at, testPoll)

	off := groupStatus{Key: string(metrics.GroupGPU), Label: "Graphics card", Enabled: false}
	got := find(t, store.keep([]groupStatus{off}, at.Add(testPoll), testPoll), string(metrics.GroupGPU))
	if len(got.Rows) != 0 {
		t.Fatalf("a switched-off group kept its rows: %v", labels(got))
	}

	// And the memory went with it: switching back on must not resurrect the old
	// readings alongside the new ones.
	back := find(t, store.keep([]groupStatus{gpuGroup(reading("gpu_temperature", "1", "49 °C"))},
		at.Add(2*testPoll), testPoll), string(metrics.GroupGPU))
	if len(back.Rows) != 1 {
		t.Errorf("switching the group back on resurrected stale rows: %v", labels(back))
	}
}

// A whole source falling over is reported by the panel itself, which then says
// what went wrong. Leaving the old rows underneath that message would contradict
// it.
func TestAGroupThatGoesUnavailableDropsItsRowsAtOnce(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	store.keep([]groupStatus{gpuGroup(reading("gpu_temperature", "1", "51 °C"))}, at, testPoll)

	broken := groupStatus{
		Key: string(metrics.GroupGPU), Label: "Graphics card",
		Enabled: true, Available: false, Error: "the driver would not answer",
	}
	got := find(t, store.keep([]groupStatus{broken}, at.Add(testPoll), testPoll), string(metrics.GroupGPU))
	if len(got.Rows) != 0 {
		t.Errorf("a failed group kept its rows under the failure message: %v", labels(got))
	}
}

// The store must not invent rows for a group it has never seen, and must not
// leak one group's rows into another.
func TestRowsDoNotCrossBetweenGroups(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	disk := groupStatus{Key: string(metrics.GroupDisk), Label: "Drives", Enabled: true, Available: true}
	store.keep([]groupStatus{gpuGroup(reading("gpu_temperature", "1", "51 °C")), disk}, at, testPoll)

	out := store.keep([]groupStatus{gpuGroup(), disk}, at.Add(testPoll), testPoll)
	if got := find(t, out, string(metrics.GroupDisk)); len(got.Rows) != 0 {
		t.Errorf("the drives panel grew a graphics row: %v", labels(got))
	}
}

// The page decides how to dim a row from what arrives on the wire, so the flag
// has to be there — and the two fields that identify a row internally must not
// be, one of them being an export identifier with no business in the browser.
func TestTheStaleFlagIsOnTheWireAndTheKeysAreNot(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	store.keep([]groupStatus{gpuGroup(reading("gpu_engine_copy", "1", "0 %"))}, at, testPoll)
	panels := store.keep([]groupStatus{gpuGroup()}, at.Add(testPoll), testPoll)

	// The rows on their own: the group's own "key" belongs on the wire, and
	// looking at the whole payload would confuse the two.
	encoded, err := json.Marshal(find(t, panels, string(metrics.GroupGPU)).Rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(encoded)

	if !strings.Contains(body, `"stale":true`) {
		t.Errorf("the page cannot tell the row is stale: %s", body)
	}
	// The row's own identifiers stay behind: defID is an internal ordering aid,
	// and key is an export identifier with no business in the browser.
	for _, leak := range []string{"gpu_engine_copy_1", `"key"`, `"defID"`} {
		if strings.Contains(body, leak) {
			t.Errorf("%s reached the browser: %s", leak, body)
		}
	}
}

// The server can flag a row all it likes; the page has to do something visible
// with it. Rendered in both languages, because the explanation is a translated
// string and a missing one would show up as an empty tooltip rather than as an
// error.
func TestThePageCanShowAStaleRow(t *testing.T) {
	for _, language := range i18n.Available {
		tmpl, err := template.ParseFS(templateFS, "templates/layout.html", "templates/status.html")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "layout", pageData{Lang: language.Code}); err != nil {
			t.Fatalf("render in %s: %v", language.Code, err)
		}
		page := buf.String()

		// The rule that dims it, and the script that applies it.
		if !strings.Contains(page, "dd.stale") {
			t.Errorf("%s: nothing styles a stale row", language.Code)
		}
		if !strings.Contains(page, "r.stale") {
			t.Errorf("%s: the script ignores the flag", language.Code)
		}
		// And the reason, in this language.
		if want := i18n.T(language.Code, "hardware.stale"); !strings.Contains(page, want) {
			t.Errorf("%s: the page does not carry %q", language.Code, want)
		}
	}
}

// The pattern this was measured against, replayed.
//
// Polling the running exporter's /api/status once a second for a minute and
// diffing the row sets gave this presence trace for "GPU 1 Copy engine", the one
// row of 125 that moved: five seconds present, five absent, five present, six
// absent, sixteen present, five absent, six present, five absent, five present.
// The panel's height followed it.
//
// The default poll interval is 500 ms, so each of those seconds is two polls and
// the longest gap is twelve — inside the twenty-poll window, which is how the
// window was sized. Replaying the trace, the row count must never change.
func TestTheMeasuredFlickerNoLongerChangesThePanelHeight(t *testing.T) {
	// One character per second of the measured trace: 1 = the reading arrived.
	const trace = "11111.....11111......1111111111111111.....111111.....11111"

	store := newLingerStore()
	at := time.Now()
	heights := map[int]bool{}

	for second, mark := range trace {
		rows := []row{
			reading("gpu_engine_3d", "1", "4 %"),
			reading("gpu_temperature", "1", "51 °C"),
		}
		if mark == '1' {
			rows = append(rows, reading("gpu_engine_copy", "1", "0 %"))
		}
		// A second of wall clock is two polls at the default rate.
		now := at.Add(time.Duration(second) * time.Second)
		got := find(t, store.keep([]groupStatus{gpuGroup(rows...)}, now, testPoll), string(metrics.GroupGPU))
		heights[len(got.Rows)] = true
	}

	if len(heights) != 1 {
		t.Errorf("the panel still changes height: row counts seen = %v", heights)
	}
	if !heights[3] {
		t.Errorf("row counts seen = %v, want only 3", heights)
	}
}

// Two browsers on the same dashboard share one store, and the handler holds no
// other lock. Worth a test of its own because it is the one thing here that is
// not obviously safe, and because -Race only proves what the tests exercised.
func TestTheStoreSurvivesTwoDashboardsAtOnce(t *testing.T) {
	store := newLingerStore()
	at := time.Now()

	done := make(chan struct{})
	for viewer := 0; viewer < 4; viewer++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				rows := []row{reading("gpu_temperature", "1", "51 °C")}
				if i%2 == 0 {
					rows = append(rows, reading("gpu_engine_copy", "1", "0 %"))
				}
				store.keep([]groupStatus{gpuGroup(rows...)}, at.Add(time.Duration(i)*testPoll), testPoll)
			}
		}()
	}
	for viewer := 0; viewer < 4; viewer++ {
		<-done
	}
}

// A store nobody asks is worth nothing. Everything above tests the store on its
// own, and all of it stays green if the single call in statusFor goes — measured
// by taking that call out, at which point the whole package still passed while
// the dashboard flickered exactly as before. So this one walks the path the page
// walks: two readings through the payload builder, the second one missing a row.
func TestTheDashboardPayloadIsBuiltThroughTheLinger(t *testing.T) {
	server, _ := newServer(t, func(cfg *config.Config) { cfg.GPUEnabled = true })
	cfg := server.app.Config()

	withCopy := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}
	withCopy.Set.Add(metrics.Gauge(metrics.GPUEngineCopy, "1", 0))
	withCopy.Set.Add(metrics.Gauge(metrics.GPUTemperature, "1", 51))

	withoutCopy := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}
	withoutCopy.Set.Add(metrics.Gauge(metrics.GPUTemperature, "1", 52))

	at := time.Now()
	server.statusFor(app.Status{Config: cfg, Snapshot: withCopy, UpdatedAt: at})
	resp := server.statusFor(app.Status{
		Config: cfg, Snapshot: withoutCopy, UpdatedAt: at.Add(testPoll),
	})

	panel := find(t, resp.Groups, string(metrics.GroupGPU))
	if len(panel.Rows) != 2 {
		t.Fatalf("the payload the page polls for lost the row: %v", labels(panel))
	}
	var held row
	for _, r := range panel.Rows {
		if r.Stale {
			held = r
		}
	}
	if held.Value == "" {
		t.Errorf("no row is flagged stale, so the linger is not in this path: %v", labels(panel))
	}
}

// The linger is a display device and nothing else. It is handed the rendered
// panel, never the snapshot, so there is no path from here to MQTT, JSON,
// Prometheus or InfluxDB — a missing value is left out of those deliberately,
// and this must not turn it into a stale one.
//
// The test walks the real path: build the panels from a snapshot, drop a reading
// from the next snapshot, and check that the panel keeps the row while the
// snapshot the exporters see does not.
func TestTheLingerNeverReachesTheExportedReadings(t *testing.T) {
	cfg := config.Defaults()
	cfg.GPUEnabled = true

	withCopy := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}
	withCopy.Set.Add(metrics.Gauge(metrics.GPUEngineCopy, "1", 0))
	withCopy.Set.Add(metrics.Gauge(metrics.GPUTemperature, "1", 51))

	withoutCopy := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}
	withoutCopy.Set.Add(metrics.Gauge(metrics.GPUTemperature, "1", 52))

	store := newLingerStore()
	at := time.Now()

	first := app.Status{Config: cfg, Snapshot: withCopy, UpdatedAt: at}
	store.keep(groupStatuses(first, i18n.EN), at, testPoll)

	second := app.Status{Config: cfg, Snapshot: withoutCopy, UpdatedAt: at.Add(testPoll)}
	panels := store.keep(groupStatuses(second, i18n.EN), second.UpdatedAt, testPoll)

	// The panel kept it.
	if got := find(t, panels, string(metrics.GroupGPU)); len(got.Rows) != 2 {
		t.Fatalf("the display lost the row: %v", labels(got))
	}

	// The readings the exporters render did not gain it. This is the invariant:
	// a value that was not measured is left out, not carried over.
	for _, r := range withoutCopy.Entities() {
		if r.Def.ID == metrics.GPUEngineCopy.ID {
			t.Fatal("the linger wrote a stale reading back into the snapshot the exporters publish")
		}
	}
	if _, found := withoutCopy.Set.Find(metrics.GPUEngineCopy.ID, "1"); found {
		t.Fatal("the linger added the missing reading to the exported set")
	}
}
