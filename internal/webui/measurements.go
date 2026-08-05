//go:build windows

package webui

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The two numbers the database estimate cannot measure for itself.
const (
	// bytesPerStateRow is what one row in Home Assistant's states table costs
	// on disk, index included.
	//
	// A stated assumption rather than a measurement, and the page says so. It
	// is the figure a default SQLite recorder lands near; somebody running
	// MariaDB or keeping long attributes will be somewhere else entirely,
	// which is exactly why the number is shown next to the result instead of
	// being buried in it.
	bytesPerStateRow = 300
	// purgeKeepDays is Home Assistant's own default retention. Anybody who
	// changed it can divide.
	purgeKeepDays = 10
)

// measurementRow is one measurement on the selection page.
type measurementRow struct {
	ID   string
	Name string
	Unit string
	// Instances is how many entities this measurement becomes on this machine
	// right now — one per graphics card, per volume, per adapter. It is what
	// makes the estimate about this PC rather than about the catalogue.
	Instances int
	Selected  bool
	// Value is what this measurement reads right now, empty where it is
	// switched off or the machine does not supply it. Shown so somebody
	// deciding whether they want a measurement can see what it says rather
	// than guess from its name.
	Value string
	// Rung is the lowest rung that carries this measurement, as a slider
	// position, so the page can re-tick the whole list while the slider is
	// being dragged instead of asking the server for every pixel. A
	// measurement no rung carries sits above the last position and is never
	// ticked by the slider.
	Rung int
}

// rungOf is the slider position from which a measurement is on.
func rungOf(id string) int {
	for i, preset := range metrics.Presets {
		if metrics.PresetContains(preset, id) {
			return i
		}
	}
	return len(metrics.Presets)
}

// measurementGroup is one node of the tree: a sensor group and its
// measurements, in catalogue order.
type measurementGroup struct {
	Key   string
	Label string
	Rows  []measurementRow
}

// Selected counts the ticked rows, for the heading.
func (g measurementGroup) Selected() int {
	count := 0
	for _, r := range g.Rows {
		if r.Selected {
			count++
		}
	}
	return count
}

// measurementsData is everything the page needs.
type measurementsData struct {
	Groups []measurementGroup
	Preset string
	// Rungs in slider order, with the position of the current one.
	Rungs    []string
	RungAt   int
	Estimate estimateInputs
}

// estimateInputs are the numbers the page multiplies together. They are handed
// over rather than folded into a result, because the result is worth nothing
// without them: a reader has to be able to see which of these is wrong.
type estimateInputs struct {
	// Churn is the measured share of entities whose value actually differs
	// from one publish to the next, and Samples how many publishes that came
	// from. Zero samples means nothing has been measured yet.
	Churn   float64 `json:"churn"`
	Samples int     `json:"samples"`
	// PublishMs is the pace in force right now.
	PublishMs int `json:"publish_ms"`
	// Decimals matters more than anything else here: a temperature to one
	// decimal changes almost every publish, the same one rounded to a whole
	// degree changes a few times an hour.
	Decimals bool `json:"decimals"`
	// Rendering says which of the two paces is in force, so the page knows
	// which field to watch while it is being typed in.
	Rendering    bool    `json:"rendering"`
	BytesPerRow  int     `json:"bytes_per_row"`
	KeepDays     int     `json:"keep_days"`
	FallbackRate float64 `json:"fallback_rate"`
}

// fallbackChurn stands in until the first publishes have been compared. Half
// the entities, which is deliberately a round number nobody will mistake for a
// measurement.
const fallbackChurn = 0.5

// measurementsFor builds the tree, the slider position and the estimate.
func measurementsFor(st app.Status, lang i18n.Lang) measurementsData {
	selected := selectionOf(st.Config)
	instances := instanceCounts(st)
	produced := producedCounts(st)
	values := currentValues(st, lang)

	byGroup := map[metrics.Group][]measurementRow{}
	for _, d := range metrics.All {
		byGroup[d.PanelGroup()] = append(byGroup[d.PanelGroup()], measurementRow{
			ID:        d.ID,
			Name:      d.Name.In(lang),
			Unit:      d.Unit,
			Instances: instancesOf(d, selected[d.ID], produced, instances),
			Selected:  selected[d.ID],
			Value:     values[d.ID],
			Rung:      rungOf(d.ID),
		})
	}

	groups := make([]measurementGroup, 0, len(metrics.Groups))
	for _, group := range metrics.Groups {
		rows := byGroup[group]
		if len(rows) == 0 {
			continue
		}
		groups = append(groups, measurementGroup{
			Key:   string(group),
			Label: group.Label(lang),
			Rows:  rows,
		})
	}

	rungs := make([]string, 0, len(metrics.Presets))
	at := 0
	for i, preset := range metrics.Presets {
		rungs = append(rungs, string(preset))
		if string(preset) == st.Config.Measurements.Preset {
			at = i
		}
	}

	return measurementsData{
		Groups: groups,
		Preset: st.Config.Measurements.Preset,
		Rungs:  rungs,
		RungAt: at,
		Estimate: estimateInputs{
			Churn:        st.Churn,
			Samples:      st.ChurnSamples,
			PublishMs:    publishPace(st),
			Decimals:     st.Config.Decimals,
			Rendering:    st.Snapshot.Rendering(),
			BytesPerRow:  bytesPerStateRow,
			KeepDays:     purgeKeepDays,
			FallbackRate: fallbackChurn,
		},
	}
}

// selectionOf is what the stored configuration currently selects.
func selectionOf(cfg config.Config) map[string]bool {
	return metrics.Resolve(
		metrics.Preset(cfg.Measurements.Preset),
		cfg.Measurements.Added,
		cfg.Measurements.Removed,
	)
}

// instancesOf is how many entities this one measurement becomes here.
//
// Counted from the last reading wherever it can be: a measurement that is
// switched on and still produced nothing is one this machine cannot supply —
// no battery, no kernel driver, no card that reports a hotspot — and it costs
// a database exactly nothing. Two earlier drafts of this page got that wrong in
// both directions, first claiming 226 entities and then 180 where the exporter
// publishes 141.
//
// Only a measurement that is switched off has to be guessed at, and then the
// question is what it would become: one per card, per volume, per adapter, or
// simply one.
func instancesOf(d metrics.Definition, on bool, produced map[string]int, perGroup map[metrics.Group]int) int {
	if on {
		return produced[d.ID]
	}
	if d.InstanceLabel == "" {
		return 1
	}
	return perGroup[d.Group]
}

// currentValues is what each measurement reads right now, rendered the way the
// dashboard renders it.
//
// One measurement can be several entities — a temperature per card, a capacity
// per volume. The first is shown with a note of how many more there are, rather
// than a list: the point is to recognise the measurement, not to read all of
// its instances here.
func currentValues(st app.Status, lang i18n.Lang) map[string]string {
	// Every reading, not only the ones that become entities: a value kept out
	// of Home Assistant still reaches JSON and Prometheus, and somebody
	// deciding whether they want it should see what it says.
	byID := map[string][]metrics.Reading{}
	for _, r := range st.Snapshot.Readings {
		byID[r.Def.ID] = append(byID[r.Def.ID], r)
	}

	out := make(map[string]string, len(byID))
	for id, readings := range byID {
		text := readingText(readings[0])
		if len(readings) > 1 {
			text += fmt.Sprintf(" +%d", len(readings)-1)
		}
		out[id] = text
	}
	return out
}

// maxValueWidth is how much of a value fits in the column before the row stops
// being a list and starts being a paragraph.
const maxValueWidth = 34

// readingText renders one reading for the column on the right.
//
// A ranked list has to go through TableText: its Value is a nested object meant
// for JSON, and printing that with fmt puts "map[apps:[{firefox.exe 3.39} …]]"
// on the page, which is what it did.
func readingText(r metrics.Reading) string {
	var text string
	if r.Def.Kind == metrics.KindTable {
		text = r.TableText()
	} else {
		text = fmt.Sprint(r.Value())
		if r.Def.Unit != "" {
			text += " " + r.Def.Unit
		}
	}

	// Cut on runes rather than bytes: a degree sign is two of the latter.
	if runes := []rune(text); len(runes) > maxValueWidth {
		text = strings.TrimSpace(string(runes[:maxValueWidth-1])) + "…"
	}
	return text
}

// producedCounts is how many entities each measurement actually became in the
// last reading.
func producedCounts(st app.Status) map[string]int {
	counts := map[string]int{}
	for _, r := range st.Snapshot.Entities() {
		counts[r.Def.ID]++
	}
	return counts
}

// instanceCounts is how many entities one measurement of a group becomes on
// this machine: one per card, per volume, per adapter.
//
// Taken from the last reading, so it describes this PC. A group that is
// switched off entirely has none, and counts as one — otherwise ticking a
// measurement would add nothing to the estimate and the page would look broken.
func instanceCounts(st app.Status) map[metrics.Group]int {
	counts := map[metrics.Group]int{}
	for _, group := range metrics.Groups {
		count := len(st.Snapshot.GroupInstances(group))
		if count == 0 {
			count = 1
		}
		counts[group] = count
	}
	return counts
}

func (s *Server) handleMeasurements(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "measurements", "page.measurements")
}

// saveMeasurements turns the ticked boxes back into a rung plus the exceptions
// the user made to it.
//
// The difference is stored rather than the list, so a measurement the catalogue
// gains later joins the rung on its own. That means working out here what the
// user's ticks say about the rung they picked, which is the whole trick: added
// is what they ticked and the rung does not carry, removed is the other way
// round.
func saveMeasurements(cfg *config.Config, r *http.Request) {
	preset := r.FormValue("preset")
	if !metrics.Preset(preset).Valid() {
		preset = config.PresetExtended
	}
	cfg.Measurements.Preset = preset

	ticked := map[string]bool{}
	for _, id := range r.Form["measurement"] {
		ticked[strings.TrimSpace(id)] = true
	}

	var added, removed []string
	for _, d := range metrics.All {
		onRung := metrics.PresetContains(metrics.Preset(preset), d.ID)
		switch {
		case ticked[d.ID] && !onRung:
			added = append(added, d.ID)
		case !ticked[d.ID] && onRung:
			removed = append(removed, d.ID)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	cfg.Measurements.Added = added
	cfg.Measurements.Removed = removed
}

// handleRung moves the slider: it takes the rung on its own, drops every
// exception, and saves.
//
// A separate endpoint from the form because it means something different.
// Sliding to another rung is "forget what I picked and give me this", and
// folding that into the same submit as the ticks would make the two fight over
// which of them is the answer.
func (s *Server) handleRung(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config()
	preset := r.FormValue("preset")
	if !metrics.Preset(preset).Valid() {
		http.Redirect(w, r, "/measurements?error="+neturl.QueryEscape("unknown preset "+preset), http.StatusSeeOther)
		return
	}

	cfg.Measurements.Preset = preset
	cfg.Measurements.Added = nil
	cfg.Measurements.Removed = nil

	err := s.app.ApplyConfig(cfg)
	if err != nil {
		s.log.Error("apply rung", "preset", preset, "error", err)
	}
	respondSave(w, r, "/measurements", "#rung", err)
}
