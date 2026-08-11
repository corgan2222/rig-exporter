//go:build windows

package winapi

import (
	"strings"
	"testing"

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
