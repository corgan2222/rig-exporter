//go:build windows

// Package winapi wraps the handful of Win32 calls rig-exporter needs that are not
// already covered by golang.org/x/sys/windows.
package winapi

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procOpenFileMappingW       = kernel32.NewProc("OpenFileMappingW")
	procGetSystemFirmwareTable = kernel32.NewProc("GetSystemFirmwareTable")
	procGetTickCount64         = kernel32.NewProc("GetTickCount64")
	procGetSystemTimes         = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx   = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetLastInputInfo       = user32.NewProc("GetLastInputInfo")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procEnumDisplaySettingsW   = user32.NewProc("EnumDisplaySettingsW")
	procMessageBoxW            = user32.NewProc("MessageBoxW")
	procShellExecuteW          = shell32.NewProc("ShellExecuteW")
)

// OpenFileMapping opens an existing named file mapping object.
// golang.org/x/sys/windows only wraps CreateFileMapping, but opening someone
// else's mapping read-only is exactly what reading RTSS requires.
func OpenFileMapping(access uint32, inheritHandle bool, name string) (windows.Handle, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var inherit uintptr
	if inheritHandle {
		inherit = 1
	}

	handle, callErr := Call(procOpenFileMappingW,
		uintptr(access),
		inherit,
		uintptr(unsafe.Pointer(namePtr)),
	)
	if handle == 0 {
		if errno, ok := callErr.(windows.Errno); ok && errno != 0 {
			return 0, errno
		}
		return 0, fmt.Errorf("OpenFileMapping(%s) failed", name)
	}
	return windows.Handle(handle), nil
}

// CommittedBytes reports how large a mapped region really is, or 0 when it
// cannot be determined.
//
// This belongs next to OpenFileMapping because every caller of that pair faces
// the same problem: the size to copy comes from a header inside the mapping,
// and the mapping is written by another process. A header that asks for more
// than was mapped is not a theory — the shared memory names are per-session and
// unqualified, so any process running as the same user can create one first and
// decide what the header says. Reading past the region is an access violation,
// which Go cannot recover from and which under -H windowsgui ends the program
// without a window or a log line.
//
// Both readers had a copy of this. One of them applied it and the other did
// not, which is the whole reason it now lives here.
func CommittedBytes(addr uintptr) int {
	var info windows.MemoryBasicInformation
	if err := windows.VirtualQuery(addr, &info, unsafe.Sizeof(info)); err != nil {
		return 0
	}

	// VirtualQuery succeeds for memory that was never mapped: it answers for
	// the free region the address falls in, and address 0 lands in a free
	// region of some two gigabytes. Returning that would hand a caller a clamp
	// that clamps nothing. Only committed pages describe bytes anybody may
	// read.
	if info.State != windows.MEM_COMMIT {
		return 0
	}

	// RegionSize counts from the base of the region, and the base can sit
	// below addr — VirtualQuery rounds down to the containing page. What the
	// caller may read is what is left from addr to the end of the region.
	end := info.BaseAddress + info.RegionSize
	if end <= addr {
		return 0
	}
	return int(end - addr)
}

// TickCount is milliseconds since boot, truncated to 32 bits so it can be
// compared directly against the GetTickCount timestamps RTSS stores.
func TickCount() uint32 {
	ret, _ := Call(procGetTickCount64)
	return uint32(ret)
}

// UptimeHours is how long the machine has been running, and whether it could be
// read. Unlike TickCount this keeps the full 64-bit value, so it stays correct
// past 49 days.
//
// The second result exists because zero is a real answer: a machine that just
// booted has been up for nearly zero hours. Without it a failed call published
// "just rebooted", which is a claim rather than a gap.
func UptimeHours() (float64, bool) {
	ret, _ := Call(procGetTickCount64)
	if ret == 0 {
		// GetTickCount64 cannot fail; it can only be missing, and Call answers
		// zero for that. A genuine zero would mean the machine booted during
		// this instruction.
		return 0, false
	}
	return float64(uint64(ret)) / 1000 / 3600, true
}

// lastInputInfo mirrors LASTINPUTINFO.
type lastInputInfo struct {
	Size uint32
	Time uint32
}

// IdleSeconds is how long ago the user last touched keyboard or mouse, and
// whether it could be read.
//
// The counter is per-session and stops while the workstation is locked, which
// is exactly what makes it useful as a presence signal in Home Assistant — and
// exactly why the second result matters. Zero seconds idle means somebody is at
// the machine right now, so a failed read that answered zero told every presence
// automation the opposite of the truth.
func IdleSeconds() (float64, bool) {
	info := lastInputInfo{}
	info.Size = uint32(unsafe.Sizeof(info))

	ret, _ := Call(procGetLastInputInfo, uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0, false
	}
	// Both values wrap after ~49 days; unsigned subtraction survives that.
	return float64(TickCount()-info.Time) / 1000, true
}

// ForegroundPID is the process owning the focused window, or 0 if there is
// none (locked screen, focus in transition).
func ForegroundPID() uint32 {
	hwnd, _ := Call(procGetForegroundWindow)
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	_, _ = Call(procGetWindowThreadProcess, hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// SystemTimes returns the raw counters behind CPU utilisation. Kernel time
// includes idle time, which is why the caller subtracts it.
func SystemTimes() (idle, kernel, user uint64, err error) {
	var idleFT, kernelFT, userFT windows.Filetime
	ret, callErr := Call(procGetSystemTimes,
		uintptr(unsafe.Pointer(&idleFT)),
		uintptr(unsafe.Pointer(&kernelFT)),
		uintptr(unsafe.Pointer(&userFT)),
	)
	if ret == 0 {
		return 0, 0, 0, fmt.Errorf("GetSystemTimes: %w", callErr)
	}
	return filetimeTo64(idleFT), filetimeTo64(kernelFT), filetimeTo64(userFT), nil
}

func filetimeTo64(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// memoryStatusEx mirrors MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// smbiosProvider is 'RSMB', the firmware table provider that hands out the
// raw SMBIOS structures.
const smbiosProvider = 0x52534D42

// SMBIOS returns the raw SMBIOS structure table, with the four byte header
// Windows prepends already stripped.
//
// This is the only way to reach memory module facts — speed, type, how many
// slots are filled — without going through WMI, which would mean COM and a
// service that is not always healthy.
func SMBIOS() ([]byte, error) {
	size, callErr := Call(procGetSystemFirmwareTable, smbiosProvider, 0, 0, 0)
	if size == 0 {
		return nil, fmt.Errorf("GetSystemFirmwareTable(RSMB): %w", callErr)
	}

	buf := make([]byte, size)
	written, callErr := Call(procGetSystemFirmwareTable,
		smbiosProvider, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if written == 0 || written > uintptr(len(buf)) {
		return nil, fmt.Errorf("GetSystemFirmwareTable(RSMB) returned %d: %w", written, callErr)
	}

	// RawSMBIOSData: calling method, major, minor, revision, then a length and
	// the table itself. Only the table is of interest here.
	const rawHeaderSize = 8
	if int(written) <= rawHeaderSize {
		return nil, fmt.Errorf("SMBIOS table is empty")
	}
	return buf[rawHeaderSize:written], nil
}

// MemoryStatus returns physical memory load in percent plus total and
// available bytes.
func MemoryStatus() (loadPercent uint32, totalBytes, availBytes uint64, err error) {
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))

	ret, callErr := Call(procGlobalMemoryStatusEx, uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return 0, 0, 0, fmt.Errorf("GlobalMemoryStatusEx: %w", callErr)
	}
	return status.MemoryLoad, status.TotalPhys, status.AvailPhys, nil
}

// devModeW mirrors DEVMODEW. Only the display half of the unions is declared,
// which is what EnumDisplaySettingsW fills in; the field order and the 220
// byte total still match the Win32 layout exactly.
type devModeW struct {
	DeviceName         [32]uint16
	SpecVersion        uint16
	DriverVersion      uint16
	Size               uint16
	DriverExtra        uint16
	Fields             uint32
	PositionX          int32
	PositionY          int32
	DisplayOrientation uint32
	DisplayFixedOutput uint32
	Color              int16
	Duplex             int16
	YResolution        int16
	TTOption           int16
	Collate            int16
	FormName           [32]uint16
	LogPixels          uint16
	BitsPerPel         uint32
	PelsWidth          uint32
	PelsHeight         uint32
	DisplayFlags       uint32
	DisplayFrequency   uint32
	ICMMethod          uint32
	ICMIntent          uint32
	MediaType          uint32
	DitherType         uint32
	Reserved1          uint32
	Reserved2          uint32
	PanningWidth       uint32
	PanningHeight      uint32
}

const enumCurrentSettings = 0xFFFFFFFF

// DisplayMode returns the current mode of the primary display. Reading it from
// the display driver rather than GetSystemMetrics means the numbers are the
// real pixel dimensions regardless of the process DPI awareness.
func DisplayMode() (width, height, refreshHz int, err error) {
	dm := devModeW{}
	dm.Size = uint16(unsafe.Sizeof(dm))

	ret, callErr := Call(procEnumDisplaySettingsW,
		0, // primary display of the calling thread
		uintptr(enumCurrentSettings),
		uintptr(unsafe.Pointer(&dm)),
	)
	if ret == 0 {
		return 0, 0, 0, fmt.Errorf("EnumDisplaySettingsW: %w", callErr)
	}
	return int(dm.PelsWidth), int(dm.PelsHeight), int(dm.DisplayFrequency), nil
}

// MessageBox button and icon flags used by this app.
const (
	MBOK              = 0x00000000
	MBYesNo           = 0x00000004
	MBIconWarning     = 0x00000030
	MBIconInformation = 0x00000040
	MBSetForeground   = 0x00010000
	MBTopMost         = 0x00040000

	// IDYes is the MessageBox return value for the Yes button.
	IDYes = 6
)

// MessageBox shows a modal dialog and returns the ID of the button pressed.
func MessageBox(title, text string, flags uint32) int {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	textPtr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return 0
	}
	ret, _ := Call(procMessageBoxW,
		0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(flags),
	)
	return int(ret)
}

// OpenURL hands a URL or file path to the shell, which opens it in whatever
// the user has registered for it.
func OpenURL(target string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}

	const swShowNormal = 1

	// ShellExecuteW returns a value greater than 32 on success.
	ret, callErr := Call(procShellExecuteW,
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0,
		swShowNormal,
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW(%s): %w", target, callErr)
	}
	return nil
}

// AcquireSingleInstance takes a named mutex so a second launch can bow out
// instead of putting a duplicate icon in the tray. The returned handle must
// stay alive for the lifetime of the process.
func AcquireSingleInstance(name string) (windows.Handle, bool, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, false, err
	}
	// CreateMutex hands back a valid handle even when the mutex already
	// exists; the "error" is how it reports that we are not the first.
	handle, err := windows.CreateMutex(nil, false, namePtr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("create mutex %s: %w", name, err)
	}
	return handle, true, nil
}
