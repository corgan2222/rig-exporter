package metrics

import "sync/atomic"

// Two sensor sets, because not everybody wants a hundred and thirty entities.
//
// The standard set is what somebody watching their machine actually looks at:
// how hot it is, how busy, how full, how fast. The extended set adds everything
// that answers "how is this machine built" or "what exactly happened in that
// one second" — clock rates, module inventories, per-thread load, the state of
// RTSS itself.
//
// The split is a curated list rather than a rule over the definitions, because
// no rule gets it right. Diagnostic-versus-primary is about where Home
// Assistant files an entity, not about whether anybody wants it: the drive
// label is diagnostic and belongs in the standard set, while the observed clock
// peak is primary and does not.
//
// standardSet and everything else must together be exactly the catalogue —
// TestTheTwoSensorSetsPartitionTheCatalogue holds that.
var standardSet = map[string]bool{
	"cpu":               true,
	"cpu_load_1":        true,
	"cpu_load_5":        true,
	"cpu_load_15":       true,
	"cpu_model":         true,
	"cpu_power":         true,
	"cpu_temperature":   true,
	"cpu_vendor":        true,
	"disk_busy":         true,
	"disk_free":         true,
	"disk_free_percent": true,
	"disk_label":        true,
	"disk_read":         true,
	"disk_temperature":  true,
	"disk_total":        true,
	"disk_used":         true,
	"disk_used_percent": true,
	"disk_write":        true,
	"fps":               true,
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
	// standard set's question. The copy engine is the exception: it moves
	// data around and interests somebody chasing a stutter, not somebody
	// glancing at a dashboard.
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
	// The two self-usage figures are in the standard set because their own
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
	// answers "how is this machine built", which is the extended set's
	// question — and on a desktop none of it exists either way.
	"battery":           true,
	"battery_ac":        true,
	"battery_charging":  true,
	"battery_power":     true,
	"battery_remaining": true,
	"battery_runtime":   true,
}

// standardOnly drops everything outside the standard set at collection time.
//
// Package state for the same reason as decimals: the alternative is threading a
// configuration through a dozen hardware sources that have no other reason to
// know it exists. Atomic because the settings page writes it while the
// collector goroutine reads it.
var standardOnly atomic.Bool

// SetStandardOnly restricts collection to the standard set, or lifts the
// restriction. Off — the extended set — is the default.
func SetStandardOnly(on bool) { standardOnly.Store(on) }

// StandardOnly reports whether collection is currently restricted.
func StandardOnly() bool { return standardOnly.Load() }

// InStandardSet reports whether a measurement survives the restriction.
func (d Definition) InStandardSet() bool { return standardSet[d.ID] }

// StandardDefinitions and ExtendedDefinitions list the two sets in catalogue
// order, for the settings page to show. Extended means "the ones the extended
// set adds", not "all of them" — the page puts the two lists side by side, and
// repeating the standard set in both would say nothing.
func StandardDefinitions() []Definition { return definitionsIn(true) }

// ExtendedDefinitions lists what the extended set adds to the standard one.
func ExtendedDefinitions() []Definition { return definitionsIn(false) }

func definitionsIn(standard bool) []Definition {
	out := make([]Definition, 0, len(All))
	for _, d := range All {
		if d.InStandardSet() == standard {
			out = append(out, d)
		}
	}
	return out
}
