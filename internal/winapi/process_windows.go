//go:build windows

package winapi

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// K32GetProcessMemoryInfo rather than the psapi.dll export of the same name:
// kernel32 has carried it since Windows 7, and one fewer DLL to load is one
// fewer thing that can be missing.
var procGetProcessMemoryInfo = kernel32.NewProc("K32GetProcessMemoryInfo")

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS. Every field after the
// first two is a SIZE_T, which is why they are uintptr and not uint64: the
// struct is eight bytes shorter on a 32-bit build, and cb has to match.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// ProcessWorkingSetBytes is how much physical memory this process is holding
// right now — the number Task Manager shows in its memory column.
//
// The working set rather than the commit size, because the question this
// answers is "what is rig-exporter costing this machine", and pages that have
// been swapped out are not costing it anything.
func ProcessWorkingSetBytes() (uint64, error) {
	// LazyProc.Call panics on a symbol it cannot resolve — there is no error
	// value — and with -H windowsgui that kills the tray without a word.
	if err := procGetProcessMemoryInfo.Find(); err != nil {
		return 0, fmt.Errorf("K32GetProcessMemoryInfo unavailable: %w", err)
	}

	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))

	ret, _, callErr := procGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.CB),
	)
	if ret == 0 {
		return 0, fmt.Errorf("K32GetProcessMemoryInfo: %w", callErr)
	}
	return uint64(counters.WorkingSetSize), nil
}

// ProcessCPUTime is the CPU time this process has consumed since it started,
// in the same 100-nanosecond units as SystemTimes.
//
// Kernel and user are added because the caller only ever wants the total: time
// spent inside a system call is time this process asked for.
func ProcessCPUTime() (uint64, error) {
	var creation, exit, kernel, user windows.Filetime
	err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user)
	if err != nil {
		return 0, fmt.Errorf("GetProcessTimes: %w", err)
	}
	return filetimeTo64(kernel) + filetimeTo64(user), nil
}
