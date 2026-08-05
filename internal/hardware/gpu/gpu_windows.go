//go:build windows

package gpu

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/hardware/afterburner"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// Source collects the GPU group.
//
// It re-picks its backing sources on every collection rather than latching one
// at startup, so starting Afterburner mid-session upgrades the Windows adapter
// inventory to live telemetry without a restart.
type Source struct {
	afterburner afterburner.Reader
	// lastSource records which backends produced the most recent readings,
	// for the diagnostic entity and the settings page.
	lastSource string
	// engines reads the WDDM performance counters, on a slower schedule of
	// its own.
	engines *engineSampler
}

// New builds the GPU source.
func New() *Source { return &Source{engines: newEngineSampler()} }

// Close releases the performance counters the engine sampler holds open. A
// configuration change throws the whole source away and builds a new one, so
// without this every save would leak two PDH queries.
func (s *Source) Close() {
	if s.engines != nil {
		s.engines.close()
	}
}

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupGPU }

// Collect appends the GPU readings.
//
// DXGI establishes a stable Windows adapter inventory first. Afterburner then
// contributes live readings for NVIDIA, AMD and Intel, and NVML fills NVIDIA
// gaps such as power and memory use. The error is a diagnostic: no graphics
// adapter at all is a normal state, not a failure of the exporter.
func (s *Source) Collect(set *metrics.Set) error {
	var sources []string
	cards := map[string]string{}       // instance -> card name
	luids := map[string]windows.LUID{} // instance -> adapter identifier

	if adapters, err := dxgiAdapters(); err == nil {
		if mergeFromDXGI(set, adapters, cards, luids) {
			sources = append(sources, "Windows DXGI")
		}
	}

	if snap, err := s.afterburner.Read(); err == nil && snap.CardCount() > 0 {
		if collectFromAfterburner(set, snap, cards) > 0 {
			sources = append(sources, "MSI Afterburner "+snap.VersionString())
		}
	}

	if nvmlCards, err := nvmlCards(); err == nil {
		if mergeFromNVML(set, nvmlCards, cards) {
			sources = append(sources, "NVIDIA NVML")
		}
	}

	// Last, so the overall utilisation only fills a gap the vendor sources
	// left. The engine breakdown and the memory figure are its own and are
	// added whatever else ran.
	if s.engines != nil && s.engines.merge(set, luids) {
		sources = append(sources, "Windows GPU counters")
	}

	if len(sources) == 0 {
		s.lastSource = ""
		return fmt.Errorf("no graphics adapter found: Windows DXGI, MSI Afterburner, and NVML are unavailable")
	}

	// Derived once per card from the name, after both collectors have had their
	// say, so the answer does not depend on which of them named the card.
	for instance, name := range cards {
		if _, exists := set.Find(metrics.GPUVendor.ID, instance); exists {
			continue
		}
		if vendor := vendorOf(name); vendor != "" {
			set.Add(metrics.Text(metrics.GPUVendor, instance, vendor))
		}
	}

	s.lastSource = strings.Join(sources, " + ")
	set.Add(metrics.Text(metrics.GPUSource, "", s.lastSource))
	return nil
}

// vendorOf reads the manufacturer out of a card name.
//
// Neither Afterburner nor NVML publishes a vendor field, and the name is the
// one thing both always supply. Matched on the marketing names people actually
// see, with the vendor spelled the way the vendor spells it — a value somebody
// will compare against in an automation should not be a surprise.
func vendorOf(name string) string {
	lower := strings.ToLower(name)
	for _, candidate := range []struct{ match, vendor string }{
		{"nvidia", "NVIDIA"},
		{"geforce", "NVIDIA"},
		{"quadro", "NVIDIA"},
		{"tesla", "NVIDIA"},
		{"radeon", "AMD"},
		{"amd", "AMD"},
		{"firepro", "AMD"},
		{"intel", "Intel"},
		{"arc ", "Intel"},
		{"iris", "Intel"},
	} {
		if strings.Contains(lower, candidate.match) {
			return candidate.vendor
		}
	}
	return ""
}

// SourceName is what produced the most recent readings, empty when none did.
func (s *Source) SourceName() string { return s.lastSource }

// afterburnerSensor maps a definition onto the sensor name suffixes
// Afterburner might use for it. Names differ between vendors and driver
// versions, so each entry lists every spelling seen in the wild.
type afterburnerSensor struct {
	def      metrics.Definition
	suffixes []string
}

var afterburnerSensors = []afterburnerSensor{
	{metrics.GPULoad, []string{"usage", "utilization"}},
	{metrics.GPUTemperature, []string{"temperature"}},
	{metrics.GPUHotspot, []string{"hotspot temperature", "junction temperature"}},
	{metrics.GPUCoreClock, []string{"core clock", "clock"}},
	{metrics.GPUMemoryClock, []string{"memory clock"}},
	{metrics.GPUVRAMUsed, []string{"memory usage", "used dedicated memory"}},
	{metrics.GPUVRAMPercent, []string{"FB usage"}},
	{metrics.GPUFan, []string{"fan speed"}},
	{metrics.GPUFanRPM, []string{"fan tachometer"}},
	{metrics.GPUPower, []string{"power"}},
	{metrics.GPUVoltage, []string{"voltage"}},
}

// collectFromAfterburner appends readings for every card Afterburner knows,
// records their names in cards, and reports how many sensors were found.
func collectFromAfterburner(set *metrics.Set, snap afterburner.Snapshot, cards map[string]string) int {
	found := 0
	named := make([]namedCard, snap.CardCount())
	for index := range named {
		named[index].Index = index
		if index < len(snap.GPUs) {
			named[index].Name = snap.GPUs[index].Device
		}
	}
	instances := assignNamedInstances(named, cards)

	// This source draws on two programs and hands off between them, so it says
	// which is speaking rather than leaving the interface to guess.
	previous := set.Origin
	set.Origin = "MSI Afterburner"
	defer func() { set.Origin = previous }()

	for index := 0; index < snap.CardCount(); index++ {
		instance := instances[index]

		name := ""
		if index < len(snap.GPUs) {
			name = snap.GPUs[index].Device
		}
		if name != "" {
			if _, exists := set.Find(metrics.GPUName.ID, instance); !exists {
				set.Add(metrics.Text(metrics.GPUName, instance, name))
			}
			if _, exists := cards[instance]; !exists {
				cards[instance] = name
			}
		}

		for _, sensor := range afterburnerSensors {
			entry, ok := snap.FindGPU(index, sensor.suffixes...)
			if !ok {
				continue
			}
			set.Add(metrics.Gauge(sensor.def, instance, entry.Value))
			found++
		}
	}
	return found
}

// mergeFromDXGI adds the adapter facts Windows exposes without a vendor tool.
// It deliberately contributes only inventory: DXGI does not report the live
// temperature, clocks or utilisation that Afterburner and NVML provide.
func mergeFromDXGI(set *metrics.Set, adapters []dxgiAdapter, cards map[string]string, luids map[string]windows.LUID) bool {
	added := false

	previous := set.Origin
	set.Origin = "Windows DXGI"
	defer func() { set.Origin = previous }()

	named := make([]namedCard, len(adapters))
	for i, adapter := range adapters {
		named[i] = namedCard{Index: adapter.Index, Name: adapter.Name}
	}
	instances := assignNamedInstances(named, cards)

	for i, adapter := range adapters {
		instance := instances[i]
		cards[instance] = adapter.Name
		// Recorded even when nothing else about this adapter is worth adding:
		// it is what maps the performance counters onto this card.
		luids[instance] = adapter.LUID

		addText := func(def metrics.Definition, value string) {
			if value == "" {
				return
			}
			if _, exists := set.Find(def.ID, instance); exists {
				return
			}
			set.Add(metrics.Text(def, instance, value))
			added = true
		}
		addMemory := func(def metrics.Definition, bytes uint64) {
			if bytes == 0 {
				return
			}
			if _, exists := set.Find(def.ID, instance); exists {
				return
			}
			set.Add(metrics.Gauge(def, instance, float64(bytes)/(1024*1024)))
			added = true
		}

		addText(metrics.GPUName, adapter.Name)
		addText(metrics.GPUVendor, vendorFromPCI(adapter.VendorID))
		addText(metrics.GPUDriverVersion, adapter.DriverVersion)
		addMemory(metrics.GPUDedicatedMemoryTotal, adapter.DedicatedVideoMemory)
		addMemory(metrics.GPUSharedMemoryTotal, adapter.SharedSystemMemory)
	}
	return added
}

func vendorFromPCI(id uint32) string {
	switch id {
	case 0x1002, 0x1022:
		return "AMD"
	case 0x10de:
		return "NVIDIA"
	case 0x8086:
		return "Intel"
	default:
		return ""
	}
}

// mergeFromNVML adds the readings NVML has that are still missing.
//
// Cards are matched by name rather than by index, because Afterburner and NVML
// enumerate independently and a machine with two cards from different vendors
// would otherwise attribute one card's memory to the other.
func mergeFromNVML(set *metrics.Set, nvml []nvmlCard, cards map[string]string) bool {
	added := false

	previous := set.Origin
	set.Origin = "NVIDIA NVML"
	defer func() { set.Origin = previous }()

	instances := assignInstances(nvml, cards)
	for i, card := range nvml {
		instance := instances[i]
		cards[instance] = card.Name

		add := func(def metrics.Definition, value float64, have bool) {
			if !have {
				return
			}
			if _, exists := set.Find(def.ID, instance); exists {
				return
			}
			set.Add(metrics.Gauge(def, instance, value))
			added = true
		}

		if _, exists := set.Find(metrics.GPUName.ID, instance); !exists {
			set.Add(metrics.Text(metrics.GPUName, instance, card.Name))
			added = true
		}
		add(metrics.GPULoad, card.LoadPercent, card.hasLoad)
		add(metrics.GPUTemperature, card.TempC, card.hasTemp)
		add(metrics.GPUCoreClock, card.CoreClock, card.CoreClock > 0)
		add(metrics.GPUMemoryClock, card.MemClock, card.MemClock > 0)
		add(metrics.GPUVRAMUsed, card.VRAMUsedMB, card.hasVRAM)
		add(metrics.GPUVRAMTotal, card.VRAMTotalMB, card.hasVRAM)
		add(metrics.GPUPower, card.PowerW, card.hasPower)
		add(metrics.GPUPowerLimit, card.PowerLimitW, card.hasLimit)
		add(metrics.GPUFan, card.FanPercent, card.hasFan)
		add(metrics.GPUFanRPM, card.FanRPM, card.hasFanRPM)

		// How close the card is to its power ceiling is the number that says
		// whether the limit is what is holding it back.
		if card.hasPower && card.hasLimit && card.PowerLimitW > 0 {
			if _, exists := set.Find(metrics.GPUPowerPercent.ID, instance); !exists {
				set.Add(metrics.Gauge(metrics.GPUPowerPercent, instance,
					card.PowerW/card.PowerLimitW*100))
				added = true
			}
		}

		// The percentage is derived rather than read, so it is only added when
		// both halves are known and nothing has supplied it already.
		if card.hasVRAM && card.VRAMTotalMB > 0 {
			if _, exists := set.Find(metrics.GPUVRAMPercent.ID, instance); !exists {
				set.Add(metrics.Gauge(metrics.GPUVRAMPercent, instance,
					card.VRAMUsedMB/card.VRAMTotalMB*100))
				added = true
			}
		}
	}
	return added
}

// assignInstances decides which instance each NVML card belongs to, returning
// one instance per card in the order the cards were given.
//
// Afterburner and NVML enumerate independently, so the two lists have to be
// joined on something. The card name is all there is, and it is not unique: two
// identical cards produce two identical names. Three rules keep that from
// corrupting the readings.
//
// No instance is handed out twice, so a pair of RTX 4090s cannot both land on
// Afterburner's card 0 — which is what happened when this matched names against
// a map, since Go randomises map iteration and the winner changed from run to
// run. Names are matched in index order instead, which pairs the two lists the
// way anyone would by hand.
//
// An unmatched card never takes an instance that Afterburner has already named.
// A laptop whose integrated chip is Afterburner's card 0 would otherwise be
// handed the discrete card's VRAM and power limit the moment the two spell the
// name differently. Such a card gets an instance of its own instead: one more
// entry than expected is a great deal easier to understand than two entries
// quietly describing the same silicon.
func assignInstances(nvml []nvmlCard, cards map[string]string) []string {
	named := make([]namedCard, len(nvml))
	for i, card := range nvml {
		named[i] = namedCard{Index: card.Index, Name: card.Name}
	}
	return assignNamedInstances(named, cards)
}

type namedCard struct {
	Index int
	Name  string
}

// assignNamedInstances joins an independently enumerated card list onto the
// stable instances already known. Names win over positions because Windows,
// Afterburner and NVML can enumerate a hybrid laptop in different orders.
func assignNamedInstances(incoming []namedCard, cards map[string]string) []string {
	known := make([]string, 0, len(cards))
	for instance := range cards {
		known = append(known, instance)
	}
	sort.Slice(known, func(i, j int) bool { return metrics.LessInstance(known[i], known[j]) })

	out := make([]string, len(incoming))
	claimed := make(map[string]bool, len(incoming))

	normalise := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

	for i, card := range incoming {
		// A name-less Afterburner snapshot still has trustworthy sensor indices.
		// Reusing an already inventoried card at that position is less surprising
		// than inventing a duplicate card merely because the optional name array
		// was not present in this shared-memory version.
		if strings.TrimSpace(card.Name) == "" {
			candidate := strconv.Itoa(card.Index)
			if _, exists := cards[candidate]; exists && !claimed[candidate] {
				out[i] = candidate
				claimed[candidate] = true
			}
			continue
		}
		for _, instance := range known {
			if claimed[instance] || normalise(cards[instance]) != normalise(card.Name) {
				continue
			}
			out[i] = instance
			claimed[instance] = true
			break
		}
	}

	for i, card := range incoming {
		if out[i] != "" {
			continue
		}
		// Its own index, but only if Afterburner has not already put a card
		// there and no earlier NVML card took it.
		candidate := strconv.Itoa(card.Index)
		if _, taken := cards[candidate]; !taken && !claimed[candidate] {
			out[i] = candidate
			claimed[candidate] = true
			continue
		}
		for next := 0; ; next++ {
			candidate := strconv.Itoa(next)
			if _, taken := cards[candidate]; taken || claimed[candidate] {
				continue
			}
			out[i] = candidate
			claimed[candidate] = true
			break
		}
	}
	return out
}
