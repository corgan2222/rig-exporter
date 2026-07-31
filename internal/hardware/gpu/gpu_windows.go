//go:build windows

package gpu

import (
	"fmt"
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

	s.lastSource = strings.Join(sources, " + ")
	set.Add(metrics.Text(metrics.GPUSource, "", s.lastSource))
	return nil
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

	for _, card := range nvml {
		instance := instanceFor(card, cards)
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

// instanceFor finds which instance an NVML card already occupies, matching on
// the card name, and falls back to its own index when it is not known yet.
func instanceFor(card nvmlCard, cards map[string]string) string {
	for instance, name := range cards {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(card.Name)) {
			return instance
		}
	}
	return strconv.Itoa(card.Index)
}
