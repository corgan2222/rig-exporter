package gameid

import (
	"sync"
	"testing"
	"time"
)

// fakeRegistry stands in for the real one so the logic can be tested without a
// Steam installation. The real reader is the only Windows-specific part.
type fakeRegistry map[string]string

func (f fakeRegistry) String(path, name string) (string, bool) {
	v, ok := f[path+`\`+name]
	return v, ok
}

func (f fakeRegistry) Uint(path, name string) (uint64, bool) {
	v, ok := f[path+`\`+name]
	if !ok {
		return 0, false
	}
	var n uint64
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + uint64(r-'0')
	}
	return n, true
}

func TestNoGameWhenSteamReportsZero(t *testing.T) {
	// Steam writes 0 into RunningAppID whenever no game is up. Measured on a
	// real installation with nothing running.
	reg := fakeRegistry{steamKey + `\RunningAppID`: "0"}

	if game, ok := Running(reg); ok {
		t.Fatalf("a game was reported while Steam said none: %+v", game)
	}
}

func TestNoGameWithoutSteam(t *testing.T) {
	// A machine without Steam has no key at all. That is not an error, it is
	// an answer: this source has nothing to say.
	if game, ok := Running(fakeRegistry{}); ok {
		t.Fatalf("a game was reported without Steam installed: %+v", game)
	}
}

func TestTheRunningGameCarriesItsIDAndTitle(t *testing.T) {
	reg := fakeRegistry{
		steamKey + `\RunningAppID`:   "2183900",
		appsKey + `\2183900\Name`:    "Warhammer 40,000: Space Marine 2",
		appsKey + `\2183900\Running`: "1",
	}

	game, ok := Running(reg)
	if !ok {
		t.Fatal("no game reported while Steam named one")
	}
	if game.AppID != "2183900" {
		t.Errorf("AppID = %q, want %q", game.AppID, "2183900")
	}
	if game.Title != "Warhammer 40,000: Space Marine 2" {
		t.Errorf("Title = %q, want the full title", game.Title)
	}
	if game.Platform != PlatformSteam {
		t.Errorf("Platform = %q, want %q", game.Platform, PlatformSteam)
	}
}

func TestTheIDSurvivesAMissingTitle(t *testing.T) {
	// Steam does not always keep a Name for an app. The id is the useful part
	// — it is what fetches the artwork — so a missing title must not throw the
	// whole reading away.
	reg := fakeRegistry{steamKey + `\RunningAppID`: "730"}

	game, ok := Running(reg)
	if !ok {
		t.Fatal("the reading was dropped because the title was missing")
	}
	if game.AppID != "730" || game.Title != "" {
		t.Errorf("got %+v, want the id alone", game)
	}
}

func TestAnUnreadableIDIsNotAGame(t *testing.T) {
	// Anything that is not a plain number is not an AppID. Reporting it would
	// send Home Assistant looking for artwork that cannot exist.
	reg := fakeRegistry{steamKey + `\RunningAppID`: "not-a-number"}

	if game, ok := Running(reg); ok {
		t.Fatalf("a non-numeric value was accepted: %+v", game)
	}
}

func TestTheLongerPathWins(t *testing.T) {
	// Two games where one lives inside the other's folder. The longer match is
	// the more specific one and must win; the shorter would swallow it.
	installs := []Install{
		{Name: "Launcher Collection", Dir: `Q:\Games\Epic Games`},
		{Name: "DOOM 64", Dir: `Q:\Games\Epic Games\DOOM64`},
	}

	got, ok := MatchInstall(installs, `Q:\Games\Epic Games\DOOM64\DOOM64_x64.exe`)
	if !ok {
		t.Fatal("no match for a path that is inside a known install")
	}
	if got.Name != "DOOM 64" {
		t.Errorf("Name = %q, want the more specific install", got.Name)
	}
}

func TestSeparatorsAndCaseDoNotMatter(t *testing.T) {
	// Measured: Epic writes "D:\games/Borderlands3" and Ubisoft
	// "Q:/Games/AssassinsCreedSyndicate/" — mixed separators, sometimes a
	// trailing one. Without normalising, the comparison fails silently.
	installs := []Install{{Name: "Cyberpunk 2077", Dir: `Q:/Games/GoG/Cyberpunk 2077/`}}

	if _, ok := MatchInstall(installs, `q:\games\gog\cyberpunk 2077\bin\x64\Cyberpunk2077.exe`); !ok {
		t.Error("a path that differs only in separators and case did not match")
	}
}

func TestAFolderPrefixIsNotAMatch(t *testing.T) {
	// "…\Doom" must not match "…\Doom64\game.exe". Comparing strings without
	// respecting the separator is the classic way to get that wrong.
	installs := []Install{{Name: "DOOM", Dir: `Q:\Games\DOOM`}}

	if got, ok := MatchInstall(installs, `Q:\Games\DOOM64\DOOM64_x64.exe`); ok {
		t.Errorf("matched %q on a partial folder name", got.Name)
	}
}

func TestAnUnknownPathMatchesNothing(t *testing.T) {
	installs := []Install{{Name: "DOOM 64", Dir: `Q:\Games\Epic Games\DOOM64`}}

	if _, ok := MatchInstall(installs, `C:\Windows\explorer.exe`); ok {
		t.Error("a path outside every install was matched")
	}
}

// countingSearch stands in for the Steam store, and records how often it was
// asked so the cache can be proven rather than assumed. It is called from the
// identifier's own goroutine, hence the lock.
type countingSearch struct {
	mu     sync.Mutex
	calls  int
	answer map[string]string
	// titles is what the store calls each term it knows, where a test cares.
	titles map[string]string
}

func (c *countingSearch) lookup(term string) (Match, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls++
	id, ok := c.answer[term]
	if !ok {
		return Match{}, false
	}
	// The store answers with a spelling of its own. Echoing the term back would
	// hide the case this distinction exists for: a term worked out from a file
	// name is a guess, and the title that gets published has to be the store's.
	return Match{AppID: id, Title: c.titles[term]}, true
}

func (c *countingSearch) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// countingCatalogue is the launcher catalogue, counting how often it is read.
type countingCatalogue struct {
	reads    int
	installs []Install
}

func (c *countingCatalogue) read() []Install {
	c.reads++
	return c.installs
}

const cyberpunkExe = `Q:\Games\GoG\Cyberpunk 2077\bin\x64\Cyberpunk2077.exe`

func cyberpunkCatalogue() *countingCatalogue {
	return &countingCatalogue{installs: []Install{
		{Platform: PlatformGOG, Name: "Cyberpunk 2077", Dir: `Q:\Games\GoG\Cyberpunk 2077`},
	}}
}

func TestSteamAnswersWithoutAnythingElseBeingAsked(t *testing.T) {
	// The cheapest source wins: two registry reads name both halves, so
	// neither the launcher catalogues nor the store may be touched at all.
	reg := fakeRegistry{
		steamKey + `\RunningAppID`: "979690",
		appsKey + `\979690\Name`:   "The Ascent",
	}
	catalogue := cyberpunkCatalogue()
	search := &countingSearch{answer: map[string]string{}}

	ident := New(reg, catalogue.read, search.lookup)
	game, ok := ident.Identify(cyberpunkExe)
	if !ok {
		t.Fatal("Steam named a game and nothing was reported")
	}
	if game.Platform != PlatformSteam || game.Title != "The Ascent" || game.AppID != "979690" {
		t.Errorf("got %+v, want Steam's own answer", game)
	}
	if catalogue.reads != 0 || search.count() != 0 {
		t.Errorf("catalogue read %d times and the store asked %d times for a Steam game",
			catalogue.reads, search.count())
	}
}

func TestALauncherGameIsNamedFromItsCatalogue(t *testing.T) {
	catalogue := cyberpunkCatalogue()
	search := &countingSearch{answer: map[string]string{"Cyberpunk 2077": "1091500"}}

	ident := New(fakeRegistry{}, catalogue.read, search.lookup)

	game, ok := ident.Identify(cyberpunkExe)
	if !ok {
		t.Fatal("a game inside a known GOG install was not identified")
	}
	if game.Platform != PlatformGOG || game.Title != "Cyberpunk 2077" {
		t.Errorf("got %+v, want the GOG catalogue's answer", game)
	}
	// The store has not answered yet, and an app id that has not arrived is
	// absent rather than guessed.
	if game.AppID != "" {
		t.Errorf("AppID = %q before the store answered", game.AppID)
	}

	ident.wait()
	if game, _ := ident.Identify(cyberpunkExe); game.AppID != "1091500" {
		t.Errorf("AppID = %q after the store answered, want 1091500", game.AppID)
	}
}

func TestAPathNoLauncherClaimsIsNotAGame(t *testing.T) {
	// RTSS hooks whatever renders, including a browser. Nothing may be
	// invented for it — no platform, no title, no id.
	//
	// "No launcher claims it" is no longer why: an executable nothing claims is
	// now turned into a search term on purpose. What keeps a browser out is that
	// it is on the list of programs that are never games, and that list is what
	// this checks.
	catalogue := cyberpunkCatalogue()
	search := &countingSearch{answer: map[string]string{}}

	ident := New(fakeRegistry{}, catalogue.read, search.lookup)

	if game, ok := ident.Identify(`C:\Program Files\Mozilla Firefox\firefox.exe`); ok {
		t.Fatalf("a game was reported for %+v", game)
	}
	if search.count() != 0 {
		t.Error("the store was asked about a browser")
	}
}

func TestATitleIsLookedUpOnlyOnce(t *testing.T) {
	catalogue := cyberpunkCatalogue()
	search := &countingSearch{answer: map[string]string{"Cyberpunk 2077": "1091500"}}

	ident := New(fakeRegistry{}, catalogue.read, search.lookup)

	for range 5 {
		ident.Identify(cyberpunkExe)
		ident.wait()
	}

	if search.count() != 1 {
		t.Errorf("the store was asked %d times, want 1", search.count())
	}
}

func TestAMissIsRememberedToo(t *testing.T) {
	// The case that would otherwise hammer the store: a game Steam does not
	// have. Without remembering the miss, every single poll asks again.
	catalogue := &countingCatalogue{installs: []Install{
		{Platform: PlatformEpic, Name: "Some Itch.io Game", Dir: `Q:\Games\Itch`},
	}}
	search := &countingSearch{answer: map[string]string{}}

	ident := New(fakeRegistry{}, catalogue.read, search.lookup)

	for range 5 {
		game, ok := ident.Identify(`Q:\Games\Itch\game.exe`)
		if !ok || game.AppID != "" {
			t.Fatalf("got %+v, %v — want the title without an id", game, ok)
		}
		ident.wait()
	}

	if search.count() != 1 {
		t.Errorf("the store was asked %d times for a miss, want 1", search.count())
	}
}

func TestAnEmptyTitleIsNeverLookedUp(t *testing.T) {
	catalogue := &countingCatalogue{installs: []Install{
		{Platform: PlatformEpic, Name: "  ", Dir: `Q:\Games\Nameless`},
	}}
	search := &countingSearch{answer: map[string]string{}}

	ident := New(fakeRegistry{}, catalogue.read, search.lookup)
	ident.Identify(`Q:\Games\Nameless\game.exe`)
	ident.wait()

	if search.count() != 0 {
		t.Error("the store was asked for a title that is only whitespace")
	}
}

func TestTheCatalogueIsReadOncePerUnknownPath(t *testing.T) {
	// Identify runs on every poll. Reading the launcher catalogues each time
	// would mean a registry enumeration and a directory of files twice a
	// second, for an answer that cannot have changed.
	catalogue := cyberpunkCatalogue()
	ident := New(fakeRegistry{}, catalogue.read, nil)

	for range 10 {
		ident.Identify(cyberpunkExe)
		ident.Identify(`C:\Windows\explorer.exe`)
	}

	if catalogue.reads != 1 {
		t.Errorf("the catalogue was read %d times, want 1", catalogue.reads)
	}
}

func TestAGameInstalledLaterIsFoundOnceTheCatalogueIsStale(t *testing.T) {
	// A launcher catalogue read at startup does not know about a game
	// installed an hour later, and "I installed it, why is it not there" is a
	// report nobody can act on. A path nobody claims is worth one reread — but
	// only after the catalogue has had time to go stale.
	catalogue := &countingCatalogue{}
	ident := New(fakeRegistry{}, catalogue.read, nil)

	clock := time.Now()
	ident.now = func() time.Time { return clock }

	if _, ok := ident.Identify(cyberpunkExe); ok {
		t.Fatal("a game was found in an empty catalogue")
	}

	catalogue.installs = cyberpunkCatalogue().installs
	clock = clock.Add(catalogueTTL + time.Minute)

	// The same path again is answered from the miss, without a reread: that
	// decision was taken and remembered.
	if _, ok := ident.Identify(cyberpunkExe); ok {
		t.Error("a path already answered was looked at again")
	}
	if catalogue.reads != 1 {
		t.Errorf("the catalogue was read %d times for a path already answered, want 1", catalogue.reads)
	}

	// A path that has never been seen is what a newly installed game looks
	// like, and that is worth the reread.
	game, ok := ident.Identify(`Q:\Games\GoG\Cyberpunk 2077\bin\x64\Cyberpunk2077_dx12.exe`)
	if !ok {
		t.Fatal("a game installed after the catalogue was read stayed invisible")
	}
	if game.Title != "Cyberpunk 2077" {
		t.Errorf("Title = %q", game.Title)
	}
	if catalogue.reads != 2 {
		t.Errorf("the catalogue was read %d times, want 2", catalogue.reads)
	}
}

func TestTheStoreIsNeverWaitedFor(t *testing.T) {
	// A slow store must not become a slow exporter: Identify runs inside the
	// measurement loop, and the id it does not have yet arrives on a later
	// reading instead.
	blocked := make(chan struct{})
	done := make(chan struct{})
	defer func() {
		close(blocked)
		<-done
	}()

	search := func(string) (Match, bool) {
		<-blocked
		close(done)
		return Match{AppID: "1091500", Title: "Cyberpunk 2077"}, true
	}

	ident := New(fakeRegistry{}, cyberpunkCatalogue().read, search)

	answered := make(chan Game, 1)
	go func() {
		game, _ := ident.Identify(cyberpunkExe)
		answered <- game
	}()

	select {
	case game := <-answered:
		if game.Title != "Cyberpunk 2077" || game.AppID != "" {
			t.Errorf("got %+v, want the title without an id", game)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Identify waited for the store")
	}
}

// A game bought outside Steam, GOG and Epic — or installed by hand — is claimed
// by no catalogue on the machine, and used to produce no reading at all. The
// executable is the only thing left, so it becomes a question for the store.
//
// What the store answers with is what gets published. The term that was asked,
// "Space Marine 2", is a guess assembled from a file name; the title on the
// reading is the store's own spelling, which is the one somebody would
// recognise and the only one anybody checked.
func TestAGameNoLauncherClaimsIsLookedUpByName(t *testing.T) {
	search := &countingSearch{
		answer: map[string]string{"Space Marine 2": "2183900"},
		titles: map[string]string{"Space Marine 2": "Warhammer 40,000: Space Marine 2"},
	}

	ident := New(fakeRegistry{}, cyberpunkCatalogue().read, search.lookup)
	exe := `M:\Handinstalliert\SpaceMarine2.exe`

	// The first look asks and reports nothing: the store runs off this
	// goroutine, and a measuring loop never waits for somebody else's server.
	if _, ok := ident.Identify(exe); ok {
		t.Error("something was reported before the store had answered")
	}
	ident.wait()

	game, ok := ident.Identify(exe)
	if !ok {
		t.Fatal("the store answered and nothing was reported")
	}
	if game.Title != "Warhammer 40,000: Space Marine 2" {
		t.Errorf("Title = %q, want the store's spelling rather than the term asked", game.Title)
	}
	if game.AppID != "2183900" {
		t.Errorf("AppID = %q, want 2183900", game.AppID)
	}
	// No launcher said anything, so there is no platform to report. An empty
	// field is left out of every export rather than filled in — which is right,
	// because nobody measured it.
	if game.Platform != "" {
		t.Errorf("Platform = %q, want empty: no launcher claimed this game", game.Platform)
	}
}

// The guess is a question, not an answer. A store that knows nothing leaves the
// reading absent rather than publishing a title assembled from a file name.
func TestAnUnknownExecutableStaysUnreportedWhenTheStoreHasNothing(t *testing.T) {
	search := &countingSearch{answer: map[string]string{}}

	ident := New(fakeRegistry{}, cyberpunkCatalogue().read, search.lookup)
	exe := `M:\Handinstalliert\SomethingObscure.exe`

	for range 5 {
		if game, ok := ident.Identify(exe); ok {
			t.Fatalf("got %+v, want nothing: the store does not have this game", game)
		}
		ident.wait()
	}

	// Once, not five times. A miss is the case that would otherwise ask again on
	// every poll for as long as the game is open.
	if search.count() != 1 {
		t.Errorf("the store was asked %d times, want 1", search.count())
	}
}

// RTSS hooks whatever presents frames, which is how a browser ends up here. The
// store must not be asked about it at all — not because the answer would be
// wrong, but because the name has no business leaving the machine.
func TestAProgramThatIsNeverAGameNeverReachesTheStore(t *testing.T) {
	search := &countingSearch{answer: map[string]string{}}

	ident := New(fakeRegistry{}, cyberpunkCatalogue().read, search.lookup)

	for _, exe := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Steam\steam.exe`,
	} {
		if game, ok := ident.Identify(exe); ok {
			t.Errorf("%s was reported as %+v", exe, game)
		}
	}
	ident.wait()

	if search.count() != 0 {
		t.Errorf("the store was asked %d times about a program that is not a game", search.count())
	}
}
