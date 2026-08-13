package webui

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The frame time tile used to decide for itself whether it had a reading, by
// testing whether the number was greater than zero. That works right up until
// the number legitimately is zero — which RTSS produces about once a second,
// every time its measurement window resets under a poll. The tile then blanked
// for one poll and came back, twice a second, on a game that never stuttered.
//
// The payload now says so outright, so the page has something to read that a
// value cannot be confused with.
func TestTheFrametimeSaysWhetherItIsAMeasurement(t *testing.T) {
	server, _ := newServer(t, func(cfg *config.Config) { cfg.GPUEnabled = true })
	cfg := server.app.Config()

	measured := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}
	measured.Set.Add(metrics.Gauge(metrics.Frametime, "", 6.98))

	if resp := server.statusFor(app.Status{Config: cfg, Snapshot: measured}); !resp.FrametimeAvailable {
		t.Error("frametime_available = false for a collected frame time")
	}

	// Nothing added at all: the state the collector now leaves behind when RTSS
	// has no frame time to give. Frametime itself is 0.0 either way, which is
	// exactly why the flag has to exist.
	missing := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}

	resp := server.statusFor(app.Status{Config: cfg, Snapshot: missing})
	if resp.FrametimeAvailable {
		t.Error("frametime_available = true without a frame time reading")
	}
	if resp.Frametime != 0 {
		t.Errorf("frametime = %v, want the zero value: there is nothing to report", resp.Frametime)
	}
}
