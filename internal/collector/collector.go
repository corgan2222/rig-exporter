// Package collector turns the hardware sources into one Set of readings,
// which is the only thing the exporters and the tray ever look at.
//
// The core source — RTSS plus the always-available system counters — is built
// in. Everything else is an optional Source that the app adds according to the
// configuration, and that drops out silently when its data is unavailable.
package collector

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/rtss"
	"github.com/corgan2222/rig-exporter/internal/sysinfo"
)

// NoGame is reported for the game reading while nothing is being rendered.
const NoGame = "none"

// RTSSStatus describes whether FPS data is reachable, which is surfaced both
// in the tray and as a Home Assistant diagnostic entity.
type RTSSStatus string

const (
	// RTSSOK means the shared memory was read successfully.
	RTSSOK RTSSStatus = "ok"
	// RTSSNotRunning means RivaTuner Statistics Server is not running.
	RTSSNotRunning RTSSStatus = "not_running"
	// RTSSAccessDenied means RTSS runs elevated and this process does not.
	RTSSAccessDenied RTSSStatus = "access_denied"
	// RTSSError covers everything else, with the detail in the message.
	RTSSError RTSSStatus = "error"
)

// OK reports whether FPS values can be trusted.
func (s RTSSStatus) OK() bool { return s == RTSSOK }

// Source contributes the readings of one sensor group.
//
// Collect appends to the set rather than returning a slice, so a source that
// half succeeds — three of four disks readable — still reports what it has.
// An error is a diagnostic, not a reason to discard those readings.
type Source interface {
	Group() metrics.Group
	Collect(set *metrics.Set) error
}

// Snapshot is one complete collection pass.
type Snapshot struct {
	metrics.Set
	At time.Time

	RTSSStatus  RTSSStatus
	RTSSMessage string
	RTSSVersion string

	// FPSOrigin names what supplied the frame rate when it did not come from
	// RTSS, and is empty otherwise. The interface needs it to tell a genuine
	// reading from the zero that stands for "nothing is rendering".
	FPSOrigin string

	// SourceErrors records which optional groups failed, for the settings page.
	SourceErrors map[metrics.Group]string
}

// Typed accessors for the values the tray and the settings page show. They
// read out of the same Set the exporters publish, so there is no second copy
// of the data to keep in sync.

// FPS is the current frame rate, 0 when nothing is rendering.
func (s Snapshot) FPS() float64 { return s.Number(metrics.FPS.ID) }

// FrametimeMs is the time the most recent frame took.
func (s Snapshot) FrametimeMs() float64 { return s.Number(metrics.Frametime.ID) }

// Game is the rendering application, or NoGame.
func (s Snapshot) Game() string { return s.Str(metrics.Game.ID) }

// GameRunning reports whether an application is currently rendering.
func (s Snapshot) GameRunning() bool { return s.Flag(metrics.GameRunning.ID) }

// Rendering reports whether a game is actually producing frames, which is what
// picks between the two publish intervals.
//
// Both halves matter: RTSS keeps an entry alive for a moment after the last
// frame, and a game sitting at zero frames a second has nothing to say that is
// worth a fast series.
func (s Snapshot) Rendering() bool { return s.GameRunning() && s.FPS() > 0 }

// Resolution is the primary display mode, e.g. "2560x1440".
func (s Snapshot) Resolution() string { return s.Str(metrics.Resolution.ID) }

// RefreshHz is the primary display refresh rate.
func (s Snapshot) RefreshHz() int { return int(s.Number(metrics.RefreshRate.ID)) }

// CPUPercent is the system-wide processor load.
func (s Snapshot) CPUPercent() float64 { return s.Number(metrics.CPULoad.ID) }

// RAMPercent is the physical memory load.
func (s Snapshot) RAMPercent() float64 { return s.Number(metrics.RAMLoad.ID) }

// RTSSSource reads the RTSS shared memory.
type RTSSSource interface {
	Read() (rtss.Snapshot, error)
}

// SystemSource supplies the always-available machine counters.
type SystemSource interface {
	CPUPercent() (float64, error)
	Memory() (sysinfo.Memory, error)
	Display() (sysinfo.Display, error)
	ForegroundPID() uint32
	TickCount() uint32
	IdleSeconds() float64
	UptimeHours() float64
	WindowsVersion() string
	ProcessCount() (int, error)
	SelfUsage() (sysinfo.SelfUsage, error)
}

// FrameRate reads a frame rate from somewhere other than RTSS, reporting
// whether one was available at all.
//
// A graphics driver can count presented frames without any overlay program:
// AMD's ADLX does, for whatever is running fullscreen. What it cannot say is
// which application that is, which is why this supplies a number and not a
// game — the name and the process id stay RTSS's alone.
type FrameRate func() (float64, bool)

// Collector produces snapshots.
type Collector struct {
	rtss    RTSSSource
	system  SystemSource
	sources []Source
	log     *slog.Logger

	// frames stands in for RTSS when RTSS has no rendering application, and
	// frameOrigin names what supplied the number.
	frames      FrameRate
	frameOrigin string

	idleMs    uint32
	lastDispl sysinfo.Display

	// The operating system cannot change under a running process, so it is
	// read once rather than on every collection.
	osOnce    sync.Once
	osVersion string

	// version is this program's own build, reported alongside the readings so
	// a series can say what wrote it.
	version string

	// selfUsage adds what this process costs the machine. Its own sensor group,
	// off by default: it is a measurement of the tool, not of the PC.
	selfUsage bool
}

// ReportVersion makes the collector publish which build produced its readings.
func (c *Collector) ReportVersion(version string) { c.version = version }

// ReportSelfUsage makes the collector publish this process's own CPU share and
// working set.
func (c *Collector) ReportSelfUsage(on bool) { c.selfUsage = on }

// UseFrameRateFallback registers a frame rate to fall back on when RTSS has no
// rendering application, labelled with what supplies it.
//
// It never displaces RTSS. RTSS knows the game, the process and the time the
// last frame actually took; a driver counter knows none of those. It only fills
// the case where the frame rate would otherwise be reported as zero.
func (c *Collector) UseFrameRateFallback(origin string, read FrameRate) {
	c.frames, c.frameOrigin = read, origin
}

// New wires a collector with the core source only. idleMs is how long an RTSS
// entry may go without a new frame before it stops counting as the game.
func New(rtssSource RTSSSource, system SystemSource, idleMs int, log *slog.Logger) *Collector {
	if idleMs < 0 {
		idleMs = 0
	}
	if log == nil {
		log = slog.Default()
	}
	return &Collector{rtss: rtssSource, system: system, idleMs: uint32(idleMs), log: log}
}

// AddSource registers an optional sensor group.
func (c *Collector) AddSource(sources ...Source) {
	for _, s := range sources {
		if s != nil {
			c.sources = append(c.sources, s)
		}
	}
}

// Collect takes one reading. It never fails: a source that errors contributes
// nothing and is recorded in SourceErrors, which is what keeps CPU, RAM and
// the display reporting while RTSS or a graphics card is unavailable.
func (c *Collector) Collect() Snapshot {
	snap := Snapshot{At: time.Now(), SourceErrors: map[metrics.Group]string{}}

	snap.Set.Origin = "RivaTuner (RTSS)"
	c.collectRTSS(&snap)
	snap.Set.Origin = originWindows
	c.collectSystem(&snap)

	for _, source := range c.sources {
		// Everything a source adds is stamped with what supplied it. A source
		// backed by more than one program overrides this as it goes.
		snap.Set.Origin = originOf(source)
		if err := source.Collect(&snap.Set); err != nil {
			snap.SourceErrors[source.Group()] = err.Error()
			c.log.Debug("source unavailable", "group", source.Group(), "error", err)
		}
	}
	snap.Set.Origin = ""
	return snap
}

// OriginNamer is implemented by sources that read something other than Windows
// itself. Everything else genuinely does come from Windows, so that is the
// default rather than a placeholder.
type OriginNamer interface {
	OriginName() string
}

const (
	originWindows = "Windows"
	// originExporter credits the one reading the program makes up itself.
	originExporter = "rig-exporter"
)

func originOf(source Source) string {
	if named, ok := source.(OriginNamer); ok {
		if name := named.OriginName(); name != "" {
			return name
		}
	}
	return originWindows
}

func (c *Collector) collectRTSS(snap *Snapshot) {
	data, err := c.rtss.Read()
	if err != nil {
		snap.RTSSStatus, snap.RTSSMessage = classify(err)
		c.addGameReadings(snap, rtss.Entry{}, false)
		return
	}

	snap.RTSSStatus = RTSSOK
	snap.RTSSVersion = data.VersionString()

	entry, ok := rtss.SelectActive(data.Entries, c.system.ForegroundPID(), c.system.TickCount(), c.idleMs)
	c.addGameReadings(snap, entry, ok)
}

func (c *Collector) addGameReadings(snap *Snapshot, entry rtss.Entry, running bool) {
	game := NoGame
	if running {
		game = entry.Name()
	}

	snap.Add(
		metrics.Text(metrics.Game, "", game),
		metrics.Bool(metrics.GameRunning, "", running),
		metrics.Bool(metrics.RTSSUp, "", snap.RTSSStatus.OK()),
		metrics.Text(metrics.RTSSStatus, "", string(snap.RTSSStatus)),
	)
	if snap.RTSSVersion != "" {
		snap.Add(metrics.Text(metrics.RTSSVersion, "", snap.RTSSVersion))
	}
	if running {
		snap.Add(
			metrics.Gauge(metrics.FPS, "", entry.FPS()),
			metrics.Gauge(metrics.Frametime, "", entry.FrametimeMs()),
			metrics.Gauge(metrics.GamePID, "", float64(entry.ProcessID)),
		)
		return
	}
	// RTSS has nothing rendering. The graphics driver may still be counting
	// presented frames, and on a machine without RTSS that is the difference
	// between a frame rate and a permanent zero.
	if fps, ok := c.frameRateFallback(); ok {
		previous := snap.Set.Origin
		snap.Set.Origin = c.frameOrigin
		snap.Add(
			metrics.Gauge(metrics.FPS, "", fps),
			// Derived, the same way FrametimeMs already falls back to the
			// inverse of the rate on RTSS builds without a frame time counter.
			metrics.Gauge(metrics.Frametime, "", 1000/fps),
		)
		snap.Set.Origin = previous
		snap.FPSOrigin = c.frameOrigin
		return
	}

	// Reporting zero rather than omitting keeps the FPS entity numeric, so
	// Home Assistant graphs it as a line that drops to the floor instead of
	// breaking into segments.
	snap.Add(
		metrics.Gauge(metrics.FPS, "", 0),
		metrics.Gauge(metrics.Frametime, "", 0),
	)
}

// frameRateFallback asks the registered driver counter, if there is one. A rate
// of zero is treated as no answer: it means nothing is presenting, which is
// exactly the state the zero below already describes.
func (c *Collector) frameRateFallback() (float64, bool) {
	if c.frames == nil {
		return 0, false
	}
	fps, ok := c.frames()
	if !ok || fps <= 0 {
		return 0, false
	}
	return fps, true
}

func (c *Collector) collectSystem(snap *Snapshot) {
	if cpu, err := c.system.CPUPercent(); err == nil {
		snap.Add(metrics.Gauge(metrics.CPULoad, "", cpu))
	}
	// Only the headline percentage lives here; the amounts and the module
	// facts belong to the memory group, which can be switched off.
	if mem, err := c.system.Memory(); err == nil {
		snap.Add(metrics.Gauge(metrics.RAMLoad, "", mem.UsedPercent))
	}

	display, err := c.system.Display()
	if err != nil {
		// Display queries fail while the session is locked or monitors are
		// switching; the last known mode beats reporting zeroes.
		display = c.lastDispl
	} else {
		c.lastDispl = display
	}
	snap.Add(
		metrics.Text(metrics.Resolution, "", display.String()),
		metrics.Gauge(metrics.RefreshRate, "", float64(display.RefreshHz)),
		metrics.Gauge(metrics.DisplayWidth, "", float64(display.Width)),
		metrics.Gauge(metrics.DisplayHeight, "", float64(display.Height)),
	)

	snap.Add(
		metrics.Gauge(metrics.IdleTime, "", c.system.IdleSeconds()),
		metrics.Gauge(metrics.Uptime, "", c.system.UptimeHours()),
	)

	// Not a machine reading: this is the program saying which build produced
	// everything else, so it is credited to the program rather than to Windows.
	if c.version != "" {
		reading := metrics.Text(metrics.ExporterVersion, "", c.version)
		reading.Origin = originExporter
		snap.Add(reading)
	}

	// Also the program talking about itself, so also credited to it rather than
	// to Windows. Not an optional Source, because those are keyed by group and
	// this belongs to the core group, which is always there. A failure is left
	// out rather than reported as zero: "the exporter is using no CPU" and "the
	// counter could not be read" are different answers, and only one of them is
	// reassuring.
	if c.selfUsage {
		if usage, err := c.system.SelfUsage(); err == nil {
			cpu := metrics.Gauge(metrics.ExporterCPU, "", usage.CPUPercent)
			memory := metrics.Gauge(metrics.ExporterMemory, "", usage.MemoryMB)
			cpu.Origin, memory.Origin = originExporter, originExporter
			snap.Add(cpu, memory)
		} else {
			c.log.Debug("own resource usage unavailable", "error", err)
		}
	}

	// The operating system cannot change while the process runs, so it is read
	// once and kept.
	c.osOnce.Do(func() { c.osVersion = c.system.WindowsVersion() })
	if c.osVersion != "" {
		snap.Add(metrics.Text(metrics.OSVersion, "", c.osVersion))
	}
	if processes, err := c.system.ProcessCount(); err == nil {
		snap.Add(metrics.Gauge(metrics.Processes, "", float64(processes)))
	}
}

func classify(err error) (RTSSStatus, string) {
	switch {
	case errors.Is(err, rtss.ErrNotRunning):
		return RTSSNotRunning, rtss.ErrNotRunning.Error()
	// A section left behind by a closed RTSS means the same thing to a reader
	// as no section at all, and saying so beats explaining a signature.
	case errors.Is(err, rtss.ErrShutDown):
		return RTSSNotRunning, rtss.ErrShutDown.Error()
	case errors.Is(err, rtss.ErrAccessDenied):
		return RTSSAccessDenied, rtss.ErrAccessDenied.Error()
	default:
		return RTSSError, err.Error()
	}
}
