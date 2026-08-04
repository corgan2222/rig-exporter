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
	t.Cleanup(func() {
		metrics.SetStandardOnly(false)
		metrics.SetDecimals(true)
	})
	metrics.SetStandardOnly(false)
	metrics.SetDecimals(true)

	cfg := config.Defaults()
	cfg.SensorSet = config.SensorSetStandard
	cfg.Decimals = false
	// Nothing that would touch hardware or a schedule; only the wiring matters.
	cfg.GPUEnabled, cfg.CPUDetailEnabled, cfg.RAMDetailEnabled = false, false, false
	cfg.DiskEnabled, cfg.NetEnabled, cfg.PingEnabled = false, false, false

	_, _, stop := Probe(cfg, applog.Discard())
	defer stop()

	if !metrics.StandardOnly() {
		t.Error("-probe reported the extended set although the standard one is configured")
	}
	if metrics.Decimals() {
		t.Error("-probe kept the decimals although they are switched off")
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
