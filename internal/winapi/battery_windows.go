//go:build windows

package winapi

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	powrprof = windows.NewLazySystemDLL("powrprof.dll")
	setupapi = windows.NewLazySystemDLL("setupapi.dll")

	procGetSystemPowerStatus        = kernel32.NewProc("GetSystemPowerStatus")
	procCallNtPowerInformation      = powrprof.NewProc("CallNtPowerInformation")
	procSetupDiEnumDeviceInterfaces = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetInterfaceDetail   = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
)

// systemPowerStatus mirrors SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// systemBatteryState mirrors SYSTEM_BATTERY_STATE, the payload of
// CallNtPowerInformation at level five. Rate is documented as an unsigned
// value and is signed in practice: negative means the pack is draining.
type systemBatteryState struct {
	AcOnLine          byte
	BatteryPresent    byte
	Charging          byte
	Discharging       byte
	Spare1            [3]byte
	Tag               byte
	MaxCapacity       uint32
	RemainingCapacity uint32
	Rate              int32
	EstimatedTime     uint32
	DefaultAlert1     uint32
	DefaultAlert2     uint32
}

const (
	// systemBatteryStateLevel is POWER_INFORMATION_LEVEL SystemBatteryState.
	systemBatteryStateLevel = 5

	// batteryFlagNoBattery is the SYSTEM_POWER_STATUS bit that says this
	// machine has no battery at all, as opposed to one that is simply full.
	batteryFlagNoBattery = 128
	// batteryPercentUnknown is what BatteryLifePercent carries when Windows
	// cannot say.
	batteryPercentUnknown = 255

	batteryUnknown = 0xFFFFFFFF
	// batteryUnknownRate is BATTERY_UNKNOWN_RATE, as the signed value it
	// arrives as.
	batteryUnknownRate = -2147483648
)

// BatteryState is the live state of the machine's battery pack, aggregated by
// Windows over however many cells are installed.
//
// The "known" flags are there because Windows distinguishes "nought" from "the
// controller did not say", and so does everything downstream: a missing
// reading is left out rather than published as a zero.
type BatteryState struct {
	Present     bool
	OnAC        bool
	Charging    bool
	Discharging bool

	ChargePercent float64
	ChargeKnown   bool

	// RemainingMWh and FullMWh are milliwatt-hours — unless the controller
	// reports relative units, which only BatteryDevices can tell.
	RemainingMWh uint32
	FullMWh      uint32

	// RateMW is positive while charging and negative while draining.
	RateMW    int32
	RateKnown bool

	// RuntimeSeconds is what Windows expects to be left, and exists only
	// while the pack is actually draining.
	RuntimeSeconds uint32
	RuntimeKnown   bool
}

// Battery reads the live power state without opening a battery device.
//
// Two calls rather than one: the power information level carries the energy
// figures and the charge rate, while GetSystemPowerStatus carries the
// percentage Windows puts in its own taskbar and the flag that says there is
// no battery at all. A desktop answers Present false and nothing else.
//
// Neither call needs elevation, WMI, or anything installed.
func Battery() (BatteryState, error) {
	var state BatteryState

	// LazyProc.Call panics on a missing symbol rather than returning an
	// error, and under -H windowsgui that panic is silent. Every entry point
	// is resolved before it is used.
	if err := procCallNtPowerInformation.Find(); err != nil {
		return state, fmt.Errorf("CallNtPowerInformation unavailable: %w", err)
	}

	var raw systemBatteryState
	status, _, _ := procCallNtPowerInformation.Call(
		systemBatteryStateLevel,
		0, 0, // no input buffer
		uintptr(unsafe.Pointer(&raw)),
		unsafe.Sizeof(raw),
	)
	if status != 0 {
		return state, fmt.Errorf("CallNtPowerInformation(SystemBatteryState): NTSTATUS 0x%x", status)
	}

	state.Present = raw.BatteryPresent != 0
	state.OnAC = raw.AcOnLine != 0
	state.Charging = raw.Charging != 0
	state.Discharging = raw.Discharging != 0
	if raw.MaxCapacity != batteryUnknown {
		state.FullMWh = raw.MaxCapacity
	}
	if raw.RemainingCapacity != batteryUnknown {
		state.RemainingMWh = raw.RemainingCapacity
	}
	if raw.Rate != batteryUnknownRate {
		state.RateMW, state.RateKnown = raw.Rate, true
	}
	// An estimate while the pack is charging or idle says nothing, and
	// Windows fills it with the unknown marker often enough anyway.
	if state.Discharging && raw.EstimatedTime != batteryUnknown {
		state.RuntimeSeconds, state.RuntimeKnown = raw.EstimatedTime, true
	}

	// The second call only adds to what the first said, so its failure costs
	// the percentage and nothing else.
	if err := procGetSystemPowerStatus.Find(); err != nil {
		return state, nil
	}
	var power systemPowerStatus
	if ret, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&power))); ret == 0 {
		return state, nil
	}
	if power.BatteryFlag&batteryFlagNoBattery != 0 {
		state.Present = false
	}
	if power.BatteryLifePercent != batteryPercentUnknown && power.BatteryLifePercent <= 100 {
		state.ChargePercent, state.ChargeKnown = float64(power.BatteryLifePercent), true
	}
	return state, nil
}

// BatteryInfo is what one battery controller says about itself. All of it is
// static or nearly so — a cycle count moves in months — which is why the
// caller reads it on a schedule of its own rather than every collection.
type BatteryInfo struct {
	DesignedMWh uint32
	FullMWh     uint32
	CycleCount  uint32
	Chemistry   string
	VoltageMV   uint32
	// Relative marks a controller that reports capacities in units of its
	// own choosing. The two capacities still compare with each other, so
	// wear survives — but they are not milliwatt-hours and must not be
	// published as energy.
	Relative bool
}

// batteryDeviceGUID is GUID_DEVICE_BATTERY, the interface class every battery
// driver exposes. It holds the same value as GUID_DEVCLASS_BATTERY.
var batteryDeviceGUID = windows.GUID{
	Data1: 0x72631e54, Data2: 0x78a4, Data3: 0x11d0,
	Data4: [8]byte{0xbc, 0xf7, 0x00, 0xaa, 0x00, 0xb7, 0xb3, 0x2a},
}

// The battery control codes, written out rather than assembled, so they can be
// checked against batclass.h without running CTL_CODE in one's head:
// device type 0x29, read access, method buffered.
const (
	ioctlBatteryQueryTag         = 0x294040
	ioctlBatteryQueryInformation = 0x294044
	ioctlBatteryQueryStatus      = 0x29404C

	// batteryInformationLevel selects BATTERY_INFORMATION.
	batteryInformationLevel = 0
	// batteryCapacityRelative is BATTERY_CAPACITY_RELATIVE.
	batteryCapacityRelative = 0x40000000
)

// batteryQueryInformation mirrors BATTERY_QUERY_INFORMATION.
type batteryQueryInformation struct {
	Tag              uint32
	InformationLevel uint32
	AtRate           int32
}

// batteryInformation mirrors BATTERY_INFORMATION.
type batteryInformation struct {
	Capabilities        uint32
	Technology          byte
	Reserved            [3]byte
	Chemistry           [4]byte
	DesignedCapacity    uint32
	FullChargedCapacity uint32
	DefaultAlert1       uint32
	DefaultAlert2       uint32
	CriticalBias        uint32
	CycleCount          uint32
}

// batteryWaitStatus mirrors BATTERY_WAIT_STATUS. A timeout of nought means
// answer with the current state instead of waiting for it to change.
type batteryWaitStatus struct {
	Tag          uint32
	Timeout      uint32
	PowerState   uint32
	LowCapacity  uint32
	HighCapacity uint32
}

// batteryStatus mirrors BATTERY_STATUS.
type batteryStatus struct {
	PowerState uint32
	Capacity   uint32
	Voltage    uint32
	Rate       int32
}

// spDeviceInterfaceData mirrors SP_DEVICE_INTERFACE_DATA.
type spDeviceInterfaceData struct {
	Size               uint32
	InterfaceClassGUID windows.GUID
	Flags              uint32
	Reserved           uintptr
}

// detailHeaderSize is sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W) on 64 bit,
// which is all this program is built for. SetupAPI wants the size of that
// fixed header in cbSize, not the size of the buffer handed to it — passing
// the buffer size is the classic way to earn ERROR_INVALID_USER_BUFFER.
const detailHeaderSize = 8

// BatteryDevices reads every battery this machine has a driver for.
//
// This is the only route to designed capacity, cycle count and chemistry, and
// therefore the only way to say anything about wear: the live power calls know
// how full the pack is, not how large it was when new. It needs no elevation
// either. A machine without a battery has no such device and gets an empty
// slice rather than an error.
func BatteryDevices() ([]BatteryInfo, error) {
	if err := procSetupDiEnumDeviceInterfaces.Find(); err != nil {
		return nil, fmt.Errorf("SetupDiEnumDeviceInterfaces unavailable: %w", err)
	}
	if err := procSetupDiGetInterfaceDetail.Find(); err != nil {
		return nil, fmt.Errorf("SetupDiGetDeviceInterfaceDetailW unavailable: %w", err)
	}

	set, err := windows.SetupDiGetClassDevsEx(&batteryDeviceGUID, "", 0,
		windows.DIGCF_PRESENT|windows.DIGCF_DEVICEINTERFACE, 0, "")
	if err != nil {
		return nil, fmt.Errorf("SetupDiGetClassDevs(battery): %w", err)
	}
	defer set.Close()

	var out []BatteryInfo
	for index := 0; ; index++ {
		path, ok := batteryDevicePath(set, index)
		if !ok {
			break
		}
		info, err := readBatteryDevice(path)
		if err != nil {
			// One pack that will not answer is no reason to drop the others.
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// batteryDevicePath returns the device path of the index'th battery interface,
// and false once the enumeration has run out.
func batteryDevicePath(set windows.DevInfo, index int) (string, bool) {
	data := spDeviceInterfaceData{}
	data.Size = uint32(unsafe.Sizeof(data))

	ret, _, _ := procSetupDiEnumDeviceInterfaces.Call(
		uintptr(set), 0,
		uintptr(unsafe.Pointer(&batteryDeviceGUID)),
		uintptr(index),
		uintptr(unsafe.Pointer(&data)),
	)
	if ret == 0 {
		return "", false
	}

	// First call asks how much room the path needs; it fails by design.
	var needed uint32
	procSetupDiGetInterfaceDetail.Call(
		uintptr(set),
		uintptr(unsafe.Pointer(&data)),
		0, 0,
		uintptr(unsafe.Pointer(&needed)),
		0,
	)
	if needed <= detailHeaderSize {
		return "", false
	}

	buf := make([]byte, needed)
	*(*uint32)(unsafe.Pointer(&buf[0])) = detailHeaderSize
	ret, _, _ = procSetupDiGetInterfaceDetail.Call(
		uintptr(set),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed),
		0, 0,
	)
	if ret == 0 {
		return "", false
	}
	// DevicePath starts directly behind cbSize, at offset four, whatever the
	// padded size of the header happens to be.
	return utf16BytesToString(buf[4:]), true
}

// readBatteryDevice opens one battery and asks it what it is.
func readBatteryDevice(path string) (BatteryInfo, error) {
	var info BatteryInfo

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return info, err
	}
	// Opened for reading and writing: the queries below only read, but the
	// battery class driver refuses a handle that asks for read alone.
	handle, err := windows.CreateFile(pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return info, fmt.Errorf("open battery device: %w", err)
	}
	defer windows.CloseHandle(handle)

	// Every later query is keyed by the tag. It changes when the pack is
	// swapped, which is how the driver refuses to answer about a battery that
	// is no longer the one asked about.
	var wait, tag, returned uint32
	if err := windows.DeviceIoControl(handle, ioctlBatteryQueryTag,
		(*byte)(unsafe.Pointer(&wait)), uint32(unsafe.Sizeof(wait)),
		(*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag)),
		&returned, nil); err != nil {
		return info, fmt.Errorf("battery tag: %w", err)
	}

	query := batteryQueryInformation{Tag: tag, InformationLevel: batteryInformationLevel}
	var raw batteryInformation
	if err := windows.DeviceIoControl(handle, ioctlBatteryQueryInformation,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		(*byte)(unsafe.Pointer(&raw)), uint32(unsafe.Sizeof(raw)),
		&returned, nil); err != nil {
		return info, fmt.Errorf("battery information: %w", err)
	}

	info.Relative = raw.Capabilities&batteryCapacityRelative != 0
	info.DesignedMWh = raw.DesignedCapacity
	info.FullMWh = raw.FullChargedCapacity
	info.CycleCount = raw.CycleCount
	// Four characters, padded with spaces or with nothing at all: "LION",
	// "LiP", "NiMH", "PbAc".
	info.Chemistry = strings.TrimSpace(strings.TrimRight(string(raw.Chemistry[:]), "\x00"))

	// Voltage is a live reading rather than a fact about the pack, but it
	// comes off the same handle and nothing cheaper offers it. A controller
	// that will not say leaves it at nought, which the caller drops.
	status := batteryStatus{}
	waitStatus := batteryWaitStatus{Tag: tag}
	if err := windows.DeviceIoControl(handle, ioctlBatteryQueryStatus,
		(*byte)(unsafe.Pointer(&waitStatus)), uint32(unsafe.Sizeof(waitStatus)),
		(*byte)(unsafe.Pointer(&status)), uint32(unsafe.Sizeof(status)),
		&returned, nil); err == nil && status.Voltage != batteryUnknown {
		info.VoltageMV = status.Voltage
	}
	return info, nil
}

// utf16BytesToString reads a NUL terminated UTF-16 string out of a byte buffer
// a Win32 call filled in.
func utf16BytesToString(b []byte) string {
	chars := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := uint16(b[i]) | uint16(b[i+1])<<8
		if c == 0 {
			break
		}
		chars = append(chars, c)
	}
	return windows.UTF16ToString(chars)
}
