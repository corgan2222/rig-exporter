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
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

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
	// KindTable is a short ranked list — a name and a number per row.
	//
	// It exists because "the five processes using the most CPU" is one
	// measurement with five rows, not five measurements. Splitting it into five
	// numbered entities would give Home Assistant a series per rank whose
	// meaning changes whenever two programs swap places.
	//
	// Each format renders it in its own idiom: a nested object in JSON, one
	// series per row with a label in Prometheus, a field per rank in InfluxDB,
	// and in Home Assistant the leader as the state with the whole table
	// alongside it as attributes.
	KindTable
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
	// Group decides whether the measurement is collected at all, and which
	// switch turns it off.
	Group Group
	// Panel overrides where the interface shows the measurement, without
	// moving it out of the group that collects it. The overall processor and
	// memory load are core readings — the dashboard tiles need them whatever
	// else is switched off — but belong on the processor and memory panels,
	// which is where a reader looks for them. Publishing them twice under two
	// names would be the alternative, and two Home Assistant entities holding
	// the same number is worse than none.
	Panel Group
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

// PanelGroup is where the interface shows this measurement, which is its own
// group unless it was placed elsewhere.
func (d Definition) PanelGroup() Group {
	if d.Panel != "" {
		return d.Panel
	}
	return d.Group
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
	// Origin names what supplied this number — Windows, MSI Afterburner, NVML,
	// PawnIO. It is for the person looking at the interface, who has a real
	// reason to know which of their programs a reading depends on.
	//
	// It never reaches an export. The same measurement has to look identical
	// whatever produced it, or a dashboard would start depending on which
	// helpers happen to run on a given machine. Nothing in render.go reads it.
	Origin string

	Number float64
	Text   string
	Bool   bool
	Rows   []Row
}

// Row is one line of a KindTable reading: what it is, and how much.
type Row struct {
	Label string  `json:"name"`
	Value float64 `json:"value"`
}

// decimals decides whether numeric readings keep their fractional part.
//
// It is package state rather than a parameter because Gauge is called from
// every hardware source, and precision is a presentation choice that has no
// business being threaded through a dozen collectors that do not otherwise
// know the configuration exists. Atomic because the settings page writes it
// while the collector goroutine reads it.
var decimals atomic.Bool

func init() { decimals.Store(true) }

// SetDecimals turns the fractional part of numeric readings on or off for
// every export at once. Off means a value has to move by a whole unit before
// it counts as changed, which is what keeps it out of a time series database.
func SetDecimals(on bool) { decimals.Store(on) }

// Decimals reports whether numeric readings currently keep decimals.
func Decimals() bool { return decimals.Load() }

// EffectivePrecision is the decimal count a reading is actually rendered with:
// the definition's own, or none while decimals are switched off.
func (d Definition) EffectivePrecision() int {
	if !decimals.Load() {
		return 0
	}
	return d.Precision
}

// Gauge builds a numeric reading, rounded to the definition's precision.
//
// Rounding happens here rather than in each exporter so that every format
// agrees on the number. A reading that reaches MQTT as 37 must not reach
// Prometheus as 37.4, or the two disagree about when it last changed.
func Gauge(def Definition, instance string, value float64) Reading {
	return Reading{Def: def, Instance: instance, Number: Round(value, def.EffectivePrecision())}
}

// Text builds a string reading.
//
// The value is forced to valid UTF-8 here, at the one place every text reading
// passes through. Text arrives from the operating system — process names, card
// names, adapter descriptions, volume labels, firmware part numbers — and not
// all of those APIs promise UTF-8. A single stray byte makes Prometheus reject
// the entire scrape and InfluxDB reject the line, so a mangled character is the
// better failure by a wide margin.
func Text(def Definition, instance, value string) Reading {
	return Reading{Def: def, Instance: instance, Text: strings.ToValidUTF8(value, "")}
}

// Bool builds a flag reading.
func Bool(def Definition, instance string, value bool) Reading {
	return Reading{Def: def, Instance: instance, Bool: value}
}

// Table builds a ranked list reading, already in the order it should be shown.
//
// Rows without a label are dropped rather than rendered as a blank line, and a
// table with no rows left produces no reading at all — the same rule the rest of
// the catalogue follows: a measurement that could not be taken is absent, not
// zero. Labels go through the same UTF-8 repair as Text, because they are
// process names and Windows does not promise UTF-8.
func Table(def Definition, instance string, rows []Row) Reading {
	precision := def.EffectivePrecision()

	kept := make([]Row, 0, len(rows))
	for _, row := range rows {
		// Trimmed before the emptiness check: a label of two spaces is not a
		// name, and it renders as a blank line with a number beside it.
		label := strings.TrimSpace(strings.ToValidUTF8(row.Label, ""))
		if label == "" {
			continue
		}
		kept = append(kept, Row{Label: label, Value: Round(row.Value, precision)})
	}
	if len(kept) == 0 {
		return Reading{}
	}
	return Reading{Def: def, Instance: instance, Rows: kept}
}

// instanceAfter says which part of an identifier the instance follows, keyed by
// the dimension being enumerated.
//
// Reading a list of keys should tell you what is being enumerated before it
// tells you what is being measured: disk_c_used and disk_c_free sit together,
// where disk_used_c and disk_free_c scatter one drive across the alphabet. The
// same for graphics cards and network adapters.
//
// Not every dimension wants this. A processor core is enumerated by the noun
// cpu_core, so cpu_core_5 already reads correctly and inserting the number
// earlier would give cpu_5_core. A dimension absent from this map keeps the
// instance at the end.
var instanceAfter = map[string]string{
	"gpu":  "gpu",
	"disk": "disk",
	"nic":  "net",
}

// Key is how this reading is named in JSON and in Home Assistant entity ids.
func (r Reading) Key() string {
	if r.Instance == "" {
		return r.Def.ID
	}
	instance := Slug(r.Instance)

	prefix, ok := instanceAfter[r.Def.InstanceLabel]
	if !ok {
		return r.Def.ID + "_" + instance
	}
	switch {
	case r.Def.ID == prefix:
		return device(prefix, instance)
	case strings.HasPrefix(r.Def.ID, prefix+"_"):
		return device(prefix, instance) + "_" + strings.TrimPrefix(r.Def.ID, prefix+"_")
	default:
		// A measurement filed under a dimension whose noun it does not carry —
		// ping_rtt against the network, say. Appending stays correct.
		return r.Def.ID + "_" + instance
	}
}

// device names one piece of hardware: the noun with its instance stuck to it,
// so a reader sees gpu0 and diskc as single words rather than as two.
//
// The join is dropped only when the instance is itself several words. An
// adapter called "Ethernet 2" slugs to ethernet_2, and netethernet_2 would be
// unreadable — there the separator earns its place.
func device(prefix, instance string) string {
	if strings.Contains(instance, "_") {
		return prefix + "_" + instance
	}
	return prefix + instance
}

// LegacyKeys are the identifiers this reading has been published under before.
//
// Every one of them may still carry a retained discovery message on the broker,
// and a retained message outlives both this program and any entity deleted by
// hand in Home Assistant — delete the entity and it reappears the moment Home
// Assistant restarts. They are therefore retired explicitly, and the list only
// ever grows.
//
// In order: the original form, which simply appended the instance; and the one
// that separated the instance from its noun.
func (r Reading) LegacyKeys() []string {
	if r.Instance == "" {
		return nil
	}
	instance := Slug(r.Instance)
	current := r.Key()

	candidates := []string{r.Def.ID + "_" + instance}
	if prefix, ok := instanceAfter[r.Def.InstanceLabel]; ok {
		switch {
		case r.Def.ID == prefix:
			candidates = append(candidates, prefix+"_"+instance)
		case strings.HasPrefix(r.Def.ID, prefix+"_"):
			candidates = append(candidates,
				prefix+"_"+instance+"_"+strings.TrimPrefix(r.Def.ID, prefix+"_"))
		}
	}

	seen := map[string]bool{current: true}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

// deviceLabels name the piece of hardware an instanced reading belongs to.
//
// An empty label means the instance already names the thing: a network adapter
// is called "Ethernet 2", and "NIC Ethernet 2" would only add noise.
var deviceLabels = map[string]i18n.Text{
	"gpu":  {DE: "GPU", EN: "GPU"},
	"disk": {DE: "Laufwerk", EN: "Drive"},
	"core": {DE: "Kern", EN: "Core"},
	"nic":  {},
}

// groupPrefixes do the same job for readings that exist only once.
//
// A processor has one clock and one temperature, so there is no instance to
// group by — but "Takt" and "Temperatur" alone would still scatter across a
// device page among every other measurement. Keyed by panel rather than by
// group, so the overall processor load, which is collected as a core reading,
// is filed with the rest of the processor.
//
// The core panel has no prefix. FPS and the running game are the headline
// values and belong at the top, not behind a label.
//
// The drive prefix is plural where deviceLabels has it singular, and that is
// deliberate: the only instance-less drive readings are the sums over every
// volume. "Laufwerk Gesamtkapazität" would claim to describe one disk.
var groupPrefixes = map[Group]i18n.Text{
	GroupCPU:  {DE: "CPU", EN: "CPU"},
	GroupRAM:  {DE: "RAM", EN: "RAM"},
	GroupGPU:  {DE: "GPU", EN: "GPU"},
	GroupDisk: {DE: "Laufwerke", EN: "Drives"},
	GroupNet:  {DE: "Netzwerk", EN: "Network"},
}

// hardwarePrefix is what leads this reading's name, empty for the ones that
// need none.
func (r Reading) hardwarePrefix(lang i18n.Lang) string {
	if r.Instance == "" {
		return groupPrefixes[r.Def.PanelGroup()].In(lang)
	}

	label, ok := deviceLabels[r.Def.InstanceLabel]
	if !ok {
		return r.Instance
	}
	if prefix := label.In(lang); prefix != "" {
		return prefix + " " + r.Instance
	}
	return r.Instance
}

// DisplayName is the Home Assistant entity name, which the device name is
// prefixed to by Home Assistant itself.
//
// The hardware comes first, and that is the whole point. Home Assistant sorts a
// device page alphabetically by this name, so "GPU 0 Temperatur" and "GPU 0
// Lüfter" end up next to each other while "GPU-Temperatur 0" and "GPU-Lüfter 0"
// are scattered among every other measurement that happens to start the same
// way. On a device with a hundred entities that is the difference between a
// list you can read and one you cannot.
func (r Reading) DisplayName(lang i18n.Lang) string {
	name := r.Def.Name.In(lang)
	if prefix := r.hardwarePrefix(lang); prefix != "" {
		return prefix + " " + name
	}
	return name
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
	case KindTable:
		// Three views of the same five rows, because three different readers
		// need three different shapes.
		//
		// "top" is separate from the list rather than something a consumer has
		// to index out of it, because that is what a Home Assistant value
		// template reads to get the entity's state, and a template that has to
		// subscript an array fails silently when the array is empty.
		//
		// "apps" is the list, for anything that wants to render the table.
		//
		// rank1..rankN are the same numbers again, flat. That is the only shape
		// a chart can use: a card plotting an attribute over time reads
		// attributes[name] out of every historical state and expects a number
		// there, so a list of objects gives it nothing to draw. The names come
		// along as rankN_name so a legend can say who rank two was.
		out := map[string]any{"top": r.Rows[0].Label, "apps": r.Rows}
		for i, row := range r.Rows {
			rank := strconv.Itoa(i + 1)
			out["rank"+rank] = row.Value
			out["rank"+rank+"_name"] = row.Label
		}
		return out
	default:
		return r.Number
	}
}

// TableText renders a ranked list on one line, for the places that show a
// reading as text: the settings page and -probe.
//
// One line because a panel row is one line, and because a ranking is read at a
// glance — the charts that make the numbers worth keeping are Home Assistant's
// job, not this program's.
func (r Reading) TableText() string {
	precision := r.Def.EffectivePrecision()

	parts := make([]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		value := strconv.FormatFloat(row.Value, 'f', precision, 64)
		if r.Def.Unit != "" {
			value += " " + r.Def.Unit
		}
		parts = append(parts, row.Label+" "+value)
	}
	return strings.Join(parts, " · ")
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
	// Origin is stamped onto everything added from here on. The collector sets
	// it around each source, and a source that draws on more than one backing
	// program sets it again as it switches — which is how the graphics group
	// can say that the temperature came from Afterburner and the memory total
	// from NVML.
	Origin string
}

// Add appends readings, skipping any whose definition is empty. That lets a
// source build a list without checking every optional value at the call site.
func (s *Set) Add(readings ...Reading) {
	for _, r := range readings {
		if r.Def.ID == "" {
			continue
		}
		// The sensor set is filtered here rather than in the exporters, so a
		// measurement the user switched off is absent everywhere at once. A
		// value that the dashboard shows but Home Assistant never receives
		// would be the worse kind of setting.
		if standardOnly.Load() && !r.Def.InStandardSet() {
			continue
		}
		if r.Origin == "" {
			r.Origin = s.Origin
		}
		s.Readings = append(s.Readings, r)
	}
}

// Origins lists what supplied readings in this set, and what each supplied, in
// presentation order.
func (s Set) Origins() []OriginSummary {
	order := []string{}
	byName := map[string][]Reading{}

	for _, r := range s.Readings {
		name := r.Origin
		if name == "" {
			continue
		}
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		byName[name] = append(byName[name], r)
	}
	sort.Strings(order)

	out := make([]OriginSummary, 0, len(order))
	for _, name := range order {
		readings := byName[name]
		sortReadings(readings)
		out = append(out, OriginSummary{Name: name, Readings: readings})
	}
	return out
}

// OriginSummary is one supplier and everything it produced.
type OriginSummary struct {
	Name     string
	Readings []Reading
}

// Names lists the distinct measurements this supplier produced, without
// repeating one name per graphics card or per drive.
func (o OriginSummary) Names(lang i18n.Lang) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range o.Readings {
		name := r.Def.Name.In(lang)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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
	sort.Slice(out, func(i, j int) bool { return LessInstance(out[i], out[j]) })
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
		return LessInstance(a.Instance, b.Instance)
	})
}

// LessInstance orders two instance names.
//
// Numeric instances are compared as numbers: a plain string comparison puts
// core 10 between core 1 and core 2, which is exactly the kind of list nobody
// can read. Anything non-numeric falls back to the usual ordering, which is
// what drive letters and adapter names want.
func LessInstance(a, b string) bool {
	na, aErr := strconv.Atoi(a)
	nb, bErr := strconv.Atoi(b)
	aNumeric, bNumeric := aErr == nil, bErr == nil

	switch {
	case aNumeric && bNumeric:
		return na < nb
	case aNumeric:
		return true // numbers before names
	case bNumeric:
		return false
	default:
		return a < b
	}
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
	out := strings.Trim(b.String(), "_")
	if out != "" {
		return out
	}

	// Nothing survived, which happens as soon as the name is written in a
	// script this filter does not keep: a Japanese adapter name reduces to
	// nothing at all. An empty instance would collapse the key to "net_type_"
	// and, worse, make every such adapter share one entity. A digest of the
	// original keeps them apart and keeps the same name on the same entity
	// across restarts, which is what Home Assistant needs.
	digest := fnv.New32a()
	digest.Write([]byte(s))
	return fmt.Sprintf("x%08x", digest.Sum32())
}
