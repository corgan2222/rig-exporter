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
// The second return value is what Call itself reports: a windows.Errno that is
// zero when the call set no error. Callers keep reading it exactly as they did
// before — the guard adds a case, it does not reshape the ones that existed.
func Call(p *windows.LazyProc, args ...uintptr) (uintptr, error) {
	if err := p.Find(); err != nil {
		return 0, fmt.Errorf("%s unavailable: %w", p.Name, err)
	}
	ret, _, callErr := p.Call(args...)
	return ret, callErr
}
