//go:build windows

package ram

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/sysinfo"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// Source collects the memory group: how much is in the machine, how fast it
// runs, and how much of it is in use.
type Source struct {
	system *sysinfo.Provider

	// The firmware description of the modules cannot change while the machine
	// is running, so it is read once.
	once  sync.Once
	info  Info
	fwErr error
}

// New builds the memory source.
func New(system *sysinfo.Provider) *Source { return &Source{system: system} }

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupRAM }

// Collect appends the memory readings.
//
// The usage figures come from Windows and always work; the module facts come
// from the firmware and may not be there at all, which is not a reason to drop
// the usage.
func (s *Source) Collect(set *metrics.Set) error {
	usage, usageErr := s.system.Memory()
	if usageErr == nil {
		set.Add(
			metrics.Gauge(metrics.RAMUsed, "", float64(usage.UsedMB)),
			metrics.Gauge(metrics.RAMFree, "", float64(usage.AvailableMB)),
			metrics.Gauge(metrics.RAMTotal, "", float64(usage.TotalMB)),
		)
		// The load in percent is a core reading, collected whether or not this
		// group runs, and shown on this panel through its Panel override.
		// Free is derived from the amounts rather than from 100 minus the
		// load: Windows rounds the load to whole percent, and the two would
		// disagree by a percentage point at the edges.
		if usage.TotalMB > 0 {
			set.Add(metrics.Gauge(metrics.RAMFreePercent, "",
				float64(usage.AvailableMB)/float64(usage.TotalMB)*100))
		}
	}

	s.once.Do(func() {
		table, err := winapi.SMBIOS()
		if err != nil {
			s.fwErr = err
			return
		}
		s.info, s.fwErr = Parse(table)
	})

	if s.fwErr != nil {
		if usageErr != nil {
			return fmt.Errorf("no memory information: %w", s.fwErr)
		}
		// Usage was collected; the module facts simply are not available.
		return nil
	}

	info := s.info
	if info.ConfiguredSpeed > 0 {
		set.Add(metrics.Gauge(metrics.RAMClock, "", float64(info.ConfiguredSpeed)))
	}
	if info.RatedSpeed > 0 {
		set.Add(metrics.Gauge(metrics.RAMClockMax, "", float64(info.RatedSpeed)))
	}
	if info.Type != "" {
		set.Add(metrics.Text(metrics.RAMType, "", info.Type))
	}
	if info.Slots > 0 {
		set.Add(
			metrics.Gauge(metrics.RAMModules, "", float64(len(info.Modules))),
			metrics.Gauge(metrics.RAMSlots, "", float64(info.Slots)),
		)
	}

	seen := map[string]bool{}
	for i, module := range info.Modules {
		instance := moduleInstance(i, module, seen)
		set.Add(metrics.Text(metrics.RAMModule, instance, describe(module)))
	}
	return usageErr
}

// moduleInstance names a module by its slot, and guarantees the name is
// unique: two modules sharing an instance would collapse into one entity, and
// firmware that labels every slot the same is not unusual.
func moduleInstance(index int, module Module, seen map[string]bool) string {
	instance := module.Slot()
	if instance == "" || seen[instance] {
		instance = strconv.Itoa(index)
	}
	seen[instance] = true
	return instance
}

// describe summarises a module in one line: what it is, how big, how fast.
func describe(module Module) string {
	text := fmt.Sprintf("%d MB %s", module.SizeMB, module.Type)
	if module.ConfiguredSpeed > 0 {
		text += fmt.Sprintf(" @ %d MT/s", module.ConfiguredSpeed)
	}
	if module.Manufacturer != "" {
		text += " · " + module.Manufacturer
	}
	return text
}
