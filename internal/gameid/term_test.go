package gameid

import "testing"

// The spellings on the left are how the executables are actually named; the
// spellings on the right are what a store search needs to be given. Nothing
// here is published — this is only what gets asked — which is why a term is
// allowed to be approximate and a title is not.
func TestASearchTermIsWorkedOutFromTheExecutable(t *testing.T) {
	for _, c := range []struct {
		exe  string
		want string
	}{
		{`D:\Games\Cyberpunk2077.exe`, "Cyberpunk 2077"},
		{`D:\Games\TheAscent.exe`, "The Ascent"},
		{`C:\Program Files\DOOMEternal.exe`, "DOOM Eternal"},
		{`E:\DOOM64.exe`, "DOOM 64"},
		// Unreal names the binary after the engine target, not after the game.
		{`D:\Games\Talos2\Talos2-Win64-Shipping.exe`, "Talos 2"},
		{`D:\Games\Satisfactory\FactoryGame-WinGDK-Shipping.exe`, "Factory Game"},
		// Separators are word breaks, not part of the name.
		{`D:\Games\hollow_knight.exe`, "hollow knight"},
		{`D:\Games\baldurs-gate-3.exe`, "baldurs gate 3"},
		// A forward slash is a path separator too — both launchers spell paths
		// both ways, which is why normalise exists elsewhere in this package.
		{`D:/Games/Portal2.exe`, "Portal 2"},
		{`Frostpunk2_x64.exe`, "Frostpunk 2"},
	} {
		got, ok := searchTerm(c.exe)
		if !ok {
			t.Errorf("searchTerm(%q) declined a game", c.exe)
			continue
		}
		if got != c.want {
			t.Errorf("searchTerm(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

// A wrong app id is not a missing picture, it is the wrong game's picture. RTSS
// hooks whatever presents frames, so a browser and a chat window turn up here
// as readily as a game does, and "Origin" would come back as a game called
// Origin. Nothing on this list is ever sent anywhere.
func TestSomeExecutablesAreNeverWorthAskingAbout(t *testing.T) {
	for _, exe := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Users\x\AppData\Local\Discord\app-1.0\Discord.exe`,
		`C:\Program Files (x86)\Steam\steam.exe`,
		`C:\Program Files\Epic Games\Launcher\EpicGamesLauncher.exe`,
		`C:\Program Files\obs-studio\bin\64bit\obs64.exe`,
		`C:\Windows\explorer.exe`,
		// Names that would match something, and whatever they matched would be
		// wrong.
		`D:\Whatever\game.exe`,
		`D:\Whatever\launcher.exe`,
		// Nothing to ask about at all.
		``,
		`D:\x\.exe`,
		`D:\x\ab.exe`,
		`D:\x\2077.exe`,
	} {
		if term, ok := searchTerm(exe); ok {
			t.Errorf("searchTerm(%q) = %q, want no question asked", exe, term)
		}
	}
}
