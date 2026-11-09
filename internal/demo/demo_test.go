package demo

import (
	"errors"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/gameid"
	"github.com/corgan2222/rig-exporter/internal/rtss"
)

type realRTSS struct{ err error }

func (r realRTSS) Read() (rtss.Snapshot, error) { return rtss.Snapshot{}, r.err }

func newDemo() *Demo { return New(func() uint32 { return 5_000_000 }) }

// Switched off, the demo must not exist as far as a reading is concerned. That
// is the state a start with -demomode is in until somebody opens the menu, and
// a simulation that leaked into it would be a wrong number, not a sparse one.
func TestSwitchedOffEverythingPassesThrough(t *testing.T) {
	d := newDemo()
	sentinel := errors.New("the real reader was asked")

	if _, err := d.Source(realRTSS{err: sentinel}).Read(); !errors.Is(err, sentinel) {
		t.Errorf("Read = %v, want the real reader's answer", err)
	}
	if _, ok := d.Identity(nil)(`C:\x.exe`); ok {
		t.Error("identified a game although neither the demo nor the setting is on")
	}

	real := func(string) (gameid.Game, bool) { return gameid.Game{Title: "real"}, true }
	if game, _ := d.Identity(real)(`C:\x.exe`); game.Title != "real" {
		t.Errorf("title = %q, want the real identification's answer", game.Title)
	}
}

// The fake entry has to survive the same selection a real one goes through:
// fresh by the tick counter, a process id, and a frame rate that comes out of
// the frame count the way RTSS's does.
func TestSwitchedOnItRendersTheChosenGame(t *testing.T) {
	d := newDemo()
	d.SetActive(true)
	d.SetTitle(2)

	snap, err := d.Source(realRTSS{err: errors.New("must not be asked")}).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	entry, ok := rtss.SelectActive(snap.Entries, 0, d.tick(), 3000)
	if !ok {
		t.Fatal("the collector would discard the entry as not rendering")
	}
	if entry.Name() != "Cyberpunk2077.exe" {
		t.Errorf("game = %q, want Cyberpunk2077.exe", entry.Name())
	}

	centre := Titles[2].FPS
	if fps := entry.FPS(); fps < centre*0.8 || fps > centre*1.2 {
		t.Errorf("fps = %.1f, want it near %.0f", fps, centre)
	}
	if entry.FrametimeMs() <= 0 {
		t.Error("no frame time, and the dashboard blanks the tile for that")
	}
}

// Plausible means bounded and moving: never a value no game would show, and
// never a flat line that looks like a stuck sensor.
func TestTheFrameRateWandersWithinItsBand(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	lowest, highest := 1e9, 0.0
	for second := 0; second < 3600; second++ {
		fps := frameRate(100, start.Add(time.Duration(second)*time.Second))
		lowest, highest = min(lowest, fps), max(highest, fps)
	}

	if lowest < 84 || highest > 116 {
		t.Errorf("range %.1f to %.1f, want it inside 84 to 116", lowest, highest)
	}
	if highest-lowest < 10 {
		t.Errorf("range %.1f to %.1f barely moves", lowest, highest)
	}
}

// The launcher is a free choice, independent of the game — that is the whole
// reason it is a menu of its own.
func TestTheChosenLauncherNamesTheGame(t *testing.T) {
	d := newDemo()
	d.SetActive(true)
	d.SetTitle(0)
	d.SetPlatform(gameid.PlatformGOG)

	game, ok := d.Identity(nil)(`C:\whatever.exe`)
	want := gameid.Game{Platform: "gog", Title: "Red Dead Redemption 2", AppID: "1174180"}
	if !ok || game != want {
		t.Errorf("game = %+v (%v), want %+v", game, ok, want)
	}
}

func TestATitleOutsideTheListIsIgnored(t *testing.T) {
	d := newDemo()
	d.SetTitle(1)
	d.SetTitle(len(Titles))
	d.SetTitle(-1)
	if d.Title() != 1 {
		t.Errorf("title = %d, want the last valid choice", d.Title())
	}
}
