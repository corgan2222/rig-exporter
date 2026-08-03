//go:build windows

package autostart

import (
	"strings"
	"testing"
)

// The autostart entry has to carry the background flag, or Windows opens a
// browser window at every logon — the fastest way to have autostart switched
// off again.
func TestTheAutostartCommandStaysQuiet(t *testing.T) {
	got := command(`C:\Program Files\rig-exporter\rig-exporter.exe`)

	if !strings.HasSuffix(got, " "+BackgroundFlag) {
		t.Errorf("command = %q, want it to end in %s", got, BackgroundFlag)
	}
	// Quoted, or Windows splits the path at the space and runs "C:\Program".
	if !strings.HasPrefix(got, `"C:\Program Files\`) {
		t.Errorf("command = %q, want the path quoted", got)
	}
	if strings.Count(got, `"`) != 2 {
		t.Errorf("command = %q, want exactly one quoted path", got)
	}
}

// A path without spaces is quoted too. One shape is easier to compare against
// than two, and Enabled has to recognise what Set wrote.
func TestTheCommandHasOneShape(t *testing.T) {
	got := command(`C:\tools\rig-exporter.exe`)
	if want := `"C:\tools\rig-exporter.exe" ` + BackgroundFlag; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}
