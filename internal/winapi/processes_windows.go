//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                        = windows.NewLazySystemDLL("ntdll.dll")
	procNtQuerySystemInformation = ntdll.NewProc("NtQuerySystemInformation")
)

const (
	systemProcessInformation      = 5
	statusInfoLengthMismatch      = 0xC0000004
	processInfoInitialBufferBytes = 1 << 21 // 2 MB; 665 processes measured at 1.58 MB
	processInfoMaxAttempts        = 8
)

// systemProcessInfo mirrors SYSTEM_PROCESS_INFORMATION.
//
// Undocumented but unchanged since Windows NT, and the only way to get every
// process in one call. The alternative — EnumProcesses plus OpenProcess plus
// GetProcessTimes per PID — means several hundred handle opens per sample, and
// the handle is refused for anything running as another user.
//
// Only the fields up to WorkingSetSize are read; the rest is here so the size
// is right and a future field can be picked up without recounting offsets.
type systemProcessInfo struct {
	NextEntryOffset              uint32
	NumberOfThreads              uint32
	WorkingSetPrivateSize        int64
	HardFaultCount               uint32
	NumberOfThreadsHighWatermark uint32
	CycleTime                    uint64
	CreateTime                   int64
	UserTime                     int64
	KernelTime                   int64
	ImageName                    unicodeString
	BasePriority                 int32
	UniqueProcessID              uintptr
	InheritedFromUniqueID        uintptr
	HandleCount                  uint32
	SessionID                    uint32
	UniqueProcessKey             uintptr
	PeakVirtualSize              uintptr
	VirtualSize                  uintptr
	PageFaultCount               uint32
	PeakWorkingSetSize           uintptr
	WorkingSetSize               uintptr
	QuotaPeakPagedPool           uintptr
	QuotaPagedPool               uintptr
	QuotaPeakNonPagedPool        uintptr
	QuotaNonPagedPool            uintptr
	PagefileUsage                uintptr
	PeakPagefileUsage            uintptr
	PrivatePageCount             uintptr
	ReadOperationCount           int64
	WriteOperationCount          int64
	OtherOperationCount          int64
	ReadTransferCount            int64
	WriteTransferCount           int64
	OtherTransferCount           int64
}

// unicodeString is UNICODE_STRING: a counted, not necessarily terminated string.
type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

// ProcessSample is one process at one instant.
type ProcessSample struct {
	PID  uint32
	Name string
	// SessionID is 0 for services and the session number for anything a person
	// started, which is how the two are told apart.
	SessionID uint32
	// CPUTime is kernel plus user time in 100-nanosecond units, cumulative
	// since the process started. Only the difference between two samples means
	// anything.
	CPUTime uint64
	// PrivateBytes is memory this process does not share with any other.
	//
	// The working set is the more familiar number but cannot be summed: the
	// twenty-eight processes of one browser each count the same shared libraries
	// again, and adding them up overstates the browser by gigabytes.
	PrivateBytes uint64
}

// Processes lists every process in one call.
//
// Measured on a machine with 665 processes: 1.58 MB and about 19 ms. That is
// cheap for what it returns and far too expensive to do on a one-second loop,
// which is why the caller runs it on a schedule of its own.
func Processes() ([]ProcessSample, error) {
	// LazyProc.Call panics on a symbol it cannot resolve, and with
	// -H windowsgui that kills the tray without a word.
	if err := procNtQuerySystemInformation.Find(); err != nil {
		return nil, fmt.Errorf("NtQuerySystemInformation unavailable: %w", err)
	}

	size := uint32(processInfoInitialBufferBytes)
	var buf []byte
	for attempt := 0; ; attempt++ {
		buf = make([]byte, size)

		var needed uint32
		status, _, _ := procNtQuerySystemInformation.Call(
			uintptr(systemProcessInformation),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(size),
			uintptr(unsafe.Pointer(&needed)),
		)
		if status == 0 {
			break
		}
		if status != statusInfoLengthMismatch || attempt+1 >= processInfoMaxAttempts {
			return nil, fmt.Errorf("NtQuerySystemInformation: status 0x%X", status)
		}
		// Processes start while this runs, so the answer to "how much room do
		// you need" is already stale. The margin is what stops the retry loop
		// from chasing a moving target.
		size = needed + needed/4
	}

	return parseProcessInfo(buf), nil
}

func parseProcessInfo(buf []byte) []ProcessSample {
	var out []ProcessSample

	for offset := uintptr(0); offset+unsafe.Sizeof(systemProcessInfo{}) <= uintptr(len(buf)); {
		entry := (*systemProcessInfo)(unsafe.Pointer(&buf[offset]))

		name := ""
		if entry.ImageName.Buffer != nil && entry.ImageName.Length > 0 {
			name = syscall.UTF16ToString(
				unsafe.Slice(entry.ImageName.Buffer, entry.ImageName.Length/2))
		}

		out = append(out, ProcessSample{
			PID:          uint32(entry.UniqueProcessID),
			Name:         name,
			SessionID:    entry.SessionID,
			CPUTime:      uint64(entry.KernelTime) + uint64(entry.UserTime),
			PrivateBytes: uint64(entry.PrivatePageCount),
		})

		if entry.NextEntryOffset == 0 {
			break
		}
		offset += uintptr(entry.NextEntryOffset)
	}
	return out
}

// LogicalProcessors is how many hardware threads the machine has, which is the
// denominator that turns process CPU time into the percentage Task Manager
// shows.
func LogicalProcessors() int {
	if n := windows.GetActiveProcessorCount(windows.ALL_PROCESSOR_GROUPS); n > 0 {
		return int(n)
	}
	return 1
}
