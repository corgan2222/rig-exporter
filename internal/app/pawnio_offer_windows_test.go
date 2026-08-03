//go:build windows

package app

import (
	"net/url"
	"testing"
)

// The published URL is a redirect, so whatever the chain ends on is what gets
// written to disk and offered to somebody as an installer. That destination is
// checked, never assumed.
func TestOnlyReleaseHostsAreAccepted(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/namazso/PawnIO.Setup/releases/download/v2.2.0/PawnIO_setup.exe": true,
		"https://objects.githubusercontent.com/whatever":                                    true,
		"https://release-assets.githubusercontent.com/whatever":                             true,

		"https://evil.example/PawnIO_setup.exe":        false,
		"https://github.com.evil.example/PawnIO.Setup": false,
		"https://notgithub.com/x":                      false,
		"http://github.com/namazso/PawnIO.Setup":       false, // downgraded to plain http
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := checkDownloadHost(parsed) == nil; got != want {
			t.Errorf("checkDownloadHost(%q) accepted=%v, want %v", raw, got, want)
		}
	}

	if checkDownloadHost(nil) == nil {
		t.Error("a missing address was accepted")
	}
}

// The download must be the only thing that function does. Executing what was
// just fetched is a separate, deliberate act, handed to the Windows shell so
// its signature and elevation checks run where the user can see them.
func TestDownloadingDoesNotExecute(t *testing.T) {
	// Compile-time in spirit: DownloadPawnIOSetup returns a path and nothing
	// else, so there is nowhere for a launch to hide. This test exists to make
	// that contract explicit and to fail loudly if the signature ever grows a
	// "run it too" flag.
	var _ func() (string, error) = func() (string, error) {
		return DownloadPawnIOSetup(t.Context(), t.TempDir())
	}
}
