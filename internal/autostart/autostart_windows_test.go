//go:build windows

package autostart

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// testValueName is this test's own entry under Run. Deliberately not
// config.AppName: that one belongs to whoever is sitting at this machine, and a
// test has no business switching their autostart on and off.
const testValueName = "rig-exporter-autostart-test"

// Enabled reads the registry on every call on purpose — an entry removed
// outside rig-exporter has to become visible rather than be believed from the
// configuration file. Caching the executable path must not turn that into a
// cached answer.
func TestEnabledSeesARegistryChangeBetweenCalls(t *testing.T) {
	t.Cleanup(func() { _ = Set(testValueName, false) })

	if err := Set(testValueName, true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	on, err := Enabled(testValueName)
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if !on {
		t.Fatal("Enabled reported off right after the entry was written")
	}

	// Removed the way something outside this program would: straight on the key.
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open Run key: %v", err)
	}
	if err := key.DeleteValue(testValueName); err != nil {
		key.Close()
		t.Fatalf("delete %s: %v", testValueName, err)
	}
	key.Close()

	if on, err = Enabled(testValueName); err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if on {
		t.Error("Enabled still reported on after the entry was removed outside")
	}
}

// Status() calls this on every measurement tick, so what it costs is not an
// academic question. os.Executable plus EvalSymlinks opens the running exe,
// which is the expensive half — the registry is not.
func BenchmarkEnabled(b *testing.B) {
	for b.Loop() {
		_, _ = Enabled(testValueName)
	}
}

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
