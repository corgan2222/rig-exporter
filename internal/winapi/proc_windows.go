//go:build windows

package winapi

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// Call invokes a lazily bound proc, but only once it is known to be there.
//
// windows.LazyProc.Call **panics** on a missing symbol — there is no error
// return to check. Linked with -H windowsgui that panic is a process that
// vanishes without a window, without a dialog and without a line in the log,
// which is the worst shape a failure can take in this program.
//
// On desktop Windows the symbols this program reaches for have all been there
// since XP, so the guard is not expected to fire. It exists because broad
// compatibility is a stated goal and because Server Core, Nano Server and
// Windows PE ship user32 and shell32 reduced or not at all — and because a rule
// that holds in eleven files and not in four is not a rule.
//
// The result of Find is memoised behind a sync.Once inside LazyProc, so from
// the second call on the guard costs a load and a branch.
//
// **Do not test the error with err != nil.** A call that succeeded comes back
// with windows.Errno(0), and a zero Errno held in an error interface is not
// nil. That is deliberately left as it is: judge the call by its return value,
// the way each function's Win32 documentation says to, and use the error only
// for the message. The existing shape
//
//	if ret == 0 { return fmt.Errorf("Something: %w", err) }
//
// then covers the missing symbol too, without a single caller changing how it
// decides. That is the whole reason the guard fits under nineteen call sites
// without reshaping any of them.
func Call(p *windows.LazyProc, args ...uintptr) (uintptr, error) {
	if err := p.Find(); err != nil {
		return 0, fmt.Errorf("%s unavailable: %w", p.Name, err)
	}
	ret, _, callErr := p.Call(args...)
	return ret, callErr
}

// CallStatus is Call for a proc whose return value *is* a Win32 error code —
// the iphlpapi family, GetIfTable2 and GetBestRoute2 among them.
//
// Those cannot use Call: zero means success there, and a missing symbol yields
// zero as well, so judging by the return value would read an absent API as a
// good call. GetBestRoute2 did exactly that — it handed back the untouched,
// all-zero route and the caller reported "no default gateway".
//
// Success is a nil error here, unlike Call. There is nothing left over to hand
// back once the status has been read, and a non-zero status is returned as the
// windows.Errno it is, so the message names the failure instead of numbering it.
func CallStatus(p *windows.LazyProc, args ...uintptr) error {
	if err := p.Find(); err != nil {
		return fmt.Errorf("%s unavailable: %w", p.Name, err)
	}
	if ret, _, _ := p.Call(args...); ret != 0 {
		return windows.Errno(ret)
	}
	return nil
}

// CallNTStatus is CallStatus for the native API, which numbers its failures
// differently: NtQuerySystemInformation and CallNtPowerInformation answer with
// an NTSTATUS, where 0xC0000004 is STATUS_INFO_LENGTH_MISMATCH and the Win32
// error of the same number is nothing at all.
//
// Kept apart from CallStatus for that reason alone. Folding the two would put
// the wrong name on every failure the native API reports, and a diagnostic that
// names the wrong thing is worse than a bare number.
func CallNTStatus(p *windows.LazyProc, args ...uintptr) error {
	if err := p.Find(); err != nil {
		return fmt.Errorf("%s unavailable: %w", p.Name, err)
	}
	if ret, _, _ := p.Call(args...); ret != 0 {
		return windows.NTStatus(ret)
	}
	return nil
}
