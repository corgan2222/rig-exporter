//go:build windows

package app

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// -probe has to show what is published, not what the catalogue could produce.
//
// The two settings that reach into the catalogue live as package state in
// metrics, and Probe used to skip them: a machine configured for the standard
// set without decimals was diagnosed as the extended set with them, which is
// the one thing a diagnostic must never do.
func TestProbeAppliesTheSettingsThatReachTheCatalogue(t *testing.T) {
	everything := metrics.Resolve(metrics.PresetExtended, nil, nil)
	t.Cleanup(func() {
		metrics.SetSelection(everything)
		metrics.SetDecimals(true)
	})
	metrics.SetSelection(everything)
	metrics.SetDecimals(true)

	cfg := config.Defaults()
	cfg.Measurements.Preset = config.PresetMinimal
	cfg.Decimals = false
	// Nothing that would touch hardware or a schedule; only the wiring matters.
	cfg.GPUEnabled, cfg.CPUDetailEnabled, cfg.RAMDetailEnabled = false, false, false
	cfg.DiskEnabled, cfg.NetEnabled, cfg.PingEnabled = false, false, false

	_, _, stop := Probe(cfg, applog.Discard())
	defer stop()

	// The clock is on the extended rung only, so a minimal configuration that
	// reached the catalogue is one that no longer selects it.
	if metrics.Selected(metrics.CPUClock.ID) {
		t.Error("-probe reported the whole catalogue although the minimal rung is configured")
	}
	if metrics.Decimals() {
		t.Error("-probe kept the decimals although they are switched off")
	}
}

// Every switch that decides which sources exist has to make sensorsChanged say
// yes. One that does not is the quietest kind of broken: the setting is saved,
// the interface shows it as taken, and the source list keeps whatever it had
// until the program is restarted. PawnIO was in exactly that state.
func TestEverySourceSwitchRebuildsTheSources(t *testing.T) {
	switches := map[string]func(*config.Config, bool){
		"gpu_enabled":           func(c *config.Config, on bool) { c.GPUEnabled = on },
		"cpu_detail_enabled":    func(c *config.Config, on bool) { c.CPUDetailEnabled = on },
		"cpu_per_core":          func(c *config.Config, on bool) { c.CPUPerCore = on },
		"pawnio_enabled":        func(c *config.Config, on bool) { c.PawnIOEnabled = on },
		"special_hardware":      func(c *config.Config, on bool) { c.SpecialHardwareEnabled = on },
		"ram_detail_enabled":    func(c *config.Config, on bool) { c.RAMDetailEnabled = on },
		"disk_enabled":          func(c *config.Config, on bool) { c.DiskEnabled = on },
		"net_enabled":           func(c *config.Config, on bool) { c.NetEnabled = on },
		"net_all_adapters":      func(c *config.Config, on bool) { c.NetAllAdapters = on },
		"battery_enabled":       func(c *config.Config, on bool) { c.BatteryEnabled = on },
		"ping_enabled":          func(c *config.Config, on bool) { c.PingEnabled = on },
		"self_usage_enabled":    func(c *config.Config, on bool) { c.SelfUsageEnabled = on },
		"top_processes_enabled": func(c *config.Config, on bool) { c.TopProcessesEnabled = on },
	}

	for name, set := range switches {
		t.Run(name, func(t *testing.T) {
			base := config.Defaults()
			// PawnIO only reaches a source alongside the processor detail.
			base.CPUDetailEnabled = true
			set(&base, false)

			changed := base
			set(&changed, true)

			if !sensorsChanged(base, changed) {
				t.Error("flipping this switch does not rebuild the sources, so the " +
					"setting is saved and then ignored until a restart")
			}
		})
	}
}

// closingSource stands in for the processor source, which holds a PDH query
// open for as long as the set exists.
type closingSource struct{ closed int }

func (c *closingSource) Group() metrics.Group           { return metrics.GroupCPU }
func (c *closingSource) Collect(set *metrics.Set) error { return nil }
func (c *closingSource) Close()                         { c.closed++ }

type plainSource struct{}

func (plainSource) Group() metrics.Group           { return metrics.GroupDisk }
func (plainSource) Collect(set *metrics.Set) error { return nil }

// Saving the settings throws the whole set away and builds a new one, so a
// source that owns an operating system handle has to be told: without this,
// every save leaked a PDH query for the lifetime of the process.
func TestStoppingClosesSourcesThatOwnSomething(t *testing.T) {
	closing := &closingSource{}
	s := &sensors{sources: []collector.Source{closing, plainSource{}}}

	s.stop()

	if closing.closed != 1 {
		t.Errorf("Close called %d times, want once", closing.closed)
	}
}
