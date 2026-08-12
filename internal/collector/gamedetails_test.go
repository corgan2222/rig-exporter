package collector

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/gameid"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// Off is how the program ships, and off has to mean nothing at all: no
// reading, no key in the document, nothing for an attributes template to find.
func TestNoDetailsWhileTheIdentificationIsOff(t *testing.T) {
	got := newCollector(runningGame(), newSystem()).Collect()

	if got.Has(metrics.GameDetails.ID) {
		t.Error("details were published without the identification being switched on")
	}
	if got.Game() != "Cyberpunk2077.exe" {
		t.Errorf("Game = %q, want the executable", got.Game())
	}
}

func TestTheIdentifiedGameRidesAlongsideTheExecutable(t *testing.T) {
	c := newCollector(runningGame(), newSystem())

	var asked string
	c.UseGameIdentity(func(exePath string) (gameid.Game, bool) {
		asked = exePath
		return gameid.Game{
			Platform: gameid.PlatformSteam,
			Title:    "Cyberpunk 2077",
			AppID:    "1091500",
		}, true
	})

	got := c.Collect()

	// The full path, because the directory is what says which launcher
	// installed the game; the file name alone would answer nothing.
	if asked != `D:\Games\Cyberpunk2077.exe` {
		t.Errorf("identification was asked about %q, want the full path RTSS reports", asked)
	}
	// The established measurement is untouched.
	if got.Game() != "Cyberpunk2077.exe" {
		t.Errorf("Game = %q, want the executable it has always been", got.Game())
	}
	if got.GameDetail(metrics.DetailPlatform) != gameid.PlatformSteam {
		t.Errorf("platform = %q", got.GameDetail(metrics.DetailPlatform))
	}
	if got.GameDetail(metrics.DetailTitle) != "Cyberpunk 2077" {
		t.Errorf("title = %q", got.GameDetail(metrics.DetailTitle))
	}
	if got.GameDetail(metrics.DetailAppID) != "1091500" {
		t.Errorf("app id = %q", got.GameDetail(metrics.DetailAppID))
	}
}

// The half-known case, which is the ordinary one for a game that is not on
// Steam: a title from the launcher, and no app id until the store answers — if
// it ever does. What is missing is missing, not empty.
func TestAGameWithoutAnAppIDPublishesTheRest(t *testing.T) {
	c := newCollector(runningGame(), newSystem())
	c.UseGameIdentity(func(string) (gameid.Game, bool) {
		return gameid.Game{Platform: gameid.PlatformGOG, Title: "Cyberpunk 2077"}, true
	})

	got := c.Collect()

	reading, ok := got.Find(metrics.GameDetails.ID, "")
	if !ok {
		t.Fatal("no details for a game the launcher named")
	}
	value, isObject := reading.Value().(map[string]any)
	if !isObject {
		t.Fatalf("Value() = %T, want an object", reading.Value())
	}
	if _, present := value[metrics.DetailAppID]; present {
		t.Errorf("app id present as %v, want it left out entirely", value[metrics.DetailAppID])
	}
	if len(value) != 2 {
		t.Errorf("details = %v, want the two that are known", value)
	}
}

// RTSS hooks whatever renders, and that is often not a game. An executable
// nobody recognises produces no details at all — not a platform of "unknown",
// not an empty title.
func TestAnUnrecognisedExecutableProducesNoDetails(t *testing.T) {
	c := newCollector(runningGame(), newSystem())
	c.UseGameIdentity(func(string) (gameid.Game, bool) { return gameid.Game{}, false })

	if got := c.Collect(); got.Has(metrics.GameDetails.ID) {
		t.Error("details were published for an executable nothing recognised")
	}
}

// With nothing rendering there is nothing to identify, and the question is not
// worth asking: it would send the identification off to read catalogues about
// an empty path on every idle poll.
func TestNothingIsIdentifiedWhileNothingRenders(t *testing.T) {
	c := newCollector(fakeRTSS{}, newSystem())

	asked := 0
	c.UseGameIdentity(func(string) (gameid.Game, bool) {
		asked++
		return gameid.Game{}, false
	})

	got := c.Collect()
	if asked != 0 {
		t.Errorf("the identification was asked %d times with nothing rendering", asked)
	}
	if got.Has(metrics.GameDetails.ID) {
		t.Error("details were published with nothing rendering")
	}
}

// The origin is for the person reading the settings page, and it should name
// the launcher that knew the game rather than RTSS, which only supplied a path.
func TestTheDetailsAreCreditedToTheLauncher(t *testing.T) {
	c := newCollector(runningGame(), newSystem())
	c.UseGameIdentity(func(string) (gameid.Game, bool) {
		return gameid.Game{Platform: gameid.PlatformEpic, Title: "DOOM 64", AppID: "1148590"}, true
	})

	reading, ok := c.Collect().Find(metrics.GameDetails.ID, "")
	if !ok {
		t.Fatal("no details reading")
	}
	if reading.Origin != "Epic Games" {
		t.Errorf("origin = %q, want the launcher that named the game", reading.Origin)
	}
	// And the frame rate beside it still belongs to RTSS.
	if fps, _ := c.Collect().Find(metrics.FPS.ID, ""); fps.Origin != "RivaTuner (RTSS)" {
		t.Errorf("the frame rate's origin became %q", fps.Origin)
	}
}
