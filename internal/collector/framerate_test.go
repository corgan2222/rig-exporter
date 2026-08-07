package collector

import (
	"errors"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/rtss"
)

// driverCounting stands in for a graphics driver that counts presented frames.
func driverCounting(fps float64, ok bool) FrameRate {
	return func() (float64, bool) { return fps, ok }
}

// Without RTSS the frame rate used to be a permanent zero. A driver that counts
// frames itself is the difference between a reading and nothing.
func TestAFrameRateFromTheDriverFillsInForRTSS(t *testing.T) {
	c := newCollector(fakeRTSS{err: rtss.ErrNotRunning}, newSystem())
	c.UseFrameRateFallback("AMD ADLX", driverCounting(59, true))

	got := c.Collect()

	if got.FPS() != 59 {
		t.Errorf("FPS = %v, want 59", got.FPS())
	}
	if got.FPSOrigin != "AMD ADLX" {
		t.Errorf("FPSOrigin = %q, want the driver that counted them", got.FPSOrigin)
	}
	// Derived from the rate, the way FrametimeMs already does on RTSS builds
	// without a frame time counter, and rounded to the two decimals the
	// catalogue gives the measurement: 1000/59 is 16.949…
	if got.FrametimeMs() != 16.95 {
		t.Errorf("FrametimeMs = %v, want 16.95", got.FrametimeMs())
	}
	// RTSS is still not running, and saying otherwise would put the dashboard
	// dot on green while the game name is missing.
	if got.RTSSStatus.OK() {
		t.Error("RTSSStatus claims RTSS is up")
	}
}

// A driver knows the rate, not the application. Inventing a name would be worse
// than admitting there is none.
func TestTheDriverFrameRateNamesNoGame(t *testing.T) {
	c := newCollector(fakeRTSS{err: rtss.ErrNotRunning}, newSystem())
	c.UseFrameRateFallback("AMD ADLX", driverCounting(60, true))

	got := c.Collect()

	if got.Game() != NoGame {
		t.Errorf("Game = %q, want %q", got.Game(), NoGame)
	}
	if got.GameRunning() {
		t.Error("GameRunning is set although nothing identified a game")
	}
	if got.Has(metrics.GamePID.ID) {
		t.Error("a process id was published for a game nobody identified")
	}
}

// RTSS knows the game, the process and the time the last frame really took.
// The driver counter must never displace that.
func TestRTSSKeepsTheFrameRateWhenItHasOne(t *testing.T) {
	system := newSystem()
	system.foreground = 4242

	c := newCollector(runningGame(), system)
	c.UseFrameRateFallback("AMD ADLX", driverCounting(30, true))

	got := c.Collect()

	if got.FPS() != 143 {
		t.Errorf("FPS = %v, want RTSS's 143", got.FPS())
	}
	if got.FrametimeMs() != 6.98 {
		t.Errorf("FrametimeMs = %v, want RTSS's measured 6.98", got.FrametimeMs())
	}
	if got.FPSOrigin != "" {
		t.Errorf("FPSOrigin = %q, want empty: RTSS supplied this", got.FPSOrigin)
	}
}

// Nothing presenting and nothing to fall back on is still a numeric zero, so a
// Home Assistant graph drops to the floor instead of breaking into segments.
func TestWithoutAnyFrameRateTheReadingStaysZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		back FrameRate
	}{
		{"no fallback registered", nil},
		{"the driver has no answer", driverCounting(0, false)},
		// Fullscreen-only counters report nothing rather than zero, but a
		// driver that does answer zero means the same thing.
		{"the driver answers zero", driverCounting(0, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCollector(fakeRTSS{err: errors.New("mapping vanished")}, newSystem())
			if tc.back != nil {
				c.UseFrameRateFallback("AMD ADLX", tc.back)
			}

			got := c.Collect()

			if !got.Has(metrics.FPS.ID) {
				t.Fatal("the FPS reading disappeared entirely")
			}
			if got.FPS() != 0 {
				t.Errorf("FPS = %v, want 0", got.FPS())
			}
			if got.FPSOrigin != "" {
				t.Errorf("FPSOrigin = %q, want empty", got.FPSOrigin)
			}
		})
	}
}

// HasFrameRate is what the tray and the dashboard both ask before showing a
// number instead of a dash. Asking only about RTSS put a dash next to a
// perfectly good driver-counted rate, which is the bug this pins down.
func TestHasFrameRateAcceptsEitherSource(t *testing.T) {
	system := newSystem()
	system.foreground = 4242

	for _, tc := range []struct {
		name string
		rtss fakeRTSS
		back FrameRate
		sys  *fakeSystem
		want bool
	}{
		{"RTSS with a running game", runningGame(), nil, system, true},
		{"the driver counting frames", fakeRTSS{err: rtss.ErrNotRunning},
			driverCounting(60, true), newSystem(), true},
		{"RTSS running, nothing rendering, no driver", fakeRTSS{snap: rtss.Snapshot{Version: 0x00020007}},
			nil, newSystem(), false},
		{"nothing anywhere", fakeRTSS{err: rtss.ErrNotRunning}, nil, newSystem(), false},
		// A driver that answers zero means nothing is presenting, which is the
		// same as having no frame rate at all.
		{"the driver answers zero", fakeRTSS{err: rtss.ErrNotRunning},
			driverCounting(0, true), newSystem(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCollector(tc.rtss, tc.sys)
			if tc.back != nil {
				c.UseFrameRateFallback("AMD ADLX", tc.back)
			}

			if got := c.Collect().HasFrameRate(); got != tc.want {
				t.Errorf("HasFrameRate = %t, want %t", got, tc.want)
			}
		})
	}
}

// RTSS running with nothing rendering is the ordinary desktop case, and the
// driver may well be counting a fullscreen application RTSS has no profile for.
func TestARunningRTSSWithoutAGameStillAcceptsTheDriverRate(t *testing.T) {
	c := newCollector(fakeRTSS{snap: rtss.Snapshot{Version: 0x00020007}}, newSystem())
	c.UseFrameRateFallback("AMD ADLX", driverCounting(61, true))

	got := c.Collect()

	if !got.RTSSStatus.OK() {
		t.Fatalf("RTSSStatus = %q, want ok: RTSS itself is running", got.RTSSStatus)
	}
	if got.FPS() != 61 {
		t.Errorf("FPS = %v, want 61", got.FPS())
	}
	if got.FPSOrigin != "AMD ADLX" {
		t.Errorf("FPSOrigin = %q", got.FPSOrigin)
	}
}
