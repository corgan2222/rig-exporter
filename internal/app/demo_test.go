//go:build windows

package app

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The demo is only worth having if what it pretends arrives the way a real game
// does: through the collector, into the same readings every export is rendered
// from. This is that path end to end, minus the tray that flips the switch.
func TestTheDemoReachesTheReadingsLikeARealGame(t *testing.T) {
	application, _ := newTestApp(t, 500, 500)
	d := application.EnableDemo()

	// Enabled but not switched on: the reading is whatever the machine says,
	// and on a test machine that is never one of the demo's titles.
	if game := application.collector.Collect().Game(); game == "GTA5.exe" {
		t.Fatal("the demo answered before anybody switched it on")
	}

	d.SetActive(true)
	d.SetTitle(1)
	d.SetPlatform("epic")
	snap := application.collector.Collect()

	if snap.Game() != "GTA5.exe" {
		t.Errorf("game = %q, want GTA5.exe", snap.Game())
	}
	if !snap.Rendering() {
		t.Errorf("fps = %v, running = %v — want a game that is rendering", snap.FPS(), snap.GameRunning())
	}
	// Published although the game identification is off in this configuration:
	// a demo without a title and an app id would show very little.
	for detail, want := range map[string]string{
		metrics.DetailPlatform: "epic",
		metrics.DetailTitle:    "Grand Theft Auto V",
		metrics.DetailAppID:    "271590",
	} {
		if got := snap.GameDetail(detail); got != want {
			t.Errorf("%s = %q, want %q", detail, got, want)
		}
	}
}
