// Package demo stands in for a rendering game when the program was started
// with -demomode.
//
// It exists so a dashboard can be built, photographed and shown to somebody
// without a game running, and so a launcher can be tried that is not installed.
// Nothing here runs without the flag: the application only wires it in when
// asked to, and until the tray switches it on it passes every question through
// to the real answer.
//
// What it fakes is deliberately narrow — the entry RTSS would report and the
// identity a launcher would give it. Everything downstream is the ordinary
// path, which is the point: the frame rate, the open game and its details reach
// the exports exactly the way real ones do.
package demo

import (
	"math"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/gameid"
	"github.com/corgan2222/rig-exporter/internal/rtss"
)

// Title is one game the demo can pretend is running.
type Title struct {
	// Name is the title as its store spells it, and the menu label.
	Name string
	// Exe is the path RTSS would report. Only the file name is published.
	Exe string
	// AppID is the real Steam id, so the artwork address resolves.
	AppID string
	// FPS is the centre the simulated frame rate moves around.
	FPS float64
}

// Titles are the games on offer, in menu order. The first is the default.
var Titles = []Title{
	{Name: "Red Dead Redemption 2", Exe: `C:\Games\Red Dead Redemption 2\RDR2.exe`, AppID: "1174180", FPS: 72},
	{Name: "Grand Theft Auto V", Exe: `C:\Games\Grand Theft Auto V\GTA5.exe`, AppID: "271590", FPS: 118},
	{Name: "Cyberpunk 2077", Exe: `C:\Games\Cyberpunk 2077\bin\x64\Cyberpunk2077.exe`, AppID: "1091500", FPS: 61},
}

// Platforms are the launchers on offer, in menu order. The first is the default.
var Platforms = []string{gameid.PlatformSteam, gameid.PlatformGOG, gameid.PlatformEpic}

const (
	// windowMs is the measurement window the fake entry claims. Ten seconds
	// rather than RTSS's one, because the frame count is an integer: over one
	// second the rate could only ever be a whole number.
	windowMs = 10_000
	// processID is what the entry carries as its process. Any non-zero value
	// does; RTSS entries without one are skipped as empty slots.
	processID = 4242
	// rtssVersion is reported while the demo answers for RTSS: 2.21, a layout
	// the reader accepts.
	rtssVersion = 2<<16 | 21
)

// RTSS is anything that reads the RTSS shared memory, which is the one method
// the collector asks for.
type RTSS interface {
	Read() (rtss.Snapshot, error)
}

// Identify is the question the collector asks about an executable.
type Identify = func(exePath string) (gameid.Game, bool)

// Demo is the switchable state: whether it is on, and what it pretends.
type Demo struct {
	tick func() uint32
	now  func() time.Time

	mu       sync.Mutex
	active   bool
	title    int
	platform string
}

// New builds a demo that is switched off. tick is the millisecond counter RTSS
// timestamps are measured against — the entry has to be fresh by that clock, or
// the collector discards it as a game that stopped rendering.
func New(tick func() uint32) *Demo {
	return &Demo{tick: tick, now: time.Now, platform: Platforms[0]}
}

// Active reports whether the demo is answering instead of RTSS.
func (d *Demo) Active() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active
}

// SetActive switches the simulated game on or off.
func (d *Demo) SetActive(on bool) {
	d.mu.Lock()
	d.active = on
	d.mu.Unlock()
}

// Title is the index into Titles of the game being simulated.
func (d *Demo) Title() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.title
}

// SetTitle picks the simulated game. An index outside Titles is ignored.
func (d *Demo) SetTitle(index int) {
	if index < 0 || index >= len(Titles) {
		return
	}
	d.mu.Lock()
	d.title = index
	d.mu.Unlock()
}

// Platform is the launcher the simulated game claims to belong to.
func (d *Demo) Platform() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.platform
}

// SetPlatform picks that launcher.
func (d *Demo) SetPlatform(platform string) {
	d.mu.Lock()
	d.platform = platform
	d.mu.Unlock()
}

// current reads everything one answer needs under a single lock, so a click in
// the menu cannot land between the game and its launcher.
func (d *Demo) current() (Title, string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return Titles[d.title], d.platform, d.active
}

// Source wraps the real reader. Switched off it is the real reader; switched
// on it reports one entry that is rendering the chosen game.
func (d *Demo) Source(real RTSS) RTSS { return source{demo: d, real: real} }

type source struct {
	demo *Demo
	real RTSS
}

func (s source) Read() (rtss.Snapshot, error) {
	title, _, active := s.demo.current()
	if !active {
		return s.real.Read()
	}

	fps := frameRate(title.FPS, s.demo.now())
	now := s.demo.tick()
	return rtss.Snapshot{
		Version: rtssVersion,
		Entries: []rtss.Entry{{
			ProcessID:   processID,
			Path:        title.Exe,
			Time0:       now - windowMs,
			Time1:       now,
			Frames:      uint32(math.Round(fps * windowMs / 1000)),
			FrameTimeUs: uint32(math.Round(1e6 / fps)),
		}},
	}, nil
}

// Identity wraps the real identification, which may be nil when it is switched
// off in the settings. While the demo is on it names the chosen game under the
// chosen launcher — also with the setting off, because a demo that shows no
// title and no artwork would demonstrate very little.
func (d *Demo) Identity(real Identify) Identify {
	return func(exePath string) (gameid.Game, bool) {
		if title, platform, active := d.current(); active {
			return gameid.Game{Platform: platform, Title: title.Name, AppID: title.AppID}, true
		}
		if real == nil {
			return gameid.Game{}, false
		}
		return real(exePath)
	}
}

// frameRate is the simulated rate at a moment in time.
//
// Three sines rather than noise: a slow swing of ten per cent that reads as the
// scene changing, a faster four per cent on top, and a little jitter. A real
// frame rate wanders like that; random numbers would jump between unrelated
// values twice a second and look like a broken sensor. It is also a pure
// function of the clock, so two exports asking at the same moment agree.
func frameRate(centre float64, at time.Time) float64 {
	t := float64(at.UnixMilli()) / 1000
	return centre * (1 + 0.10*math.Sin(t/9) + 0.04*math.Sin(t/1.7) + 0.015*math.Sin(t*2.3))
}
