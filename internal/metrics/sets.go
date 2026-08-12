package metrics

import (
	"sync/atomic"
)

// Preset is one rung of a ladder with three of them, because "everything" and
// "not everything" was never enough of a choice.
//
// The rungs are curated lists rather than a rule over the definitions, because
// no rule gets it right. Diagnostic-versus-primary is about where Home
// Assistant files an entity, not about whether anybody wants it: the drive
// label is diagnostic and belongs in the basic rung, while the observed clock
// peak is primary and does not.
//
// They nest: Minimal ⊂ Basic ⊂ Extended, held by
// TestTheRungsOfTheLadderNest. A slider that could take something away by
// moving up would not be a slider.
type Preset string

const (
	// PresetMinimal is what the dashboard tiles show, and nothing else: how
	// busy, how hot, how full, how fast. Sixteen measurements for somebody who
	// wants a machine on a wall panel rather than a diagnosis.
	PresetMinimal Preset = "minimal"
	// PresetBasic is what somebody watching their machine actually looks at.
	PresetBasic Preset = "basic"
	// PresetExtended is the whole catalogue: inventories, clock rates, per
	// thread load, wear. Useful while hunting a problem, rarely otherwise.
	PresetExtended Preset = "extended"
)

// Presets lists the rungs from fewest measurements to most, which is the
// order a slider has to offer them in.
var Presets = []Preset{PresetMinimal, PresetBasic, PresetExtended}

// Valid reports whether a preset is one this program knows.
func (p Preset) Valid() bool {
	for _, known := range Presets {
		if p == known {
			return true
		}
	}
	return false
}

// minimalSet is the wall-panel answer: the six tiles at the top of the
// dashboard, the two temperatures somebody actually worries about, how full
// the machine is, what the network is doing, and the battery on a laptop.
//
// Deliberately without uptime and the process count. Both change on every
// single reading, which makes them the two cheapest ways to fill a database
// with nothing.
var minimalSet = map[string]bool{
	"fps":                true,
	"frametime":          true,
	"game":               true,
	"game_running":       true,
	"cpu":                true,
	"cpu_temperature":    true,
	"ram":                true,
	"gpu_load":           true,
	"gpu_temperature":    true,
	"disk_overall_usage": true,
	"disk_overall_free":  true,
	"net_rx":             true,
	"net_tx":             true,
	"battery":            true,
	"battery_ac":         true,
	// Which build produced all of the above. It changes once per update and
	// answers the first question of every bug report.
	"version": true,
}

// basicSet was the standard set, and is unchanged.
var basicSet = map[string]bool{
	"cpu":             true,
	"cpu_load_1":      true,
	"cpu_load_5":      true,
	"cpu_load_15":     true,
	"cpu_model":       true,
	"cpu_power":       true,
	"cpu_temperature": true,
	"cpu_vendor":      true,
	// The cooling loop, when there is one. The liquid temperature and the two
	// speeds are the three figures somebody watches; the duty cycles the
	// controller is driving are diagnosis and stay in the extended rung.
	"cooling_device":             true,
	"cooling_liquid_temperature": true,
	"cooling_pump_speed":         true,
	"cooling_fan_speed":          true,
	"disk_busy":                  true,
	"disk_free":                  true,
	"disk_free_percent":          true,
	"disk_label":                 true,
	"disk_read":                  true,
	"disk_temperature":           true,
	"disk_total":                 true,
	"disk_used":                  true,
	"disk_used_percent":          true,
	"disk_write":                 true,
	"fps":                        true,
	// The overall figures answer "how full is this machine", which is the
	// question somebody asks before they ask about any single volume.
	"disk_overall_capacity":      true,
	"disk_overall_free":          true,
	"disk_overall_free_percent":  true,
	"disk_overall_usage":         true,
	"disk_overall_used":          true,
	"frametime":                  true,
	"game":                       true,
	"game_running":               true,
	"gpu_fan":                    true,
	"gpu_fan_rpm":                true,
	"gpu_dedicated_memory_total": true,
	"gpu_load":                   true,
	// The engine breakdown answers "what is the card doing", which is the
	// basic rung's question. The copy engine is the exception: it moves data
	// around and interests somebody chasing a stutter, not somebody glancing
	// at a dashboard.
	"gpu_engine_3d":     true,
	"gpu_engine_decode": true,
	"gpu_engine_encode": true,
	"gpu_memory_used":   true,
	"gpu_name":          true,
	"gpu_power":         true,
	"gpu_power_percent": true,
	"gpu_source":        true,
	"gpu_temperature":   true,
	"gpu_vendor":        true,
	"gpu_voltage":       true,
	"gpu_vram_percent":  true,
	"gpu_vram_total":    true,
	"gpu_vram_used":     true,
	"net_ip":            true,
	"net_link":          true,
	"net_rx":            true,
	"net_tx":            true,
	"net_rx_total":      true,
	"net_tx_total":      true,
	"net_type":          true,
	"net_wifi_signal":   true,
	"os_version":        true,
	"processes":         true,
	"ram":               true,
	"ram_free_mb":       true,
	"ram_free_percent":  true,
	"ram_total_mb":      true,
	"ram_used_mb":       true,
	"uptime":            true,
	"version":           true,
	// The two self-usage figures are in the basic rung because their own
	// sensor group already decides whether they exist at all. Being filtered
	// out here as well would leave somebody who ticked that box with nothing
	// to look at, for a reason they never touched.
	"exporter_cpu":    true,
	"exporter_memory": true,
	// Same reasoning for the two rankings: their own group decides whether
	// they exist, and a second switch hiding them would only confuse.
	"top_cpu":    true,
	"top_memory": true,
	// The live half of the battery: how full, whether it is charging, how
	// long it will last. What the pack is made of and how worn it has become
	// answers "how is this machine built", which is the extended rung's
	// question — and on a desktop none of it exists either way.
	"battery":           true,
	"battery_ac":        true,
	"battery_charging":  true,
	"battery_power":     true,
	"battery_remaining": true,
	"battery_runtime":   true,
}

// PresetContains reports whether a rung carries a measurement. Extended is
// every definition there is, so it is answered by the catalogue rather than by
// a list somebody has to remember to extend.
func PresetContains(preset Preset, id string) bool {
	switch preset {
	case PresetMinimal:
		return minimalSet[id]
	case PresetBasic:
		return basicSet[id]
	case PresetExtended:
		return true
	}
	return false
}

// PresetIDs is everything on a rung, in catalogue order.
func PresetIDs(preset Preset) []string {
	out := make([]string, 0, len(All))
	for _, d := range All {
		if PresetContains(preset, d.ID) {
			out = append(out, d.ID)
		}
	}
	return out
}

// PresetDefinitions is the same, as definitions.
func PresetDefinitions(preset Preset) []Definition {
	out := make([]Definition, 0, len(All))
	for _, d := range All {
		if PresetContains(preset, d.ID) {
			out = append(out, d)
		}
	}
	return out
}

// Resolve turns what the user asked for into the set that is actually
// collected: a rung, plus the individual measurements they added to it or took
// out of it.
//
// The rung is stored rather than the resulting list, and this is the reason: a
// measurement added to the catalogue by a later version joins the rung it
// belongs to on its own. A stored list would have left it switched off, and
// nobody would have gone looking for a value they had never heard of.
//
// Ids that no longer exist are ignored rather than rejected. A configuration
// that mentions a measurement retired two versions ago is not a broken
// configuration, it is an old one.
func Resolve(preset Preset, added, removed []string) map[string]bool {
	if !preset.Valid() {
		preset = PresetExtended
	}

	known := make(map[string]bool, len(All))
	for _, d := range All {
		known[d.ID] = true
	}

	selected := make(map[string]bool, len(All))
	for _, d := range All {
		if PresetContains(preset, d.ID) {
			selected[d.ID] = true
		}
	}
	for _, id := range added {
		if known[id] {
			selected[id] = true
		}
	}
	// Removal last: somebody who both added and removed the same measurement
	// meant the second thing, whatever order the file happens to list them in.
	for _, id := range removed {
		delete(selected, id)
	}
	return selected
}

// selection is what survives collection, and it is package state for the same
// reason as decimals: the alternative is threading a configuration through a
// dozen hardware sources that have no other reason to know it exists. Atomic
// because the settings page writes it while the collector goroutine reads it.
//
// A nil pointer means nothing has been selected yet, which is read as
// everything — a collector running before the configuration was applied should
// report too much rather than nothing.
var selection atomic.Pointer[map[string]bool]

// SetSelection replaces the set of measurements that are collected at all.
func SetSelection(ids map[string]bool) {
	copied := make(map[string]bool, len(ids))
	for id, on := range ids {
		if on {
			copied[id] = true
		}
	}
	selection.Store(&copied)
}

// Selected reports whether a measurement survives the current selection.
func Selected(id string) bool {
	current := selection.Load()
	if current == nil {
		return true
	}
	return (*current)[id]
}

// SelectedCount is how many of the catalogue's measurements are switched on,
// for the interface to show next to the slider.
func SelectedCount() int {
	count := 0
	for _, d := range All {
		if Selected(d.ID) {
			count++
		}
	}
	return count
}
