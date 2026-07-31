//go:build windows

package app

import (
	"log/slog"
	"time"

	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/hardware/cpu"
	"github.com/corgan/rig-exporter/internal/hardware/disk"
	"github.com/corgan/rig-exporter/internal/hardware/gpu"
	hwnet "github.com/corgan/rig-exporter/internal/hardware/net"
	"github.com/corgan/rig-exporter/internal/hardware/ram"
	"github.com/corgan/rig-exporter/internal/rtss"
	"github.com/corgan/rig-exporter/internal/sysinfo"
)

// sensors holds the optional sources and anything they own that needs to be
// shut down, so a configuration change can replace the whole lot at once.
type sensors struct {
	sources []collector.Source
	pinger  *hwnet.Pinger
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
	return s
}

// start launches anything that runs on its own schedule.
func (s *sensors) start() {
	if s.pinger != nil {
		s.pinger.Start()
	}
}

// stop releases those goroutines again.
func (s *sensors) stop() {
	if s.pinger != nil {
		s.pinger.Stop()
	}
}

// Probe builds a collector with the configured sources for one-off use, such
// as the -probe command line flag. The returned functions start and stop the
// sources that run on their own schedule.
func Probe(cfg config.Config, log *slog.Logger) (c *collector.Collector, start, stop func()) {
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
		a.PingEnabled != b.PingEnabled ||
		a.PingTarget != b.PingTarget ||
		a.PingCount != b.PingCount ||
		a.PingIntervalMs != b.PingIntervalMs ||
		a.IdleTimeoutMs != b.IdleTimeoutMs {
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
