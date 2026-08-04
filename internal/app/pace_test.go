//go:build windows

package app

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The pace is decided per reading, so it is tested per reading rather than by
// waiting and counting: a timing test would depend on whether RTSS sees a game
// on the machine running it, which is the one thing these tests are about.

func TestTheIdlePaceAppliesWhileNothingIsRendering(t *testing.T) {
	cfg := config.Defaults()
	cfg.PollIntervalMs = 500
	cfg.PublishIntervalMs = 2000
	cfg.IdlePublishIntervalMs = 10000
	cfg.Normalize()

	application := New(cfg, t.TempDir()+`\config.json`, applog.Discard())

	if got := application.pollsPerPublish(true); got != 4 {
		t.Errorf("rendering: got %d reads per export, want 4 (2000/500)", got)
	}
	if got := application.pollsPerPublish(false); got != 20 {
		t.Errorf("idle: got %d reads per export, want 20 (10000/500)", got)
	}
}

// A poll interval that cannot be divided into must never make the loop publish
// on every single read.
func TestThePaceNeverCollapsesToEveryRead(t *testing.T) {
	cfg := config.Defaults()
	cfg.PollIntervalMs = 1000
	cfg.PublishIntervalMs = 1000
	cfg.IdlePublishIntervalMs = 1000
	cfg.Normalize()

	application := New(cfg, t.TempDir()+`\config.json`, applog.Discard())

	for _, rendering := range []bool{true, false} {
		if got := application.pollsPerPublish(rendering); got != 1 {
			t.Errorf("rendering=%v: got %d, want 1 when both rates match", rendering, got)
		}
	}
}

// Both halves of the condition matter. RTSS keeps an entry alive for a moment
// after the last frame, so "a game is running" alone would hold the fast pace
// open on a machine that stopped rendering.
func TestRenderingNeedsBothAGameAndFrames(t *testing.T) {
	cases := []struct {
		name    string
		running bool
		fps     float64
		want    bool
	}{
		{"a game delivering frames", true, 143.5, true},
		{"a game at a standstill", true, 0, false},
		{"frames without a game", false, 60, false},
		{"an idle desktop", false, 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var snap collector.Snapshot
			snap.Add(
				metrics.Bool(metrics.GameRunning, "", tc.running),
				metrics.Gauge(metrics.FPS, "", tc.fps),
			)
			if got := snap.Rendering(); got != tc.want {
				t.Errorf("rendering() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Switching decimals off has to reach the readings themselves, not just the
// discovery payload: a value that still arrives as 37.4 costs a database row
// however Home Assistant chooses to display it.
func TestDecimalsFollowTheConfiguration(t *testing.T) {
	t.Cleanup(func() { metrics.SetDecimals(true) })

	cfg := config.Defaults()
	cfg.Decimals = false
	New(cfg, t.TempDir()+`\config.json`, applog.Discard())

	if metrics.Decimals() {
		t.Fatal("decimals are still on although the configuration switched them off")
	}
	if got := metrics.Gauge(metrics.FPS, "", 143.47).Number; got != 143 {
		t.Errorf("got %v, want the fractional part dropped", got)
	}
}
