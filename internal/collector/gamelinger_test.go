package collector

import (
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/gameid"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/rtss"
)

// idleRTSS is RTSS running with nothing rendering, which is what alt-tabbing to
// the desktop looks like from here: the entry is gone, RTSS itself is fine.
func idleRTSS() fakeRTSS {
	return fakeRTSS{snap: rtss.Snapshot{Version: 0x00020007}}
}

// cyberpunkIdentity answers for the one game in these tests, from wherever it is
// asked about — the launcher caches make the second call as cheap as the first.
func cyberpunkIdentity(seen *[]string) IdentifyGame {
	return func(exePath string) (gameid.Game, bool) {
		if seen != nil {
			*seen = append(*seen, exePath)
		}
		return gameid.Game{
			Platform: gameid.PlatformGOG,
			Title:    "Cyberpunk 2077",
			AppID:    "1091500",
		}, true
	}
}

// A game that stops rendering has not stopped being open. Alt-tab to the
// desktop and many games stop presenting frames, RTSS drops the entry, and the
// game, its title and its Steam app id all vanish at once — a Home Assistant
// card that empties and refills every time somebody switches windows.
func TestTheOpenGameSurvivesAPauseInRendering(t *testing.T) {
	at := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)

	c := newCollector(runningGame(), newSystem())
	c.now = func() time.Time { return at }
	c.system.(*fakeSystem).foreground = 4242
	c.UseGameIdentity(cyberpunkIdentity(nil))

	if got := c.Collect(); got.Game() != "Cyberpunk2077.exe" {
		t.Fatalf("Game = %q before anything happened", got.Game())
	}

	// Rendering stops. Ten seconds later the game is still open.
	c.rtss = idleRTSS()
	at = at.Add(10 * time.Second)

	got := c.Collect()
	if got.Game() != "Cyberpunk2077.exe" {
		t.Errorf("Game = %q ten seconds after rendering stopped, want the game still open", got.Game())
	}
	if got.GameDetail(metrics.DetailAppID) != "1091500" {
		t.Errorf("app id = %q, want it held with the game", got.GameDetail(metrics.DetailAppID))
	}
	if got.GameDetail(metrics.DetailTitle) != "Cyberpunk 2077" {
		t.Errorf("title = %q, want it held with the game", got.GameDetail(metrics.DetailTitle))
	}

	// What is *not* held, and must not be. These answer a different question,
	// and for that question "no" is the truth right now.
	if got.GameRunning() {
		t.Error("game_running stayed true while nothing was rendering")
	}
	if got.FPS() != 0 {
		t.Errorf("FPS = %v while nothing was rendering, want 0", got.FPS())
	}
	if got.Has(metrics.GamePID.ID) {
		t.Error("the process id was held; once RTSS drops the entry it may belong to somebody else")
	}
}

// The hold is bounded. A game that really has been closed has to be gone before
// anybody looks, or the reading is just wrong for as long as the machine is on.
func TestTheOpenGameIsForgottenAfterTheLinger(t *testing.T) {
	at := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)

	c := newCollector(runningGame(), newSystem())
	c.now = func() time.Time { return at }
	c.system.(*fakeSystem).foreground = 4242
	c.UseGameIdentity(cyberpunkIdentity(nil))
	c.Collect()

	c.rtss = idleRTSS()
	at = at.Add(gameLinger + time.Second)

	got := c.Collect()
	if got.Game() != NoGame {
		t.Errorf("Game = %q past the linger, want %q", got.Game(), NoGame)
	}
	if got.Has(metrics.GameDetails.ID) {
		t.Error("the details outlived the game they belong to")
	}
}

// A second game starting must win at once. The remembered one is only ever the
// answer while nothing else is rendering — otherwise switching between two games
// would show the wrong one for fifteen seconds.
func TestAGameThatStartsRenderingReplacesTheRememberedOneAtOnce(t *testing.T) {
	at := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)

	c := newCollector(runningGame(), newSystem())
	c.now = func() time.Time { return at }
	c.system.(*fakeSystem).foreground = 4242

	var asked []string
	c.UseGameIdentity(cyberpunkIdentity(&asked))
	c.Collect()

	c.rtss = fakeRTSS{snap: rtss.Snapshot{
		Version: 0x00020007,
		Entries: []rtss.Entry{{
			ProcessID: 5150, Path: `D:\Games\TheAscent.exe`,
			Time0: 9000, Time1: 10_000, Frames: 60, FrameTimeUs: 16_000,
		}},
	}}
	c.system.(*fakeSystem).foreground = 5150
	at = at.Add(time.Second)

	got := c.Collect()
	if got.Game() != "TheAscent.exe" {
		t.Errorf("Game = %q, want the game that is actually rendering", got.Game())
	}
	if last := asked[len(asked)-1]; last != `D:\Games\TheAscent.exe` {
		t.Errorf("the identification was asked about %q, want the new game", last)
	}
}

// Nothing to hold is not something to hold. A machine that has not run a game
// since it started must not acquire one from an empty memory.
func TestNothingIsHeldWhenNoGameHasEverRun(t *testing.T) {
	c := newCollector(idleRTSS(), newSystem())
	c.UseGameIdentity(cyberpunkIdentity(nil))

	got := c.Collect()
	if got.Game() != NoGame {
		t.Errorf("Game = %q with no game ever seen, want %q", got.Game(), NoGame)
	}
	if got.Has(metrics.GameDetails.ID) {
		t.Error("details appeared for a game that never ran")
	}
}
