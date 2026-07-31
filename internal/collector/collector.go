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
	"time"

	"github.com/corgan/rig-exporter/internal/metrics"
	"github.com/corgan/rig-exporter/internal/rtss"
	"github.com/corgan/rig-exporter/internal/sysinfo"
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
}

// Collector produces snapshots.
type Collector struct {
	rtss    RTSSSource
	system  SystemSource
	sources []Source
	log     *slog.Logger

	idleMs    uint32
	lastDispl sysinfo.Display
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

	c.collectRTSS(&snap)
	c.collectSystem(&snap)

	for _, source := range c.sources {
		if err := source.Collect(&snap.Set); err != nil {
			snap.SourceErrors[source.Group()] = err.Error()
			c.log.Debug("source unavailable", "group", source.Group(), "error", err)
		}
	}
	return snap
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
	// Reporting zero rather than omitting keeps the FPS entity numeric, so
	// Home Assistant graphs it as a line that drops to the floor instead of
	// breaking into segments.
	snap.Add(
		metrics.Gauge(metrics.FPS, "", 0),
		metrics.Gauge(metrics.Frametime, "", 0),
	)
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
}

func classify(err error) (RTSSStatus, string) {
	switch {
	case errors.Is(err, rtss.ErrNotRunning):
		return RTSSNotRunning, rtss.ErrNotRunning.Error()
	case errors.Is(err, rtss.ErrAccessDenied):
		return RTSSAccessDenied, rtss.ErrAccessDenied.Error()
	default:
		return RTSSError, err.Error()
	}
}
