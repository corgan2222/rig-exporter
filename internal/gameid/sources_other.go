//go:build !windows

package gameid

// Off Windows this package exists only so its matching and caching stay
// testable. There is no registry to read and no launcher to ask, so both
// sources answer the way an absent one does.

// CurrentUser has no hive to open, and reports nothing.
type CurrentUser struct{}

// String never finds a value.
func (CurrentUser) String(_, _ string) (string, bool) { return "", false }

// Uint never finds a value.
func (CurrentUser) Uint(_, _ string) (uint64, bool) { return 0, false }

// Installs finds no launcher catalogue.
func Installs() []Install { return nil }
