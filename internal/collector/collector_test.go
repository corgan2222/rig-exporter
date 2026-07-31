package collector

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/corgan/rig-exporter/internal/metrics"
	"github.com/corgan/rig-exporter/internal/rtss"
	"github.com/corgan/rig-exporter/internal/sysinfo"
)

type fakeRTSS struct {
	snap rtss.Snapshot
	err  error
}

func (f fakeRTSS) Read() (rtss.Snapshot, error) { return f.snap, f.err }

type fakeSystem struct {
	cpu        float64
	cpuErr     error
	memory     sysinfo.Memory
	display    sysinfo.Display
	displayErr error
	foreground uint32
	tick       uint32
	idle       float64
	uptime     float64
}

func (f *fakeSystem) CPUPercent() (float64, error)      { return f.cpu, f.cpuErr }
func (f *fakeSystem) Memory() (sysinfo.Memory, error)   { return f.memory, nil }
func (f *fakeSystem) Display() (sysinfo.Display, error) { return f.display, f.displayErr }
func (f *fakeSystem) ForegroundPID() uint32             { return f.foreground }
func (f *fakeSystem) TickCount() uint32                 { return f.tick }
func (f *fakeSystem) IdleSeconds() float64              { return f.idle }
func (f *fakeSystem) UptimeHours() float64              { return f.uptime }

func newSystem() *fakeSystem {
	return &fakeSystem{
		cpu:     24.46,
		memory:  sysinfo.Memory{UsedPercent: 51.27, TotalMB: 32000, UsedMB: 16400},
		display: sysinfo.Display{Width: 2560, Height: 1440, RefreshHz: 165},
		tick:    10_000,
		idle:    12, // whole seconds: the idle reading has no decimals
		uptime:  3.25,
	}
}

// fakeSource stands in for an optional hardware group.
type fakeSource struct {
	group    metrics.Group
	readings []metrics.Reading
	err      error
}

func (f fakeSource) Group() metrics.Group { return f.group }

func (f fakeSource) Collect(set *metrics.Set) error {
	set.Add(f.readings...)
	return f.err
}

func newCollector(source RTSSSource, system SystemSource) *Collector {
	return New(source, system, 3000, slog.Default())
}

func runningGame() fakeRTSS {
	return fakeRTSS{snap: rtss.Snapshot{
		Version: 0x00020007,
		Entries: []rtss.Entry{{
			ProcessID: 4242, Path: `D:\Games\Cyberpunk2077.exe`,
			Time0: 9000, Time1: 10_000, Frames: 143, FrameTimeUs: 6980,
		}},
	}}
}

func TestCollectWithARunningGame(t *testing.T) {
	system := newSystem()
	system.foreground = 4242

	got := newCollector(runningGame(), system).Collect()

	if got.Game() != "Cyberpunk2077.exe" {
		t.Errorf("Game = %q", got.Game())
	}
	if got.FPS() != 143 {
		t.Errorf("FPS = %v, want 143", got.FPS())
	}
	if got.FrametimeMs() != 6.98 {
		t.Errorf("FrametimeMs = %v, want 6.98", got.FrametimeMs())
	}
	if got.Resolution() != "2560x1440" || got.RefreshHz() != 165 {
		t.Errorf("display = %q @ %d", got.Resolution(), got.RefreshHz())
	}
	if got.CPUPercent() != 24.5 {
		t.Errorf("CPUPercent = %v, want 24.5 (rounded to one decimal)", got.CPUPercent())
	}
	if got.RAMPercent() != 51.3 {
		t.Errorf("RAMPercent = %v, want 51.3", got.RAMPercent())
	}
	if !got.RTSSStatus.OK() || got.RTSSVersion != "2.7" {
		t.Errorf("RTSS status = %q version = %q", got.RTSSStatus, got.RTSSVersion)
	}
	if !got.GameRunning() {
		t.Error("GameRunning = false")
	}
	if got.Number(metrics.GamePID.ID) != 4242 {
		t.Errorf("game_pid = %v, want 4242", got.Number(metrics.GamePID.ID))
	}
	if got.Number(metrics.IdleTime.ID) != 12 || got.Number(metrics.Uptime.ID) != 3.25 {
		t.Errorf("idle/uptime = %v/%v", got.Number(metrics.IdleTime.ID), got.Number(metrics.Uptime.ID))
	}
}

// Nothing rendering means fps 0 and game "none"; the machine readings must
// keep coming so the Home Assistant device does not go quiet.
func TestCollectWhenNoGameIsRunning(t *testing.T) {
	got := newCollector(fakeRTSS{snap: rtss.Snapshot{Version: 0x00020007}}, newSystem()).Collect()

	if got.Game() != NoGame {
		t.Errorf("Game = %q, want %q", got.Game(), NoGame)
	}
	if got.FPS() != 0 || got.FrametimeMs() != 0 {
		t.Errorf("FPS = %v, frametime = %v, want zeroes", got.FPS(), got.FrametimeMs())
	}
	if got.GameRunning() {
		t.Error("GameRunning = true with no entries")
	}
	if !got.RTSSStatus.OK() {
		t.Errorf("RTSS status = %q, want ok: RTSS itself is running", got.RTSSStatus)
	}
	// The FPS entity stays numeric rather than disappearing, so a graph drops
	// to the floor instead of breaking into segments.
	if !got.Has(metrics.FPS.ID) {
		t.Error("the fps reading was dropped entirely while idle")
	}
	if got.CPUPercent() == 0 || got.Resolution() != "2560x1440" {
		t.Error("system readings stopped while idle")
	}
}

func TestCollectClassifiesRTSSFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want RTSSStatus
	}{
		{"not running", rtss.ErrNotRunning, RTSSNotRunning},
		{"access denied", rtss.ErrAccessDenied, RTSSAccessDenied},
		{"anything else", errors.New("mapping vanished"), RTSSError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newCollector(fakeRTSS{err: tc.err}, newSystem()).Collect()

			if got.RTSSStatus != tc.want {
				t.Errorf("RTSSStatus = %q, want %q", got.RTSSStatus, tc.want)
			}
			if got.RTSSMessage == "" {
				t.Error("RTSSMessage is empty")
			}
			if got.Flag(metrics.RTSSUp.ID) {
				t.Error("the RTSS diagnostic reading claims it is up")
			}
			if got.Game() != NoGame || got.FPS() != 0 {
				t.Errorf("game = %q fps = %v, want the idle values", got.Game(), got.FPS())
			}
			if got.Resolution() != "2560x1440" {
				t.Error("system readings stopped when RTSS failed")
			}
		})
	}
}

// Losing the display mode (locked session, monitor switch) must not blank the
// resolution reading.
func TestCollectKeepsTheLastKnownDisplayMode(t *testing.T) {
	system := newSystem()
	c := newCollector(fakeRTSS{}, system)

	if got := c.Collect(); got.Resolution() != "2560x1440" {
		t.Fatalf("Resolution = %q", got.Resolution())
	}

	system.displayErr = errors.New("the session is locked")
	got := c.Collect()

	if got.Resolution() != "2560x1440" || got.RefreshHz() != 165 {
		t.Errorf("Resolution = %q @ %d, want the previous mode", got.Resolution(), got.RefreshHz())
	}
}

func TestCollectSurvivesACPUReadFailure(t *testing.T) {
	system := newSystem()
	system.cpuErr = errors.New("counter unavailable")

	got := newCollector(fakeRTSS{}, system).Collect()

	if got.Has(metrics.CPULoad.ID) {
		t.Error("a CPU reading was published despite the counter failing")
	}
	if got.Resolution() != "2560x1440" {
		t.Error("a CPU failure took the other readings down with it")
	}
}

func TestOptionalSourcesContributeTheirReadings(t *testing.T) {
	c := newCollector(fakeRTSS{}, newSystem())
	c.AddSource(fakeSource{
		group: metrics.GroupDisk,
		readings: []metrics.Reading{
			metrics.Gauge(metrics.DiskUsedPercent, "C:", 61.2),
			metrics.Gauge(metrics.DiskUsedPercent, "D:", 12.7),
		},
	})

	got := c.Collect()

	if !got.HasGroup(metrics.GroupDisk) {
		t.Fatal("the disk group is missing")
	}
	if instances := got.GroupInstances(metrics.GroupDisk); len(instances) != 2 {
		t.Errorf("instances = %v, want two drives", instances)
	}
}

// A source that cannot read its hardware must not take the collection down
// with it: that is what makes "report only what is there" work.
func TestAFailingSourceIsRecordedButNotFatal(t *testing.T) {
	c := newCollector(fakeRTSS{}, newSystem())
	c.AddSource(
		fakeSource{group: metrics.GroupGPU, err: errors.New("afterburner is not running")},
		fakeSource{
			group:    metrics.GroupDisk,
			readings: []metrics.Reading{metrics.Gauge(metrics.DiskUsedPercent, "C:", 61.2)},
		},
	)

	got := c.Collect()

	if got.SourceErrors[metrics.GroupGPU] == "" {
		t.Error("the GPU failure was not recorded")
	}
	if got.HasGroup(metrics.GroupGPU) {
		t.Error("the failing group contributed readings")
	}
	if !got.HasGroup(metrics.GroupDisk) {
		t.Error("a failing source suppressed a working one")
	}
	if got.CPUPercent() == 0 {
		t.Error("a failing source suppressed the core readings")
	}
}

// A source that half succeeds keeps what it managed to read.
func TestPartialSourceResultsAreKept(t *testing.T) {
	c := newCollector(fakeRTSS{}, newSystem())
	c.AddSource(fakeSource{
		group:    metrics.GroupDisk,
		readings: []metrics.Reading{metrics.Gauge(metrics.DiskUsedPercent, "C:", 61.2)},
		err:      errors.New("D: could not be opened"),
	})

	got := c.Collect()

	if _, ok := got.Find(metrics.DiskUsedPercent.ID, "C:"); !ok {
		t.Error("the readable drive was discarded along with the unreadable one")
	}
	if got.SourceErrors[metrics.GroupDisk] == "" {
		t.Error("the partial failure was not recorded")
	}
}
