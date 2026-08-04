//go:build windows

package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/export"
)

// countingTarget records how often it was handed a reading.
type countingTarget struct {
	exports atomic.Uint64
	started atomic.Bool
	stopped atomic.Bool
}

func (c *countingTarget) Start() error {
	c.started.Store(true)
	return nil
}

func (c *countingTarget) Export(collector.Snapshot) error {
	c.exports.Add(1)
	return nil
}

func (c *countingTarget) Stop() { c.stopped.Store(true) }

func (c *countingTarget) Status() export.Status {
	return export.Status{Name: "counting", Label: "Counting", Healthy: true}
}

// newTestApp builds an app whose only export target counts, and whose
// hardware sources are all switched off so a test does not depend on what the
// machine happens to have.
//
// Intervals are given in milliseconds and must be at or above
// config.MinIntervalMs: Normalize raises anything shorter, and a test built on
// a value it silently changed would be measuring the wrong thing.
func newTestApp(t *testing.T, poll, publish int) (*App, *countingTarget) {
	t.Helper()

	if poll < config.MinIntervalMs || publish < config.MinIntervalMs {
		t.Fatalf("test intervals %d/%d are below the %d ms minimum", poll, publish, config.MinIntervalMs)
	}

	cfg := config.Defaults()
	cfg.MQTTEnabled = false
	cfg.DataServerEnabled = false
	cfg.GPUEnabled = false
	cfg.CPUDetailEnabled = false
	cfg.RAMDetailEnabled = false
	cfg.DiskEnabled = false
	cfg.NetEnabled = false
	cfg.PollIntervalMs = poll
	cfg.PublishIntervalMs = publish
	// Both paces are set to the same value on purpose. These tests are about
	// the timing of the loop, not about the game/idle split, and the split
	// would otherwise make them depend on whether RTSS happens to see a
	// rendering application on the machine running the test.
	cfg.IdlePublishIntervalMs = publish
	cfg.Normalize()

	application := New(cfg, t.TempDir()+`\config.json`, applog.Discard())

	target := &countingTarget{}
	application.runners = []*runner{newRunner(target, applog.Discard())}
	return application, target
}

// Reading and publishing are separate rates, and publishing is counted in
// reads rather than timed on its own so the two cannot drift apart.
func TestPublishingRunsAtItsOwnRate(t *testing.T) {
	application, target := newTestApp(t, 250, 1000)

	application.Start()
	defer application.Stop()

	// Four reads per publish: about a dozen reads in three seconds, but only
	// three or four publishes, plus the one that goes out at once.
	time.Sleep(3 * time.Second)

	exports := target.exports.Load()
	if exports < 3 || exports > 6 {
		t.Errorf("got %d exports in three seconds at 1000 ms, want around 4", exports)
	}
	if !target.started.Load() {
		t.Error("the target was never started")
	}

	// Every read updates the status even though most are not published.
	if application.Status().UpdatedAt.IsZero() {
		t.Error("no reading was recorded")
	}
}

// The first reading goes out immediately rather than after a full interval,
// so a freshly started exporter is not silent for two seconds.
func TestTheFirstReadingIsExportedAtOnce(t *testing.T) {
	application, target := newTestApp(t, 250, 60000)

	application.Start()
	defer application.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && target.exports.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if target.exports.Load() == 0 {
		t.Error("nothing was exported although the first reading should go out at once")
	}
}

// Pausing stops the export but not the reading: the tray and the interface
// keep showing live numbers.
func TestPauseStopsExportingButNotReading(t *testing.T) {
	application, target := newTestApp(t, 250, 250)

	application.Start()
	defer application.Stop()

	time.Sleep(600 * time.Millisecond)
	application.SetPaused(true)

	paused := target.exports.Load()
	before := application.Status().UpdatedAt
	time.Sleep(900 * time.Millisecond)

	if after := target.exports.Load(); after != paused {
		t.Errorf("exports went from %d to %d while paused", paused, after)
	}
	if !application.Status().UpdatedAt.After(before) {
		t.Error("reading stopped while paused")
	}

	application.SetPaused(false)
	time.Sleep(600 * time.Millisecond)
	if target.exports.Load() <= paused {
		t.Error("exporting did not resume")
	}
}

func TestStopShutsTargetsDown(t *testing.T) {
	application, target := newTestApp(t, 250, 250)

	application.Start()
	time.Sleep(400 * time.Millisecond)
	application.Stop()

	if !target.stopped.Load() {
		t.Error("the target was not stopped")
	}
	// Stopping twice must not panic on an already closed channel.
	application.Stop()
}

// A configuration change has to reach the running loop, not just the file.
func TestApplyConfigRetimesTheLoop(t *testing.T) {
	application, target := newTestApp(t, 2000, 2000)

	application.Start()
	defer application.Stop()
	time.Sleep(300 * time.Millisecond)

	cfg := application.Config()
	cfg.PollIntervalMs = 250
	cfg.PublishIntervalMs = 250
	cfg.IdlePublishIntervalMs = 250
	if err := application.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	before := target.exports.Load()
	time.Sleep(1500 * time.Millisecond)

	// At the old rate this window would hold nothing at all.
	if got := target.exports.Load() - before; got < 3 {
		t.Errorf("got %d exports in 1.5 s at 250 ms, want the new rate to have taken effect", got)
	}
	if application.Config().PollIntervalMs != 250 {
		t.Error("the configuration was not adopted")
	}
}

// A slow target must not hold up the reading loop, and must not have two
// exports of the same target in flight at once.
func TestASlowTargetDoesNotStallTheLoop(t *testing.T) {
	application, _ := newTestApp(t, 250, 250)

	slow := &slowTarget{release: make(chan struct{})}
	application.runners = []*runner{newRunner(slow, applog.Discard())}

	application.Start()
	defer func() {
		close(slow.release)
		application.Stop()
	}()

	time.Sleep(1200 * time.Millisecond)

	if application.Status().UpdatedAt.IsZero() {
		t.Fatal("no reading was taken while the target was blocked")
	}
	if inFlight := slow.inFlight.Load(); inFlight > 1 {
		t.Errorf("%d exports of one target were in flight at once", inFlight)
	}
}

// slowTarget blocks in Export until it is released.
type slowTarget struct {
	release  chan struct{}
	inFlight atomic.Int64
}

func (s *slowTarget) Start() error { return nil }
func (s *slowTarget) Stop()        {}

func (s *slowTarget) Export(collector.Snapshot) error {
	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	<-s.release
	return nil
}

func (s *slowTarget) Status() export.Status {
	return export.Status{Name: "slow", Label: "Slow"}
}
