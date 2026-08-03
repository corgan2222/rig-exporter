// Package pawnio talks to PawnIO, a signed kernel driver that runs sandboxed
// bytecode modules on behalf of user programs.
//
// It exists here for one reason: processor temperature. AMD reports Tctl
// through the SMU and Intel through a model-specific register, and both live in
// ring 0, so no ordinary program can read them. Until now the only source was
// MSI Afterburner's shared memory, which means a machine without Afterburner
// reports no processor temperature at all.
//
// PawnIO is the safe form of that access. The alternative that tools have used
// for years, WinRing0, hands out unrestricted register and port access to
// whoever asks, which is why Microsoft put it on the driver blocklist. PawnIO
// instead loads signed bytecode into a verifying interpreter, so a module can
// only do what its module was signed to do.
//
// Two things it is NOT:
//
// PawnIO is not shipped with this program and never will be. It is GPL-2.0, the
// user installs it themselves, and this package only looks for it. Nothing here
// depends on it being present.
//
// PawnIO is not usable without elevation. Its device carries a protected ACL
// granting access to LocalSystem and administrators only, so an ordinary
// unelevated process is refused — measured, not assumed: pawnio_open returns
// E_ACCESSDENIED. The library itself loads fine and reports its version, which
// is what makes an honest "installed, but not reachable from here" possible.
package pawnio

// Availability says what can be done with PawnIO on this machine, and when the
// answer is "nothing", why.
//
// The distinction matters to the person reading it. "Not installed" is an
// invitation; "installed but this program is not elevated" is a different
// problem with a different fix, and telling someone to install software they
// already have is the kind of advice that destroys trust in the rest.
type Availability int

const (
	// NotInstalled means PawnIOLib could not be loaded.
	NotInstalled Availability = iota
	// NeedsElevation means PawnIO is there and the driver answers, but this
	// process is not allowed to open the device.
	NeedsElevation
	// DriverUnavailable means the library is installed but the driver did not
	// start: blocked, stopped, or a version that does not match.
	DriverUnavailable
	// Ready means the device opened and modules can be run.
	Ready
)

// State is the result of looking for PawnIO.
type State struct {
	Availability Availability
	// Version is what the library reports, e.g. "2.0.0". Empty when the
	// library could not be loaded.
	Version string
	// Detail carries the underlying failure for the log. It is deliberately not
	// what the interface shows: an HRESULT helps whoever is debugging and tells
	// a user nothing.
	Detail string
}

// Installed reports whether PawnIO is on the machine at all, whatever this
// process is allowed to do with it.
func (s State) Installed() bool { return s.Availability != NotInstalled }

// Usable reports whether readings can actually be taken.
func (s State) Usable() bool { return s.Availability == Ready }
