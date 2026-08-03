//go:build windows

package gpu

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/corgan/rig-exporter/internal/hardware/afterburner"
	"github.com/corgan/rig-exporter/internal/metrics"
)

// Source collects the GPU group.
//
// It re-picks its backing sources on every collection rather than latching one
// at startup, so starting Afterburner mid-session upgrades the readings from
// the NVML subset to the full set without a restart.
type Source struct {
	afterburner afterburner.Reader
	// lastSource records which backends produced the most recent readings,
	// for the diagnostic entity and the settings page.
	lastSource string
}

// New builds the GPU source.
func New() *Source { return &Source{} }

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupGPU }

// Collect appends the GPU readings.
//
// Afterburner is preferred because it covers NVIDIA, AMD and Intel and reports
// fan speed, voltage and the hotspot temperature that NVML does not. NVML then
// fills whatever gaps are left — chiefly the total amount of graphics memory,
// which Afterburner does not publish. The error is a diagnostic: no graphics
// source at all is a normal state, not a failure of the exporter.
func (s *Source) Collect(set *metrics.Set) error {
	var sources []string
	cards := map[string]string{} // instance -> card name

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

	if len(sources) == 0 {
		s.lastSource = ""
		return fmt.Errorf("no graphics telemetry source: neither MSI Afterburner nor NVML is available")
	}

	// Derived once per card from the name, after both collectors have had their
	// say, so the answer does not depend on which of them named the card.
	for instance, name := range cards {
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

	// This source draws on two programs and hands off between them, so it says
	// which is speaking rather than leaving the interface to guess.
	previous := set.Origin
	set.Origin = "MSI Afterburner"
	defer func() { set.Origin = previous }()

	for index := 0; index < snap.CardCount(); index++ {
		instance := strconv.Itoa(index)

		name := ""
		if index < len(snap.GPUs) {
			name = snap.GPUs[index].Device
		}
		if name != "" {
			set.Add(metrics.Text(metrics.GPUName, instance, name))
			cards[instance] = name
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
	known := make([]string, 0, len(cards))
	for instance := range cards {
		known = append(known, instance)
	}
	sort.Slice(known, func(i, j int) bool { return metrics.LessInstance(known[i], known[j]) })

	out := make([]string, len(nvml))
	claimed := make(map[string]bool, len(nvml))

	normalise := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

	for i, card := range nvml {
		for _, instance := range known {
			if claimed[instance] || normalise(cards[instance]) != normalise(card.Name) {
				continue
			}
			out[i] = instance
			claimed[instance] = true
			break
		}
	}

	for i, card := range nvml {
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
