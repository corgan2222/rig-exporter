//go:build windows

package pawnio

import "testing"

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

// The library is looked for by bare name first, then where an installer would
// put it, derived from the environment rather than a hardcoded path.
func TestLibraryIsLookedForBeyondTheSearchPath(t *testing.T) {
	paths := libraryPaths()

	if len(paths) == 0 || paths[0] != libraryFileName {
		t.Fatalf("paths = %v, want the bare name first", paths)
	}
	if len(paths) < 2 {
		t.Error("only the search path is consulted; an installed copy would be missed")
	}
	for _, p := range paths[1:] {
		if p == libraryFileName {
			t.Error("the bare name is repeated")
		}
	}
}
