// Package gameid works out which game is being rendered, and what the stores
// call it.
//
// RTSS reports the executable — SpaceMarine2.exe — which is what the game
// measurement has always published. This package turns that into the title a
// person would recognise and, where one exists, the Steam app id that addresses
// the artwork:
//
//	https://cdn.cloudflare.steamstatic.com/steam/apps/<AppID>/header.jpg
//
// Three sources, asked in this order, cheapest first:
//
//	Steam    HKCU\Software\Valve\Steam\RunningAppID names the app Steam
//	         launched, and ...\Steam\Apps\<id>\Name spells its title. Two
//	         registry reads: no elevation, no access to the game's process, and
//	         nothing leaves the machine. For a Steam game that is the whole
//	         answer.
//	GOG and  Their catalogues say which folder every installed game lives in, so
//	Epic     the executable path RTSS reports can be matched against them. Still
//	         local, still free.
//	Store    One search against Steam's public store endpoint, once per term,
//	         remembered including the misses. This is the only step that leaves
//	         the machine, and the only one the user has to switch on. It is
//	         asked in two situations: for the app id of a title one of the two
//	         above named, and — where none of them named anything at all — for
//	         a term worked out from the executable itself.
//
// That last case is the only guess in the package, and it is a guess about what
// to *ask*, never about what to report. Cyberpunk2077.exe becomes the question
// "Cyberpunk 2077"; what gets published is the title the store answers with and
// the id it gave, or nothing. A game bought outside Steam, GOG and Epic
// otherwise has no chance of an app id at all, and an app id is what fetches
// the artwork.
//
// Two ways of asking Steam were measured and rejected. steam_appid.txt exists
// in three of the installed games on the development machine, because it is a
// developer file rather than something every game ships. Reading the SteamAppId
// environment variable out of the game's process needs ReadProcessMemory
// against a process that may be running elevated, which is both fragile and
// exactly the shape a virus scanner looks for.
package gameid

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// The launchers this package can name. They are published as an attribute, so
// they are identifiers rather than words: lower case, never translated.
const (
	PlatformSteam = "steam"
	PlatformGOG   = "gog"
	PlatformEpic  = "epic"
)

// Game is one running title as a launcher and the store describe it.
//
// Every field is allowed to be empty, and an empty field means "not known"
// rather than "empty". Nothing here is ever filled in with a placeholder: a
// wrong app id is not a missing picture, it is the wrong game's picture.
type Game struct {
	// Platform is the launcher the game belongs to, one of the constants above.
	Platform string
	// Title is the game as its store spells it — "Warhammer 40,000: Space
	// Marine 2" where the process is only ever SpaceMarine2.exe.
	Title string
	// AppID identifies the title to Steam. Present for a Steam game without
	// anything being asked, and for another launcher's game only once the store
	// search has answered.
	AppID string
}

// Registry is the small part of the Windows registry this needs, kept behind an
// interface so the logic above it can be tested without Steam installed.
//
// Both methods answer "not there" with false rather than an error: a missing
// key is the normal state on a machine without Steam, and is not a fault worth
// distinguishing from a machine that has never run a game.
type Registry interface {
	String(path, name string) (string, bool)
	Uint(path, name string) (uint64, bool)
}

const (
	steamKey = `Software\Valve\Steam`
	appsKey  = `Software\Valve\Steam\Apps`
)

// Running reports the game Steam currently has open.
//
// The second result is false when nothing is running, when Steam is not
// installed, or when the value cannot be read as an app id. All three are the
// same answer as far as a caller is concerned: this source has nothing to
// contribute, and the measurement should be left out rather than filled in.
func Running(reg Registry) (Game, bool) {
	if reg == nil {
		return Game{}, false
	}

	id, ok := reg.Uint(steamKey, "RunningAppID")
	if !ok || id == 0 {
		return Game{}, false
	}

	appID := strconv.FormatUint(id, 10)

	// A missing name loses the title but keeps the id, which is the half that
	// fetches the artwork. Dropping the whole reading over a cosmetic gap would
	// be the worse trade.
	name, _ := reg.String(appsKey+`\`+appID, "Name")

	return Game{Platform: PlatformSteam, Title: name, AppID: appID}, true
}

// Install is one game a launcher has on disk: the title it calls the game, and
// the folder it put it in.
//
// Only launchable games belong here. Both GOG and Epic list add-ons in the same
// catalogue as the games they extend, pointing at the same folder — three
// entries share "Cyberpunk 2077" on the development machine. Each launcher
// marks the difference in its own way, and both were measured rather than
// guessed:
//
//	GOG   an add-on has dependsOn set to its base game, and no exe.
//	Epic  an add-on has no LaunchExecutable. MainGameAppName looks like the
//	      obvious test and is not one: DOOM 64 leaves it empty and is a game.
//
// Getting this wrong is not a missing icon, it is the wrong one: searching the
// store for "Cyberpunk 2077: Phantom Liberty" returns the expansion's own app
// id, and Home Assistant would confidently show the wrong artwork.
type Install struct {
	Platform string
	Name     string
	Dir      string
}

// normalise makes two paths from different launchers comparable: one separator,
// one case, no trailing separator. Measured spellings that made this necessary:
// "D:\games/Borderlands3" and "Q:/Games/AssassinsCreedSyndicate/".
func normalise(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	p = strings.TrimRight(p, `\`)
	return strings.ToLower(p)
}

// MatchInstall finds the game an executable belongs to.
//
// The longest matching folder wins, so a game inside another game's directory
// is reported as itself rather than as its host. A tie is impossible: two
// installs with the same folder would have to be the same length, and the first
// is then as good an answer as the second.
func MatchInstall(installs []Install, exePath string) (Install, bool) {
	exe := normalise(exePath)

	var best Install
	found := false
	for _, in := range installs {
		dir := normalise(in.Dir)
		if dir == "" {
			continue
		}
		// The separator matters: without it "…\DOOM" matches "…\DOOM64\x.exe".
		if !strings.HasPrefix(exe, dir+`\`) {
			continue
		}
		if !found || len(dir) > len(normalise(best.Dir)) {
			best, found = in, true
		}
	}
	return best, found
}

// Match is what a store search came back with: the id that addresses the game,
// and the title the store keeps for it.
//
// The title is carried alongside the id because the two are worth different
// things depending on how the term was arrived at. Where a launcher named the
// game, the launcher's spelling is the one to publish and only the id is new.
// Where the term was worked out from a file name, the store's spelling is the
// only spelling anybody checked.
type Match struct {
	AppID string
	Title string
}

// Search asks a store about a term, reporting whether it knew anything.
//
// Taken as a function so the cache can be tested without a network, and so the
// caller decides whether asking a store is allowed at all — this is the only
// part of the whole mechanism that leaves the machine.
type Search func(term string) (Match, bool)

// catalogueTTL is how stale the launcher catalogues may get before an
// executable nobody recognises is worth a second look.
//
// A game installed while this program runs is otherwise invisible until it is
// restarted, and "I installed it, why is it not there" is a bug report nobody
// can act on. It costs a registry enumeration and a handful of small files, and
// only for an executable that is not already known either way — a browser RTSS
// happens to hook is answered from the miss cache and never reaches this.
const catalogueTTL = 5 * time.Minute

// Identifier answers "what is this executable, as its store would name it", and
// remembers everything it has already worked out.
//
// The caching is the whole point. Identify runs on every poll — twice a second
// by default — for as long as a game is open, and every source below it is
// either a registry enumeration, a directory of small files, or a request to
// somebody else's server. Each answer is therefore taken once: per executable
// path for the launcher catalogues, per title for the store, and misses exactly
// like hits, because a miss is the case that would otherwise repeat forever.
//
// In memory only, deliberately: the answers are cheap to fetch again after a
// restart, and a cache on disk is a file to invalidate, migrate and explain.
type Identifier struct {
	reg      Registry
	installs func() []Install
	search   Search
	now      func() time.Time

	// wg covers the store lookups, which run off this goroutine. Only the tests
	// wait on it; the program simply lets them finish or lets the process end.
	wg sync.WaitGroup

	mu sync.Mutex
	// byPath is the launcher answer per executable, with a zero Install for a
	// path no launcher claims.
	byPath map[string]Install
	// matches is the store answer per term, a zero Match for a term it has
	// nothing for.
	matches map[string]Match
	// asking marks terms the store is being asked about right now, so a slow
	// answer cannot turn into one request per poll.
	asking map[string]bool

	catalogue []Install
	loadedAt  time.Time
}

// New builds an identifier from the three sources, any of which may be nil —
// a nil source is simply one that never answers.
func New(reg Registry, installs func() []Install, search Search) *Identifier {
	return &Identifier{
		reg:      reg,
		installs: installs,
		search:   search,
		now:      time.Now,
		byPath:   map[string]Install{},
		matches:  map[string]Match{},
		asking:   map[string]bool{},
	}
}

// Identify names the game an executable belongs to.
//
// Steam is asked first and its answer is taken whole, because it is the only
// source that knows both halves without anything being read, matched or asked.
// The known limitation of that: Steam reports the game it launched, not the
// window in front, so a Steam game left running in the background outranks
// whatever RTSS is currently drawing. That is a rare state, and the alternative
// — guessing from the path which of two running games is meant — is worse.
//
// The store is never waited for. A title whose app id has not arrived yet is
// reported without one, and the id appears on a later reading; a measurement
// loop that blocks on somebody else's server would turn a slow store into a
// slow exporter.
func (i *Identifier) Identify(exePath string) (Game, bool) {
	if game, ok := Running(i.reg); ok {
		return game, true
	}

	if install, ok := i.install(exePath); ok {
		// The launcher's spelling wins. It named the game on this machine, and
		// the store is only being asked for the id.
		found, _ := i.match(install.Name)
		return Game{
			Platform: install.Platform,
			Title:    install.Name,
			AppID:    found.AppID,
		}, true
	}

	// Nothing on this machine claims this executable: bought somewhere with no
	// catalogue to read, installed by hand, or run from a launcher this program
	// cannot see. All that is left is the file name, so it is turned into a
	// search term and the store is asked.
	//
	// Nothing derived from the file name is published. If the store answers,
	// what goes out is the title the store spells and the id it gave; if it does
	// not, there is no reading. The guess is a question, and only the answer is
	// reported.
	//
	// The platform is Steam. Not because Steam launched the game — it did not,
	// and it may not even be installed — but because Steam's catalogue is what
	// answered, and the platform names the source of the identity rather than
	// the shop the game was bought from. It is also what the app id addresses:
	// reporting an app id with no platform beside it would leave a reader to
	// work out for themselves which store that number belongs to.
	term, ok := searchTerm(exePath)
	if !ok {
		return Game{}, false
	}
	found, ok := i.match(term)
	if !ok {
		return Game{}, false
	}
	return Game{Platform: PlatformSteam, Title: found.Title, AppID: found.AppID}, true
}

// install matches a path against the launcher catalogues, once per path.
func (i *Identifier) install(exePath string) (Install, bool) {
	if strings.TrimSpace(exePath) == "" {
		return Install{}, false
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if known, seen := i.byPath[exePath]; seen {
		return known, known.Name != ""
	}

	hit, ok := MatchInstall(i.catalogueLocked(), exePath)
	// A path nothing claims is worth one reread of a stale catalogue: this is
	// what a game installed since the last one looks like.
	if !ok && i.now().Sub(i.loadedAt) > catalogueTTL {
		i.reloadLocked()
		hit, ok = MatchInstall(i.catalogue, exePath)
	}
	if !ok {
		hit = Install{}
	}
	i.byPath[exePath] = hit
	return hit, ok
}

func (i *Identifier) catalogueLocked() []Install {
	if i.loadedAt.IsZero() {
		i.reloadLocked()
	}
	return i.catalogue
}

func (i *Identifier) reloadLocked() {
	i.loadedAt = i.now()
	if i.installs == nil {
		i.catalogue = nil
		return
	}
	i.catalogue = i.installs()
}

// match is the store's answer for a term: the one it already gave, or nothing
// while it is being asked for the first and only time.
//
// The second result is false both for a term the store does not know and for a
// term it has not answered yet. A caller that has nothing else to publish must
// treat the two the same anyway — neither is a reading — and the next poll
// tells them apart for free.
func (i *Identifier) match(term string) (Match, bool) {
	if strings.TrimSpace(term) == "" {
		return Match{}, false
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if found, answered := i.matches[term]; answered {
		return found, found.AppID != ""
	}
	if i.search == nil || i.asking[term] {
		return Match{}, false
	}

	i.asking[term] = true
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()

		found, ok := i.search(term)
		if !ok {
			// Remembered as an empty answer rather than forgotten. A game the
			// store does not have is the case that would otherwise ask again on
			// every single poll, for as long as it is open.
			found = Match{}
		}

		i.mu.Lock()
		i.matches[term] = found
		delete(i.asking, term)
		i.mu.Unlock()
	}()
	return Match{}, false
}

// wait blocks until every store lookup has finished. For the tests: nothing in
// the program has a reason to wait for an answer it will get on the next poll.
func (i *Identifier) wait() { i.wg.Wait() }
