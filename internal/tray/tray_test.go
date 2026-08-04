//go:build windows

package tray

import (
	"strings"
	"testing"

	"github.com/corgan/rig-exporter/internal/config"
)

// The top entry is the one somebody clicks to reach the interface, so it says
// where the interface is — and it has to be the port that actually works, not
// the configured one. The web server falls back to a random port when the
// configured one is taken.
func TestTheTopEntryNamesTheAddressThatWorks(t *testing.T) {
	tray := &Tray{opts: Options{SettingsURL: func() string { return "http://127.0.0.1:48352" }}}

	title := headerTitle(tray.address())

	if !strings.Contains(title, "127.0.0.1:48352") {
		t.Errorf("title %q does not name the address", title)
	}
	if strings.Contains(title, "http://") {
		t.Errorf("title %q carries the scheme, which says nothing in a menu", title)
	}
	if !strings.HasPrefix(title, config.AppName+" "+config.VersionString()) {
		t.Errorf("title %q lost the name and version", title)
	}
}

// The menu is built before the web server has a port. A dangling separator
// would look like something failed.
func TestTheTopEntryOmitsAnAddressItDoesNotHaveYet(t *testing.T) {
	for _, tray := range []*Tray{
		{opts: Options{}},
		{opts: Options{SettingsURL: func() string { return "" }}},
	} {
		title := headerTitle(tray.address())
		if strings.Contains(title, "—") {
			t.Errorf("title %q carries a separator with nothing behind it", title)
		}
		if title != config.AppName+" "+config.VersionString() {
			t.Errorf("title = %q, want just the name and version", title)
		}
	}
}
