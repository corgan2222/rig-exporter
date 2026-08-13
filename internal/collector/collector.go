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

	"github.com/corgan2222/rig-exporter/internal/gameid"
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

// GameDetail is one of the identified game's details — its platform, its title
// or its Steam app id. Empty when the identification is switched off, when
// nothing was recognised, or when that particular half is not known yet, and
// the interface shows nothing rather than a gap in all three cases.
func (s Snapshot) GameDetail(name string) string {
	reading, _ := s.Find(metrics.GameDetails.ID, "")
	return reading.Detail(name)
}

// Rendering reports whether a game is actually producing frames, which is what
// picks between the two publish intervals.
//
// Both halves matter: RTSS keeps an entry alive for a moment after the last
// frame, and a game sitting at zero frames a second has nothing to say that is
// worth a fast series.
func (s Snapshot) Rendering() bool { return s.GameRunning() && s.FPS() > 0 }

// HasFrameRate reports whether the frame rate is a measurement rather than the
// zero that stands for "nothing is rendering".
//
// It is the one place that asks "RTSS or the graphics driver?", so the tray and
// the dashboard cannot answer it differently. Asking only about RTSS is what
// made a machine whose driver was counting frames display a dash next to a
// perfectly good number.
func (s Snapshot) HasFrameRate() bool {
	if s.FPSOrigin != "" {
		return true
	}
	return s.RTSSStatus.OK() && s.GameRunning()
}

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
	// IdleSeconds and UptimeHours report whether they could be read at all.
	// Zero is a real answer for both — nobody has touched the machine for zero
	// seconds while they are using it, and a machine that just booted has been
	// up for nearly zero hours — so a failure cannot be signalled by the value.
	IdleSeconds() (float64, bool)
	UptimeHours() (float64, bool)
	WindowsVersion() string
	// Hypervisor names the virtualisation platform, empty on real hardware.
	Hypervisor() string
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

// IdentifyGame turns the executable RTSS is reporting into what the launchers
// and the Steam store call it, reporting whether anything was recognised.
//
// It is handed in rather than built here for the same reason FrameRate is: the
// collector decides when the question is asked, the configuration decides
// whether it may be asked at all, and this package stays free of registries,
// launcher catalogues and anything that talks to a web service.
//
// It runs inside the measurement loop, so it must answer at once. Whatever it
// cannot say yet is simply absent from the reading and arrives on a later one.
type IdentifyGame func(exePath string) (gameid.Game, bool)

// Collector produces snapshots.
type Collector struct {
	rtss    RTSSSource
	system  SystemSource
	sources []*guardedSource
	log     *slog.Logger

	// sourceDeadline is how long one source may take. Zero means it is derived
	// from the poll interval on every collection, which is what production
	// does; tests set it outright.
	sourceDeadline time.Duration
	pollMs         int

	// frames stands in for RTSS when RTSS has no rendering application, and
	// frameOrigin names what supplied the number.
	frames      FrameRate
	frameOrigin string

	// identify names the game behind the executable RTSS reports. Nil while
	// the identification is switched off, which is how it ships.
	identify IdentifyGame

	idleMs    uint32
	lastDispl sysinfo.Display

	// The operating system cannot change under a running process, so it is
	// read once rather than on every collection. Neither can the firmware
	// identity that says whether this is virtual hardware.
	osOnce    sync.Once
	osVersion string

	platformOnce sync.Once
	hypervisor   string

	// version is this program's own build, reported alongside the readings so
	// a series can say what wrote it.
	version string

	// selfUsage adds what this process costs the machine. Its own sensor group,
	// off by default: it is a measurement of the tool, not of the PC.
	selfUsage bool

	// open is the game RTSS last reported, kept for gameLinger after it stops
	// reporting one. See addGameReadings.
	open openGame
	// now is the clock, so the linger can be tested without waiting.
	now func() time.Time
}

// gameLinger is how long the identity of a game outlives its detection.
//
// RTSS reports what is *rendering*, and a game that is open is not always
// rendering: alt-tab to the desktop and many of them stop, at which point RTSS
// drops the entry and the game, its title and its Steam app id all vanish at
// once. In Home Assistant that is a card that empties and refills every time
// somebody switches windows.
//
// Fifteen seconds, and only for the identity — which game is open. Whether it is
// rendering, and how fast, keep answering for themselves: game_running goes
// false immediately and the frame rate goes to zero, because those are true.
// "Which game is open" is the one thing alt-tabbing does not change, so holding
// it is not a stale value but the correct answer to a question the source
// briefly stopped being able to answer.
//
// Long enough to cover switching to a browser and back; short enough that a
// game which really has been closed is gone before anybody looks.
const gameLinger = 15 * time.Second

// openGame is the last game RTSS reported and when it last did.
type openGame struct {
	name string
	// path is what the identification needs. Kept alongside the name because
	// re-deriving it is impossible once RTSS has dropped the entry.
	path string
	at   time.Time
}

// ReportVersion makes the collector publish which build produced its readings.
func (c *Collector) ReportVersion(version string) { c.version = version }

// ReportSelfUsage makes the collector publish this process's own CPU share and
// working set.
func (c *Collector) ReportSelfUsage(on bool) { c.selfUsage = on }

// UseGameIdentity registers what turns the rendering application's executable
// path into the title a store would use. Nil, which is the default, means the
// identification is switched off and no details are ever published.
func (c *Collector) UseGameIdentity(identify IdentifyGame) { c.identify = identify }

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
	return &Collector{
		rtss:   rtssSource,
		system: system,
		idleMs: uint32(idleMs),
		log:    log,
		now:    time.Now,
	}
}

// deadline is how long a single source may take this collection.
//
// Derived from the poll interval rather than fixed, so it scales with what the
// user asked for: a five-second interval gives a source two and a half seconds,
// a 500 ms interval gives it 250 ms. An unknown interval falls back to the
// default poll rate rather than to no limit at all.
func (c *Collector) deadline() time.Duration {
	if c.sourceDeadline > 0 {
		return c.sourceDeadline
	}
	poll := c.pollMs
	if poll <= 0 {
		poll = 1000
	}
	return time.Duration(poll) * time.Millisecond / defaultSourceDeadlineShare
}

// SetPollInterval tells the collector how much time one tick has, which is what
// the per-source deadline is derived from.
func (c *Collector) SetPollInterval(ms int) { c.pollMs = ms }

// AddSource registers an optional sensor group.
func (c *Collector) AddSource(sources ...Source) {
	for _, s := range sources {
		if s != nil {
			c.sources = append(c.sources, &guardedSource{Source: s})
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

	deadline := c.deadline()
	for _, source := range c.sources {
		// Everything a source adds is stamped with what supplied it. A source
		// backed by more than one program overrides this as it goes.
		snap.Set.Origin = originOf(source.Source)
		if err := source.collect(&snap.Set, deadline, c.log); err != nil {
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
	// Which game is open is not the same question as what is rendering. RTSS
	// answers the second one; for gameLinger after it stops answering, its last
	// answer stands for the first.
	open := c.openGame(entry, running)

	game := NoGame
	if open.name != "" {
		game = open.name
	}

	snap.Add(
		metrics.Text(metrics.Game, "", game),
		// Not held. This one means "is something rendering", and the moment
		// somebody alt-tabs the honest answer is no.
		metrics.Bool(metrics.GameRunning, "", running),
		metrics.Bool(metrics.RTSSUp, "", snap.RTSSStatus.OK()),
		metrics.Text(metrics.RTSSStatus, "", string(snap.RTSSStatus)),
	)
	if snap.RTSSVersion != "" {
		snap.Add(metrics.Text(metrics.RTSSVersion, "", snap.RTSSVersion))
	}

	// The full path, not the name: the file name alone is what the game
	// measurement publishes, and the directory is the half that says which
	// launcher installed it.
	//
	// Asked whether or not anything is rendering. A game that is open but idle
	// still has a title and an app id, and those are what a dashboard draws.
	if open.path != "" {
		c.addGameDetails(snap, open.path)
	}

	if running {
		snap.Add(
			metrics.Gauge(metrics.FPS, "", entry.FPS()),
			// Not held: a process id outlives nothing. Once RTSS has dropped the
			// entry, the number may already belong to somebody else.
			metrics.Gauge(metrics.GamePID, "", float64(entry.ProcessID)),
		)
		// Only when RTSS actually measured one. Its window resets about once a
		// second and we read it twice a second, so a poll regularly lands on a
		// window with no frames in it yet: FPS is 0 there, and FrametimeMs has
		// nothing to invert either. Publishing that as 0 ms claims a frame took
		// no time at all, and the dashboard then read the claim back as "no
		// value" and blanked the tile — a flash twice a second on a reading
		// that was never actually interrupted.
		if frametime := entry.FrametimeMs(); frametime > 0 {
			snap.Add(metrics.Gauge(metrics.Frametime, "", frametime))
		}
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
	// breaking into segments. Zero frames per second is a true statement about
	// an idle machine.
	//
	// The frame time gets no such zero. It is a duration, and there is no frame
	// to have taken it — 0 ms would be the one thing a frame time can never be,
	// and a graph of it would dive to a value that reads as infinitely fast
	// rather than as nothing rendering. A missing field claims nothing.
	snap.Add(metrics.Gauge(metrics.FPS, "", 0))
}

// openGame answers which game is open, and remembers the answer.
//
// While RTSS reports one, that is the answer and the clock is reset. Once it
// stops, the last answer stands for gameLinger and is then forgotten — see the
// constant for why the identity is treated differently from the frame rate.
//
// A different game starting replaces the remembered one at once: the branch
// above runs first, so there is never a moment where the old game outranks a
// new one that is actually rendering.
func (c *Collector) openGame(entry rtss.Entry, rendering bool) openGame {
	if rendering {
		c.open = openGame{name: entry.Name(), path: entry.Path, at: c.clock()}
		return c.open
	}
	if c.open.name == "" || c.clock().Sub(c.open.at) > gameLinger {
		c.open = openGame{}
	}
	return c.open
}

// clock is time.Now unless a test said otherwise. The nil check is for a
// Collector built as a struct literal rather than through New, which the tests
// in this package do.
func (c *Collector) clock() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

// addGameDetails publishes what the launchers and the store call the running
// game, when the identification is switched on and recognised it.
//
// Nothing here is filled in: an executable nobody claims produces no reading,
// and a recognised game whose app id has not arrived yet publishes the two
// halves that are known. metrics.Details drops what has no value, so a missing
// half costs nothing at the call site.
func (c *Collector) addGameDetails(snap *Snapshot, exePath string) {
	if c.identify == nil {
		return
	}
	game, ok := c.identify(exePath)
	if !ok {
		return
	}

	// Credited to the launcher that named the game rather than to RTSS, which
	// only supplied the path. As everywhere else, the origin is for the person
	// reading the settings page and reaches no export.
	previous := snap.Set.Origin
	snap.Set.Origin = gameOrigin(game.Platform)
	snap.Add(metrics.Details(metrics.GameDetails, "",
		metrics.Detail{Name: metrics.DetailPlatform, Value: game.Platform},
		metrics.Detail{Name: metrics.DetailTitle, Value: game.Title},
		metrics.Detail{Name: metrics.DetailAppID, Value: game.AppID},
	))
	snap.Set.Origin = previous
}

// gameOrigin names the launcher a game was identified through, for the origins
// list on the settings page. An unknown platform falls back to the program
// itself rather than to a blank, which would drop the reading from that list.
func gameOrigin(platform string) string {
	switch platform {
	case gameid.PlatformSteam:
		return "Steam"
	case gameid.PlatformGOG:
		return "GOG Galaxy"
	case gameid.PlatformEpic:
		return "Epic Games"
	default:
		return originExporter
	}
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

	// Added only when they were read. Both have zero as a legitimate value, so
	// publishing a failure as zero does not merely lose a reading — it asserts
	// the opposite: idle_time zero means somebody is at the machine, which is
	// what presence automations in Home Assistant switch on.
	if idle, ok := c.system.IdleSeconds(); ok {
		snap.Add(metrics.Gauge(metrics.IdleTime, "", idle))
	}
	if uptime, ok := c.system.UptimeHours(); ok {
		snap.Add(metrics.Gauge(metrics.Uptime, "", uptime))
	}

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

	// Whether the machine is virtual explains a whole class of readings that
	// are missing or implausible rather than faulty: no board sensors, no real
	// fan, a processor clock the host decides. The flag is published either
	// way; the name only when there is one to give.
	c.platformOnce.Do(func() { c.hypervisor = c.system.Hypervisor() })
	snap.Add(metrics.Bool(metrics.Virtualized, "", c.hypervisor != ""))
	if c.hypervisor != "" {
		snap.Add(metrics.Text(metrics.Hypervisor, "", c.hypervisor))
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
