//go:build windows

package pawnio

import (
	"path/filepath"
	"testing"
)

// Detect has to work on every machine, whether or not PawnIO is there, and must
// never take the program down: it runs at startup, before anything else.
func TestDetectAlwaysAnswers(t *testing.T) {
	state := Detect()

	switch state.Availability {
	case NotInstalled, NeedsElevation, DriverUnavailable, Ready:
	default:
		t.Fatalf("Detect returned an availability of %d", state.Availability)
	}

	// The version can only be read once the library is loaded, so the two must
	// agree — a version without an installation is a contradiction.
	if !state.Installed() && state.Version != "" {
		t.Errorf("not installed, yet a version of %q was reported", state.Version)
	}
	if state.Availability != NotInstalled && state.Version == "" {
		t.Error("the library loaded but reported no version")
	}
	if !state.Usable() && state.Detail == "" {
		t.Error("unusable without saying why")
	}

	t.Logf("availability=%d version=%q detail=%q", state.Availability, state.Version, state.Detail)
}

// Repeating the lookup must not change the answer or reopen anything: the
// interface polls this on every status render.
func TestDetectIsStable(t *testing.T) {
	first := Detect()
	for i := 0; i < 3; i++ {
		if got := Detect(); got != first {
			t.Fatalf("Detect changed from %+v to %+v", first, got)
		}
	}
}

// The library is looked for by absolute path only.
//
// A bare name would be resolved by the Windows search order, which starts at
// the directory of the running executable and takes in the working directory
// and %PATH% on the way. Detect runs on every start regardless of the setting,
// and PawnIO is used elevated, so a planted DLL would be loaded by the
// administrative process — this test is the guard against the bare name coming
// back for convenience.
func TestTheLibraryIsOnlyLoadedFromAnAbsolutePath(t *testing.T) {
	paths := libraryPaths()

	if len(paths) == 0 {
		t.Fatal("no candidate at all; an installed copy would be missed")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			t.Errorf("candidate %q is not absolute, so Windows would search for it", path)
		}
		if seen[path] {
			t.Errorf("candidate %q appears twice", path)
		}
		seen[path] = true
	}
}
