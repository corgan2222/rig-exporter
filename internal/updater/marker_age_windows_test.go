//go:build windows

package updater

import (
	"encoding/json"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// markerPlan is the plan every test here inspects.
func markerPlan() applyPlan {
	return applyPlan{
		ExpectedVersion: "1.9.4",
		HelperReadyPath: `C:\rig\helper-ready`,
		HelperPath:      `C:\rig\rig-exporter-update.exe`,
	}
}

// marker writes one, as writeProcessMarker would.
func marker(t *testing.T, pid int, readyAt time.Time) []byte {
	t.Helper()

	data, err := json.Marshal(readyMarker{Version: "1.9.4", PID: pid, ReadyAt: readyAt})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A marker left behind days ago does not describe a running helper.
//
// An update that breaks off while the helper runs — a power cut, a hard
// restart, a virus scanner taking the executable mid-flight — leaves the file
// with its pid in it. Days later Windows hands that pid to a service running as
// SYSTEM, OpenProcess answers ERROR_ACCESS_DENIED, and the error travelled all
// the way up: RecoverInterruptedApply passed it on, main showed the start-failed
// dialog and exited. Not once — the file stays, the pid stays taken, and every
// further start fails the same way, with nothing on screen to say which folder
// to look in.
//
// The one fact that settles it was already on disk: the marker carries the time
// it was written, and nothing ever read it.
func TestAnAgedHelperMarkerIsNotAnActiveHelper(t *testing.T) {
	ops := &fakeApplyOps{
		files: map[string][]byte{
			`C:\rig\helper-ready`: marker(t, 4712, time.Now().UTC().Add(-72*time.Hour)),
		},
		processMatchesErr: windows.ERROR_ACCESS_DENIED,
	}

	active, err := activeApplyHelper(ops, markerPlan())

	if err != nil {
		t.Fatalf("a three-day-old marker made the start fail: %v", err)
	}
	if active {
		t.Error("a three-day-old marker was reported as a running helper")
	}
}

// And a helper that really is running still stops the start, or an update in
// progress would be trampled by the process it just handed over to.
func TestAFreshHelperMarkerStillBlocksTheStart(t *testing.T) {
	ops := &fakeApplyOps{
		files: map[string][]byte{
			`C:\rig\helper-ready`: marker(t, 4712, time.Now().UTC()),
		},
		processPaths: map[int]string{4712: `C:\rig\rig-exporter-update.exe`},
	}

	active, err := activeApplyHelper(ops, markerPlan())

	if err != nil {
		t.Fatalf("activeApplyHelper: %v", err)
	}
	if !active {
		t.Error("a running helper was not recognised, so the update would be trampled")
	}
}

// A marker with no time in it is decided the way it always was.
//
// The field has been written since the beginning, so this is not an old
// version's file — it is a truncated or hand-made one. A missing time must not
// read as "infinitely old", or a marker somebody damaged would silently permit
// a second process to start over a live update.
func TestAMarkerWithoutATimeIsDecidedByItsPID(t *testing.T) {
	ops := &fakeApplyOps{
		files: map[string][]byte{
			`C:\rig\helper-ready`: []byte(`{"version":"1.9.4","pid":4712}`),
		},
		processPaths: map[int]string{4712: `C:\rig\rig-exporter-update.exe`},
	}

	active, err := activeApplyHelper(ops, markerPlan())

	if err != nil {
		t.Fatalf("activeApplyHelper: %v", err)
	}
	if !active {
		t.Error("a marker without a time was discarded instead of being checked by pid")
	}
}

// A pid we are not allowed to open is not our helper.
//
// Access denied and invalid parameter answer this one question the same way:
// the process cannot be confirmed as ours, so it is not ours. Only invalid
// parameter counted, and everything else became a hard error that reached the
// start-failed dialog.
//
// Checked on the decision rather than on a real process, because opening a
// process we are forbidden to open cannot be arranged reliably: on an elevated
// machine the open succeeds and the failure moves one line further down, so a
// test built on a SYSTEM pid would pass or fail depending on who ran it.
func TestAPidWeCannotOpenIsNotOurHelper(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"no such process", windows.ERROR_INVALID_PARAMETER, true},
		{"a pid that now belongs to SYSTEM", windows.ERROR_ACCESS_DENIED, true},
		{"something that is actually wrong", windows.ERROR_NOT_ENOUGH_MEMORY, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := notOurProcess(tc.err); got != tc.want {
				t.Errorf("notOurProcess(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
