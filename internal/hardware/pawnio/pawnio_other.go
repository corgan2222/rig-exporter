//go:build !windows

package pawnio

// Detect has nothing to look for off Windows, where this package exists only so
// the surrounding code stays buildable.
func Detect() State {
	return State{Availability: NotInstalled, Detail: "PawnIO is Windows-only"}
}
