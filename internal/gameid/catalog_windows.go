package gameid

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// Installs is every launchable game the other two launchers have on disk.
//
// Steam is absent on purpose: it answers through the registry without a
// catalogue having to be read at all, and a game it launched is already named
// before this is reached.
func Installs() []Install {
	gog := gogInstalls()
	return append(gog, epicInstalls()...)
}

// gogInstalls reads the games GOG Galaxy has on disk.
//
// Add-ons are dropped: they carry dependsOn pointing at the game they extend
// and have no exe of their own. Measured — "Cyberpunk 2077", "Phantom Liberty"
// and "REDmod" all name the same folder, and only the first is a game.
func gogInstalls() []Install {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\WOW6432Node\GOG.com\Games`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()

	ids, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var out []Install
	for _, id := range ids {
		g, err := registry.OpenKey(registry.LOCAL_MACHINE,
			`SOFTWARE\WOW6432Node\GOG.com\Games\`+id, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		depends, _, _ := g.GetStringValue("dependsOn")
		exe, _, _ := g.GetStringValue("exe")
		name, _, _ := g.GetStringValue("gameName")
		path, _, _ := g.GetStringValue("path")
		g.Close()

		if depends != "" || exe == "" || name == "" || path == "" {
			continue
		}
		out = append(out, Install{Platform: PlatformGOG, Name: name, Dir: path})
	}
	return out
}

// epicManifest is the part of an Epic .item file this needs.
type epicManifest struct {
	DisplayName      string `json:"DisplayName"`
	InstallLocation  string `json:"InstallLocation"`
	LaunchExecutable string `json:"LaunchExecutable"`
}

// epicInstalls reads the games the Epic launcher has on disk.
//
// LaunchExecutable is the test for "is this a game": an add-on has none.
// MainGameAppName is the obvious candidate and the wrong one — DOOM 64 leaves
// it empty and is very much a game.
func epicInstalls() []Install {
	dir := filepath.Join(os.Getenv("PROGRAMDATA"), `Epic\EpicGamesLauncher\Data\Manifests`)
	items, err := filepath.Glob(filepath.Join(dir, "*.item"))
	if err != nil {
		return nil
	}

	var out []Install
	for _, item := range items {
		blob, err := os.ReadFile(item)
		if err != nil {
			continue
		}
		var m epicManifest
		if json.Unmarshal(blob, &m) != nil {
			continue
		}
		if m.LaunchExecutable == "" || m.InstallLocation == "" || m.DisplayName == "" {
			continue
		}
		out = append(out, Install{Platform: PlatformEpic, Name: m.DisplayName, Dir: m.InstallLocation})
	}
	return out
}
