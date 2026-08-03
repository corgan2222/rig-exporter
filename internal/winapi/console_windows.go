//go:build windows

package winapi

import (
	"os"

	"golang.org/x/sys/windows"
)

// This program is linked with -H windowsgui so that starting it does not flash
// a console window at the user. The cost is that it has no console at all: from
// a terminal, anything it prints goes nowhere, and -probe produced a completely
// empty file. The advice to redirect it does not help either, because there was
// never a stream to redirect.
//
// The fix is the standard one for a GUI binary that also has a command line:
// borrow the console of whatever started it.

var (
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
	procFreeConsole   = kernel32.NewProc("FreeConsole")
)

// attachParentProcess asks for the console of the process that started us.
const attachParentProcess = ^uintptr(0) // (DWORD)-1

// AttachConsole gives this process somewhere to print, and reports whether it
// found one.
//
// Three cases, in the order they are checked:
//
// Output is already going somewhere — redirected to a file, or into a pipe.
// That handle is left strictly alone: replacing it would send the user's
// redirect to a console instead of the file they asked for.
//
// Started from a terminal. Its console is borrowed, which is what somebody
// typing the command expects to see.
//
// Started from Explorer or the tray, with no console anywhere. Nothing is
// allocated: a window that appears and vanishes when the process ends is worse
// than silence. The caller writes to a file instead and says where.
func AttachConsole() bool {
	if stdoutGoesSomewhere() {
		return true
	}
	if ret, _, _ := procAttachConsole.Call(attachParentProcess); ret == 0 {
		return false
	}
	if !reopenStandardStreams() {
		procFreeConsole.Call()
		return false
	}
	return true
}

// AllocConsole makes a console of this process's own, for when there is no
// parent to borrow one from. It is separate from AttachConsole because it is a
// deliberate act with a visible window, not a fallback to slip into silently.
func AllocConsole() bool {
	if stdoutGoesSomewhere() {
		return true
	}
	if ret, _, _ := procAllocConsole.Call(); ret == 0 {
		return false
	}
	return reopenStandardStreams()
}

// stdoutGoesSomewhere reports whether writes already have a destination.
//
// An invalid or null handle is what a GUI binary starts with; anything else —
// a file, a pipe, a console — is a destination somebody chose.
func stdoutGoesSomewhere() bool {
	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	return err == nil && handle != 0 && handle != windows.InvalidHandle
}

// reopenStandardStreams points os.Stdout and os.Stderr at the console.
//
// Both the process handles and the Go values have to be replaced. Go captured
// os.Stdout at startup, when there was no console and the handle was invalid,
// so setting the process handle alone would fix nothing that this program
// actually writes through.
func reopenStandardStreams() bool {
	name, err := windows.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return false
	}

	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return false
	}

	windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, handle)
	windows.SetStdHandle(windows.STD_ERROR_HANDLE, handle)

	os.Stdout = os.NewFile(uintptr(handle), "CONOUT$")
	os.Stderr = os.Stdout
	return true
}
