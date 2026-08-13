package gameid

import (
	"path/filepath"
	"strings"
	"unicode"
)

// searchTerm turns an executable into something worth asking a store about.
//
// This is the last resort, for a game no launcher on this machine claims:
// bought somewhere else, installed by hand, or run from a launcher this program
// cannot read. All that is left is the file name, and RTSS reports plenty of
// them that are not games at all.
//
// What comes out is a *query*, never a published value. Nothing derived here
// reaches a reading: if the store answers, the title that gets published is the
// one the store spells, and if it does not, nothing is published at all. That is
// the whole reason a guess is allowed to be this rough — it is checked against
// somebody who knows.
//
// The second result is false for a name not worth asking about.
func searchTerm(exePath string) (string, bool) {
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(exePath, `\`, "/")))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" {
		return "", false
	}

	name = trimBuildSuffix(name)
	if notAGame[strings.ToLower(name)] {
		return "", false
	}

	term := strings.Join(strings.Fields(spaceOut(name)), " ")

	// Two characters is not a title, and a term with no letter at all is a
	// version number or a build id that would match anything.
	if len(term) < 3 || !strings.ContainsFunc(term, unicode.IsLetter) {
		return "", false
	}
	return term, true
}

// buildSuffixes are what engines append to the name of the game. Unreal is the
// reason this exists at all: it ships MyGame-Win64-Shipping.exe, and asking a
// store about that returns nothing rather than the wrong thing — but nothing is
// still a game the reader could have had.
//
// Longest first, so "-Win64-Shipping" is taken off whole rather than leaving a
// "-Win64" behind.
var buildSuffixes = []string{
	"-WinGDK-Shipping", "-Win64-Shipping", "-Win32-Shipping",
	"-Win64", "-Win32", "-Shipping",
	"_x64", "-x64", "_x86", "-x86", "_win64", "_win32", "_64",
}

func trimBuildSuffix(name string) string {
	for _, suffix := range buildSuffixes {
		if len(name) > len(suffix) && strings.EqualFold(name[len(name)-len(suffix):], suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}

// notAGame is what RTSS hooks that never is one: it hooks whatever presents
// frames, and a browser, a chat window and a capture program all do.
//
// This list is not the safety net — the store's answer is, because a title
// nobody sells produces no reading. It is here so those names are not sent to
// somebody else's server at all, and so a program called "Origin" cannot come
// back as a game called Origin.
//
// Generic names are in it for a different reason: "game" and "launcher" would
// each match something, and whatever they matched would be wrong.
var notAGame = map[string]bool{
	// Browsers.
	"chrome": true, "firefox": true, "msedge": true, "opera": true,
	"opera_gx": true, "brave": true, "vivaldi": true, "iexplore": true,
	// Chat, capture, media.
	"discord": true, "slack": true, "teams": true, "spotify": true,
	"obs": true, "obs32": true, "obs64": true, "vlc": true, "mpc-hc64": true,
	"streamlabs obs": true,
	// Launchers and monitors, including the two this program reads.
	"steam": true, "steamwebhelper": true, "epicgameslauncher": true,
	"galaxyclient": true, "battle.net": true, "origin": true,
	"eadesktop": true, "ubisoftconnect": true, "uplay": true,
	"rtss": true, "rtsshooksloader64": true, "msiafterburner": true,
	"rivatuner": true, "playnite.desktopapp": true, "playnite.fullscreenapp": true,
	// The Windows shell, which RTSS does sometimes attach to.
	"explorer": true, "dwm": true, "applicationframehost": true,
	"shellexperiencehost": true, "searchapp": true, "textinputhost": true,
	// Names too generic to mean anything.
	"game": true, "launcher": true, "start": true, "main": true,
	"app": true, "client": true, "bin": true, "run": true, "play": true,
}

// spaceOut puts the spaces back that a file name cannot contain.
//
// Three boundaries, each one measured against a real executable on the
// development machine:
//
//	lower → upper   TheAscent      becomes The Ascent
//	letter → digit  Cyberpunk2077  becomes Cyberpunk 2077
//	upper run → word DOOMEternal   becomes DOOM Eternal
//
// The third is the one that is easy to miss: without it an all-caps title runs
// into the word after it, and "DOOMEternal" matches nothing.
func spaceOut(name string) string {
	runes := []rune(strings.ReplaceAll(strings.ReplaceAll(name, "_", " "), "-", " "))

	var b strings.Builder
	for index, r := range runes {
		if index > 0 && needsSpace(runes, index) {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func needsSpace(runes []rune, index int) bool {
	previous, current := runes[index-1], runes[index]
	switch {
	case unicode.IsUpper(current) && unicode.IsLower(previous):
		return true
	case unicode.IsDigit(current) && unicode.IsLetter(previous):
		return true
	case unicode.IsLetter(current) && unicode.IsDigit(previous):
		return true
	case unicode.IsUpper(current) && unicode.IsUpper(previous) &&
		index+1 < len(runes) && unicode.IsLower(runes[index+1]):
		return true
	}
	return false
}
