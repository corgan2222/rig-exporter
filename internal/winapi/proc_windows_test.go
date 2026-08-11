//go:build windows

package winapi

import (
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The whole point of the guard: a symbol that is not there has to come back as
// an error. LazyProc.Call panics instead, and a panic in a -H windowsgui binary
// is a process that disappears without a window and without a log line.
func TestCallReturnsAnErrorForAMissingSymbol(t *testing.T) {
	p := windows.NewLazySystemDLL("kernel32.dll").NewProc("ThisSymbolDoesNotExistW")

	ret, err := Call(p)
	if err == nil {
		t.Fatal("a missing symbol must be an error, not a panic")
	}
	if ret != 0 {
		t.Errorf("ret = %d, want 0 — a call that never happened has no result", ret)
	}
	// The name has to be in the message, or the log says a call failed without
	// saying which one.
	if !strings.Contains(err.Error(), "ThisSymbolDoesNotExistW") {
		t.Errorf("error = %q, want the symbol name in it", err)
	}
}

// A DLL that does not exist at all takes the same path. Server Core and Windows
// PE ship user32 and shell32 reduced or not at all, and broad compatibility is
// a stated goal of this program.
func TestCallReturnsAnErrorForAMissingLibrary(t *testing.T) {
	p := windows.NewLazySystemDLL("rig-exporter-no-such.dll").NewProc("Anything")

	if _, err := Call(p); err == nil {
		t.Fatal("a missing library must be an error, not a panic")
	}
}

// A symbol that is there behaves exactly as before the guard: the return value
// of the call, and the errno the call set.
func TestCallPassesThroughToAPresentSymbol(t *testing.T) {
	p := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

	ret, err := Call(p)
	if err != nil && err != windows.Errno(0) {
		t.Fatalf("Call: %v", err)
	}
	if ret == 0 {
		t.Error("GetTickCount64 returned 0 — the call did not reach the API")
	}
}

// The trap this contract carries, nailed down rather than left to be discovered:
// a call that succeeded still comes back with Errno(0), and that is not nil in
// an error interface. Anyone writing `if err != nil` around Call gets a failure
// on every successful call — the return value is what decides, the error only
// carries the message.
//
// Written after the fact, and it passes straight away. It is not proof that the
// guard works, it is a fence around the one way to misread it.
func TestASuccessfulCallStillReturnsANonNilError(t *testing.T) {
	p := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

	ret, err := Call(p)
	if ret == 0 {
		t.Fatal("GetTickCount64 returned 0 — the call did not reach the API")
	}
	if err == nil {
		t.Skip("Call now normalises Errno(0) to nil — update the doc comment too")
	}
	if errno, ok := err.(windows.Errno); !ok || errno != 0 {
		t.Errorf("err = %v (%T), want windows.Errno(0)", err, err)
	}
}

// A proc whose return value IS the error code cannot be judged by that value:
// zero means success there, so a missing symbol — which also yields zero —
// would read as a good call. This is the iphlpapi family, and it is why Call
// alone is not enough.
func TestCallStatusReportsAMissingSymbolRatherThanSuccess(t *testing.T) {
	p := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("ThisSymbolDoesNotExistW")

	if err := CallStatus(p); err == nil {
		t.Fatal("a missing symbol read as success — zero is exactly what it returns")
	}
}

// A non-zero status comes back as the Win32 error it is, so the message says
// something a human can look up instead of a bare number.
func TestCallStatusTurnsAStatusIntoAnErrno(t *testing.T) {
	// GetIfEntry2 with a null row returns ERROR_INVALID_PARAMETER (87).
	p := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIfEntry2")

	err := CallStatus(p, 0)
	if err == nil {
		t.Fatal("a null row was accepted")
	}
	if errno, ok := err.(windows.Errno); !ok || errno != windows.ERROR_INVALID_PARAMETER {
		t.Errorf("err = %v (%T), want ERROR_INVALID_PARAMETER", err, err)
	}
}

// Success is a nil error, unlike Call — here the status carries the whole
// answer, so there is nothing left to hand back.
func TestCallStatusReturnsNilOnSuccess(t *testing.T) {
	var table uintptr
	p := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIfTable2")

	if err := CallStatus(p, uintptr(unsafe.Pointer(&table))); err != nil {
		t.Fatalf("GetIfTable2: %v", err)
	}
	if table == 0 {
		t.Error("GetIfTable2 reported success without writing a table")
	}
	free := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("FreeMibTable")
	_, _ = Call(free, table)
}

// NtQuerySystemInformation and CallNtPowerInformation answer with an NTSTATUS,
// not a Win32 code. Same shape as CallStatus, different numbering — 0xC0000004
// is STATUS_INFO_LENGTH_MISMATCH, while Errno 0xC0000004 is nothing at all.
func TestCallNTStatusReportsAMissingSymbol(t *testing.T) {
	p := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtThisDoesNotExist")

	if err := CallNTStatus(p); err == nil {
		t.Fatal("a missing symbol read as success")
	}
}

// A failed call names its status rather than printing a bare hex number.
func TestCallNTStatusTurnsAStatusIntoAnNTStatus(t *testing.T) {
	p := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQuerySystemInformation")

	// SystemProcessorPerformanceInformation with a zero-length buffer.
	var returned uint32
	err := CallNTStatus(p, 8, 0, 0, uintptr(unsafe.Pointer(&returned)))
	if err == nil {
		t.Fatal("a zero-length buffer was accepted")
	}
	if _, ok := err.(windows.NTStatus); !ok {
		t.Errorf("err = %v (%T), want windows.NTStatus", err, err)
	}
}

// The kernel32, user32 and shell32 symbols this package reaches for have to
// resolve on an ordinary desktop Windows. This catches no missing guard — it
// catches a typo in a symbol name, and that is the likelier cause of a panic
// than a Windows API that is genuinely absent.
//
// Listed by hand rather than discovered: there is no way to enumerate a
// package's variables at runtime. A proc added without a line here is simply
// not covered, which is why the guard matters more than this test does.
//
// The optional ones are deliberately left out — battery, HID, locale, ntdll.
// Those may be missing on a given machine, their callers already check with
// Find, and failing here would be a false alarm.
func TestTheSystemProcsResolveOnThisMachine(t *testing.T) {
	procs := []*windows.LazyProc{
		procOpenFileMappingW, procGetSystemFirmwareTable, procGetTickCount64,
		procGetSystemTimes, procGlobalMemoryStatusEx, procGetForegroundWindow,
		procGetLastInputInfo, procGetWindowThreadProcess, procEnumDisplaySettingsW,
		procMessageBoxW, procShellExecuteW,
		procAttachConsole, procAllocConsole, procFreeConsole,
	}

	for _, p := range procs {
		if err := p.Find(); err != nil {
			t.Errorf("%s does not resolve: %v", p.Name, err)
		}
	}
}
