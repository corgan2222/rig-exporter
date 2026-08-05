//go:build windows

package app

import (
	"log/slog"
	"time"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/hardware/battery"
	"github.com/corgan2222/rig-exporter/internal/hardware/cpu"
	"github.com/corgan2222/rig-exporter/internal/hardware/disk"
	"github.com/corgan2222/rig-exporter/internal/hardware/gpu"
	hwnet "github.com/corgan2222/rig-exporter/internal/hardware/net"
	"github.com/corgan2222/rig-exporter/internal/hardware/pawnio"
	"github.com/corgan2222/rig-exporter/internal/hardware/procs"
	"github.com/corgan2222/rig-exporter/internal/hardware/ram"
	"github.com/corgan2222/rig-exporter/internal/rtss"
	"github.com/corgan2222/rig-exporter/internal/sysinfo"
)

// sensors holds the optional sources and anything they own that needs to be
// shut down, so a configuration change can replace the whole lot at once.
type sensors struct {
	sources []collector.Source
	pinger  *hwnet.Pinger
	procs   *procs.Sampler
}

// buildSensors creates the optional sources the configuration asks for.
//
// A source being built says nothing about whether it will produce data: each
// reports what it finds and stays quiet about what it does not, which is how a
// machine without Afterburner ends up with no GPU entities rather than a row
// of unavailable ones.
func buildSensors(cfg config.Config, system *sysinfo.Provider, log *slog.Logger) *sensors {
	s := &sensors{}

	if cfg.GPUEnabled {
		s.sources = append(s.sources, gpu.New())
	}
	if cfg.CPUDetailEnabled {
		// Before the ordinary processor source, so its readings win: it goes
		// to the register the vendor publishes, while the fallback reads
		// whatever another program happened to put in shared memory.
		if cfg.PawnIOEnabled {
			s.sources = append(s.sources, pawnio.NewSource(
				pawnio.NewModuleStore(config.ModuleDir()), log))
		}
		s.sources = append(s.sources, cpu.New(cfg.CPUPerCore))
	}
	if cfg.RAMDetailEnabled {
		s.sources = append(s.sources, ram.New(system))
	}
	if cfg.DiskEnabled {
		s.sources = append(s.sources, disk.New(cfg.WantsDisk))
	}
	if cfg.NetEnabled {
		if cfg.PingEnabled {
			s.pinger = hwnet.NewPinger(cfg.PingTarget, cfg.PingCount,
				time.Duration(cfg.PingIntervalMs)*time.Millisecond, log)
		}
		s.sources = append(s.sources, hwnet.New(s.pinger, cfg.NetAllAdapters))
	}
	if cfg.BatteryEnabled {
		s.sources = append(s.sources, battery.New(log))
	}
	// Last, because it is the only source that reads the whole machine rather
	// than one piece of it, and its own ticker means the order here decides
	// nothing but where its rows land in the output.
	if cfg.TopProcessesEnabled {
		s.procs = procs.New(cfg.TopProcessesCount,
			time.Duration(cfg.TopProcessesIntervalMs)*time.Millisecond, log)
		s.sources = append(s.sources, procs.NewSource(s.procs))
	}
	return s
}

// start launches anything that runs on its own schedule.
func (s *sensors) start() {
	if s.pinger != nil {
		s.pinger.Start()
	}
	if s.procs != nil {
		s.procs.Start()
	}
}

// stop releases those goroutines again, and anything a source holds open.
//
// A configuration change throws the whole set away and builds a new one, so a
// source that owns an operating system handle — the processor source keeps a
// PDH query open for the lifetime of the set — would otherwise leak one handle
// per save.
func (s *sensors) stop() {
	if s.pinger != nil {
		s.pinger.Stop()
	}
	// Matched against the method that exists rather than against io.Closer:
	// there is nothing for these to report, and an error return that is always
	// nil only invites a caller to check it.
	for _, source := range s.sources {
		if closer, ok := source.(interface{ Close() }); ok {
			closer.Close()
		}
	}
}

// Probe builds a collector with the configured sources for one-off use, such
// as the -probe command line flag. The returned functions start and stop the
// sources that run on their own schedule.
//
// The metrics options are applied here as well, not only in New. They are
// package state in metrics, so a probe that skipped them reported the extended
// set with decimals whatever the configuration said — and a diagnostic that
// shows something other than what is published is worse than no diagnostic.
func Probe(cfg config.Config, log *slog.Logger) (c *collector.Collector, start, stop func()) {
	applyMetricsOptions(cfg)
	collectorInstance, sensors := buildCollector(cfg, rtss.Reader{}, sysinfo.New(), log)
	return collectorInstance, sensors.start, sensors.stop
}

// sensorsChanged reports whether the optional sources need rebuilding.
func sensorsChanged(a, b config.Config) bool {
	if a.GPUEnabled != b.GPUEnabled ||
		a.CPUDetailEnabled != b.CPUDetailEnabled ||
		a.CPUPerCore != b.CPUPerCore ||
		a.RAMDetailEnabled != b.RAMDetailEnabled ||
		a.DiskEnabled != b.DiskEnabled ||
		a.NetEnabled != b.NetEnabled ||
		a.NetAllAdapters != b.NetAllAdapters ||
		a.BatteryEnabled != b.BatteryEnabled ||
		a.PingEnabled != b.PingEnabled ||
		a.PingTarget != b.PingTarget ||
		a.PingCount != b.PingCount ||
		a.PingIntervalMs != b.PingIntervalMs ||
		a.IdleTimeoutMs != b.IdleTimeoutMs ||
		a.SelfUsageEnabled != b.SelfUsageEnabled ||
		a.TopProcessesEnabled != b.TopProcessesEnabled ||
		a.TopProcessesCount != b.TopProcessesCount ||
		a.TopProcessesIntervalMs != b.TopProcessesIntervalMs {
		return true
	}
	return !sameStrings(a.DiskInclude, b.DiskInclude)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
