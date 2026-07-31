// Package metrics is the single definition of what rig-exporter measures.
//
// Every exporter renders from the same readings: MQTT discovery, the JSON
// endpoint, the Prometheus exposition and the InfluxDB line protocol. A
// measurement is described once, in definitions.go, and appears in all four.
//
// Readings carry an instance, so a definition like "disk used" applies to C:,
// D: and every other volume without needing four copies of the definition.
package metrics

import (
	"math"
	"sort"
	"strings"

	"github.com/corgan/rig-exporter/internal/i18n"
)

// Kind decides how a reading is rendered in formats that distinguish types.
type Kind int

const (
	// KindGauge is a number that can go up and down.
	KindGauge Kind = iota
	// KindText is a string: an info metric in Prometheus, a tag in InfluxDB.
	KindText
	// KindBool is a flag, rendered as 0/1, true/false or ON/OFF.
	KindBool
)

// Group is the sensor family a definition belongs to. Groups are what the
// user switches on and off, and what InfluxDB splits into measurements.
type Group string

const (
	// GroupCore is always collected: FPS, game, display, CPU and RAM load.
	GroupCore Group = "core"
	// GroupGPU covers graphics card telemetry.
	GroupGPU Group = "gpu"
	// GroupCPU covers processor detail beyond the overall load in GroupCore.
	GroupCPU Group = "cpu"
	// GroupRAM covers memory: how much is installed, how fast, how full.
	GroupRAM Group = "ram"
	// GroupDisk covers volumes and their throughput.
	GroupDisk Group = "disk"
	// GroupNet covers network adapters and the latency probe.
	GroupNet Group = "net"
)

// Groups lists every group in presentation order.
var Groups = []Group{GroupCore, GroupGPU, GroupCPU, GroupRAM, GroupDisk, GroupNet}

// groupLabels names the groups for the settings page, the tray and -probe.
var groupLabels = map[Group]i18n.Text{
	GroupCore: {DE: "FPS & System", EN: "FPS & system"},
	GroupGPU:  {DE: "Grafikkarte", EN: "Graphics card"},
	GroupCPU:  {DE: "Prozessor", EN: "Processor"},
	GroupRAM:  {DE: "Arbeitsspeicher", EN: "Memory"},
	GroupDisk: {DE: "Laufwerke", EN: "Drives"},
	GroupNet:  {DE: "Netzwerk", EN: "Network"},
}

// Label is the group's name in the given language.
func (g Group) Label(lang i18n.Lang) string {
	if text, ok := groupLabels[g]; ok {
		return text.In(lang)
	}
	return string(g)
}

// Definition describes one measurement, independent of any output format and
// of the thing being measured.
type Definition struct {
	// ID names the measurement in JSON and MQTT. Together with the instance
	// it forms the entity key, so "cpu" on host corganpc2 becomes
	// sensor.cpu_corganpc2 and "disk_used_percent" for C: becomes
	// sensor.disk_used_percent_c_corganpc2.
	//
	// The id never changes with the language: it is what Home Assistant, MQTT
	// and every dashboard key off, so switching language must not rename a
	// single entity.
	ID string
	// Name is what a person reads, and follows the configured language.
	Name i18n.Text
	// Unit is the display unit, empty for text and boolean measurements.
	Unit string
	Kind Kind
	// Precision is how many decimals the value is rendered with.
	Precision int
	Group     Group
	// InstanceLabel names the dimension for measurements that repeat, e.g.
	// "gpu", "disk", "nic", "core". Empty when there is only ever one.
	InstanceLabel string

	// Prom is the fully qualified Prometheus metric name.
	Prom string
	// PromLabel carries the value for KindText, which Prometheus cannot
	// express as a sample value.
	PromLabel string
	Help      string

	// Home Assistant presentation.
	DeviceClass    string
	StateClass     string
	EntityCategory string
	Icon           string
	// NoEntity keeps a measurement out of Home Assistant while still
	// publishing it in JSON, Prometheus and InfluxDB. Used for values that
	// are useful in a dashboard query but would only clutter an entity list.
	NoEntity bool
}

// Component is the Home Assistant platform this definition is discovered as.
func (d Definition) Component() string {
	if d.Kind == KindBool {
		return "binary_sensor"
	}
	return "sensor"
}

// Reading is one definition applied to one subject.
//
// Exactly one of Number, Text and Bool is meaningful, selected by Def.Kind.
type Reading struct {
	Def Definition
	// Instance identifies which GPU, volume, adapter or core this is, and is
	// empty for measurements that exist only once.
	Instance string

	Number float64
	Text   string
	Bool   bool
}

// Gauge builds a numeric reading, rounded to the definition's precision.
func Gauge(def Definition, instance string, value float64) Reading {
	return Reading{Def: def, Instance: instance, Number: Round(value, def.Precision)}
}

// Text builds a string reading.
func Text(def Definition, instance, value string) Reading {
	return Reading{Def: def, Instance: instance, Text: value}
}

// Bool builds a flag reading.
func Bool(def Definition, instance string, value bool) Reading {
	return Reading{Def: def, Instance: instance, Bool: value}
}

// Key is how this reading is named in JSON and in Home Assistant entity ids.
func (r Reading) Key() string {
	if r.Instance == "" {
		return r.Def.ID
	}
	return r.Def.ID + "_" + Slug(r.Instance)
}

// DisplayName is the Home Assistant entity name, which the device name is
// prefixed to by Home Assistant itself.
func (r Reading) DisplayName(lang i18n.Lang) string {
	name := r.Def.Name.In(lang)
	if r.Instance == "" {
		return name
	}
	return name + " " + r.Instance
}

// Value returns the reading in the form the JSON document uses.
func (r Reading) Value() any {
	switch r.Def.Kind {
	case KindText:
		return r.Text
	case KindBool:
		if r.Bool {
			return PayloadOn
		}
		return PayloadOff
	default:
		return r.Number
	}
}

// Payload values for boolean readings, chosen to match what Home Assistant's
// binary_sensor expects by default.
const (
	PayloadOn  = "ON"
	PayloadOff = "OFF"
)

// Set is one complete collection pass.
type Set struct {
	Readings []Reading
}

// Add appends readings, skipping any whose definition is empty. That lets a
// source build a list without checking every optional value at the call site.
func (s *Set) Add(readings ...Reading) {
	for _, r := range readings {
		if r.Def.ID == "" {
			continue
		}
		s.Readings = append(s.Readings, r)
	}
}

// Find returns the reading for a definition and instance.
func (s Set) Find(id, instance string) (Reading, bool) {
	for _, r := range s.Readings {
		if r.Def.ID == id && r.Instance == instance {
			return r, true
		}
	}
	return Reading{}, false
}

// Number returns a singleton numeric reading, or 0 if it was not collected.
func (s Set) Number(id string) float64 {
	r, _ := s.Find(id, "")
	return r.Number
}

// Str returns a singleton text reading, or "" if it was not collected.
func (s Set) Str(id string) string {
	r, _ := s.Find(id, "")
	return r.Text
}

// Flag returns a singleton boolean reading, or false if it was not collected.
func (s Set) Flag(id string) bool {
	r, _ := s.Find(id, "")
	return r.Bool
}

// Has reports whether a measurement was collected at all, which is how the
// exporters tell "the source is missing" from "the value happens to be zero".
func (s Set) Has(id string) bool {
	for _, r := range s.Readings {
		if r.Def.ID == id {
			return true
		}
	}
	return false
}

// Entities returns the readings that should become Home Assistant entities,
// in a stable order so discovery does not churn between collections.
func (s Set) Entities() []Reading {
	out := make([]Reading, 0, len(s.Readings))
	for _, r := range s.Readings {
		if !r.Def.NoEntity {
			out = append(out, r)
		}
	}
	sortReadings(out)
	return out
}

// GroupInstances lists the instances present in one group, e.g. the drive
// letters that were found.
func (s Set) GroupInstances(group Group) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range s.Readings {
		if r.Def.Group != group || r.Instance == "" || seen[r.Instance] {
			continue
		}
		seen[r.Instance] = true
		out = append(out, r.Instance)
	}
	sort.Strings(out)
	return out
}

// HasGroup reports whether any reading from a group was collected.
func (s Set) HasGroup(group Group) bool {
	for _, r := range s.Readings {
		if r.Def.Group == group {
			return true
		}
	}
	return false
}

// sortReadings orders readings by group, then definition, then instance, so
// every render of the same data produces the same bytes.
func sortReadings(readings []Reading) {
	groupOrder := map[Group]int{}
	for i, g := range Groups {
		groupOrder[g] = i
	}
	sort.SliceStable(readings, func(i, j int) bool {
		a, b := readings[i], readings[j]
		if a.Def.Group != b.Def.Group {
			return groupOrder[a.Def.Group] < groupOrder[b.Def.Group]
		}
		if a.Def.ID != b.Def.ID {
			return a.Def.ID < b.Def.ID
		}
		return a.Instance < b.Instance
	})
}

// Round drops the decimals a definition does not claim to measure. Values that
// are not finite become 0, because a NaN would break every output format.
func Round(v float64, decimals int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	factor := math.Pow(10, float64(decimals))
	return math.Round(v*factor) / factor
}

// Slug reduces an instance name to what is safe in MQTT topics, Home
// Assistant entity ids and Prometheus label values, e.g. "C:" becomes "c" and
// "Ethernet 2" becomes "ethernet_2".
func Slug(s string) string {
	var b strings.Builder
	lastUnderscore := true // suppresses a leading underscore
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
