//go:build windows

// Package gpu collects graphics card inventory and telemetry.
//
// Windows DXGI supplies the adapter identity and memory limits on every
// supported machine, including integrated Intel laptop graphics. MSI
// Afterburner's shared memory adds live readings for NVIDIA, AMD and Intel.
// The two vendor libraries fill the rest without needing another program
// running: NVML ships with the NVIDIA driver, ADLX with AMD's.
//
// Windows itself exposes neither temperature nor clocks through DXGI, so a
// card without a live source reports its inventory and leaves those readings
// out. That is what the "only report what is there" behaviour is for.
package gpu

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NVML return codes that matter here.
//
// nvmlErrorNotSupported is never compared against, on purpose. A card that does
// not expose a value and a call that failed are treated the same way: the
// reading is left out. Naming the code keeps the reader from concluding that
// the distinction was overlooked — it was considered and found not to make a
// difference to anything this program does.
const (
	nvmlSuccess           = 0
	nvmlErrorNotSupported = 3
)

// nvmlDeviceHandle is an opaque pointer NVML hands back per card.
type nvmlDeviceHandle uintptr

// nvmlUtilization mirrors nvmlUtilization_t.
type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

// nvmlMemory mirrors nvmlMemory_t, in bytes.
type nvmlMemory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// NVML clock and temperature selectors.
const (
	nvmlClockGraphics = 0
	nvmlClockMem      = 2
	nvmlTempGPU       = 0
)

// nvml wraps the subset of the NVIDIA management library this needs.
//
// The DLL is resolved lazily and every failure is treated as "no NVIDIA card
// here", because that is the common case on an AMD machine and not an error.
type nvml struct {
	once sync.Once
	ok   bool

	dll *windows.LazyDLL

	init proc
	// shutdown is resolved and never called. nvmlShutdown is deliberately not
	// invoked: NVML is initialised once and left running for the lifetime of
	// the process, the same decision ADLX documents at adlx_windows.go. It is
	// resolved so the day somebody needs it, the reason it is unused is written
	// down here rather than rediscovered.
	shutdown      proc
	deviceCount   proc
	deviceByIndex proc
	deviceName    proc
	deviceUtil    proc
	deviceMemory  proc
	deviceTemp    proc
	deviceClock   proc
	devicePower   proc
	powerLimit    proc
	deviceFan     proc
	deviceFanRPM  proc
}

// proc is one NVML entry point together with whether this driver actually has
// it.
//
// windows.LazyProc.Call resolves the symbol through mustFind, which panics
// rather than returning an error. In a binary linked with -H windowsgui a panic
// inside the collection goroutine takes the tray down without a window, a
// message or an exit code. NVML grows new entry points with every driver
// generation and this code has to run on old ones, so nothing is called before
// it is known to exist.
type proc struct {
	p  *windows.LazyProc
	ok bool
}

// call runs the entry point, reporting false if this driver does not have it.
// The NVML return code is only meaningful when it does.
func (c proc) call(args ...uintptr) (uintptr, bool) {
	if !c.ok {
		return 0, false
	}
	ret, _, _ := c.p.Call(args...)
	return ret, true
}

// succeeded folds the two questions into the one the call sites ask: did this
// produce a usable value?
func (c proc) succeeded(args ...uintptr) bool {
	ret, ok := c.call(args...)
	return ok && ret == nvmlSuccess
}

func (n *nvml) resolve(name string) proc {
	p := n.dll.NewProc(name)
	return proc{p: p, ok: p.Find() == nil}
}

var lib nvml

// load initialises NVML once per process.
func (n *nvml) load() bool {
	n.once.Do(func() {
		n.dll = windows.NewLazySystemDLL("nvml.dll")
		if err := n.dll.Load(); err != nil {
			// Not on the system path: try where the driver installs it.
			n.dll = windows.NewLazyDLL(`C:\Program Files\NVIDIA Corporation\NVSMI\nvml.dll`)
			if err := n.dll.Load(); err != nil {
				return
			}
		}

		n.init = n.resolve("nvmlInit_v2")
		n.shutdown = n.resolve("nvmlShutdown")
		n.deviceCount = n.resolve("nvmlDeviceGetCount_v2")
		n.deviceByIndex = n.resolve("nvmlDeviceGetHandleByIndex_v2")
		n.deviceName = n.resolve("nvmlDeviceGetName")
		n.deviceUtil = n.resolve("nvmlDeviceGetUtilizationRates")
		n.deviceMemory = n.resolve("nvmlDeviceGetMemoryInfo")
		n.deviceTemp = n.resolve("nvmlDeviceGetTemperature")
		n.deviceClock = n.resolve("nvmlDeviceGetClockInfo")
		n.devicePower = n.resolve("nvmlDeviceGetPowerUsage")
		// The enforced limit is what the card is actually held to, which is
		// the board power limit as configured — the number people mean when
		// they say TDP.
		n.powerLimit = n.resolve("nvmlDeviceGetEnforcedPowerLimit")
		n.deviceFan = n.resolve("nvmlDeviceGetFanSpeed")
		// Tachometer readings arrived long after the rest of this list and are
		// missing on older drivers, which is what the availability check is for.
		n.deviceFanRPM = n.resolve("nvmlDeviceGetFanSpeedRPM")

		n.ok = n.init.succeeded()
	})
	return n.ok
}

// nvmlCard is one NVIDIA card as NVML reports it.
type nvmlCard struct {
	Index       int
	Name        string
	LoadPercent float64
	hasLoad     bool
	TempC       float64
	hasTemp     bool
	CoreClock   float64
	MemClock    float64
	VRAMUsedMB  float64
	VRAMTotalMB float64
	hasVRAM     bool
	PowerW      float64
	hasPower    bool
	PowerLimitW float64
	hasLimit    bool
	FanPercent  float64
	hasFan      bool
	FanRPM      float64
	hasFanRPM   bool
}

// nvmlFanSpeedInfo mirrors nvmlFanSpeedInfo_t: a versioned struct, where the
// caller writes the version and the fan index and NVML fills in the speed.
//
// The version constant encodes the struct size in its low bytes, so NVML
// answers ARGUMENT_VERSION_MISMATCH rather than reading past the end if the two
// ever disagree. Verified against the installed driver: 0x0100000C returns a
// speed, and five neighbouring values all return 25.
type nvmlFanSpeedInfo struct {
	Version uint32
	Fan     uint32
	Speed   uint32
}

const (
	nvmlFanSpeedInfoV1 = 0x0100000C
	// maxFansPerCard bounds the tachometer probe. NVML answers
	// INVALID_ARGUMENT one past the last fan, which is what actually ends the
	// loop; this only keeps a driver that answers differently from spinning.
	maxFansPerCard = 8
)

// The version constant carries the struct size, so the two must not drift.
var _ = [1]struct{}{}[unsafe.Sizeof(nvmlFanSpeedInfo{})-(nvmlFanSpeedInfoV1&0xFFFF)]

// nvmlCards enumerates the NVIDIA cards and their current readings.
func nvmlCards() ([]nvmlCard, error) {
	if !lib.load() {
		return nil, fmt.Errorf("nvml is not available")
	}

	var count uint32
	if !lib.deviceCount.succeeded(uintptr(unsafe.Pointer(&count))) {
		return nil, fmt.Errorf("nvmlDeviceGetCount did not report a device count")
	}
	if count == 0 {
		return nil, fmt.Errorf("nvml reports no devices")
	}

	cards := make([]nvmlCard, 0, count)
	for i := uint32(0); i < count; i++ {
		var handle nvmlDeviceHandle
		if !lib.deviceByIndex.succeeded(uintptr(i), uintptr(unsafe.Pointer(&handle))) {
			continue
		}
		cards = append(cards, readCard(int(i), handle))
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("no nvml device could be opened")
	}
	return cards, nil
}

func readCard(index int, handle nvmlDeviceHandle) nvmlCard {
	card := nvmlCard{Index: index, Name: cardName(handle)}

	var util nvmlUtilization
	if lib.deviceUtil.succeeded(uintptr(handle), uintptr(unsafe.Pointer(&util))) {
		card.LoadPercent, card.hasLoad = float64(util.GPU), true
	}

	var mem nvmlMemory
	if lib.deviceMemory.succeeded(uintptr(handle), uintptr(unsafe.Pointer(&mem))) {
		const mb = 1024 * 1024
		card.VRAMUsedMB = float64(mem.Used / mb)
		card.VRAMTotalMB = float64(mem.Total / mb)
		card.hasVRAM = true
	}

	var temp uint32
	if lib.deviceTemp.succeeded(uintptr(handle), nvmlTempGPU, uintptr(unsafe.Pointer(&temp))) {
		card.TempC, card.hasTemp = float64(temp), true
	}

	var clock uint32
	if lib.deviceClock.succeeded(uintptr(handle), nvmlClockGraphics, uintptr(unsafe.Pointer(&clock))) {
		card.CoreClock = float64(clock)
	}
	if lib.deviceClock.succeeded(uintptr(handle), nvmlClockMem, uintptr(unsafe.Pointer(&clock))) {
		card.MemClock = float64(clock)
	}

	var milliwatts uint32
	if lib.devicePower.succeeded(uintptr(handle), uintptr(unsafe.Pointer(&milliwatts))) {
		card.PowerW, card.hasPower = float64(milliwatts)/1000, true
	}

	var limit uint32
	if lib.powerLimit.succeeded(uintptr(handle), uintptr(unsafe.Pointer(&limit))) {
		card.PowerLimitW, card.hasLimit = float64(limit)/1000, true
	}

	var fan uint32
	// Cards without a fan report NOT_SUPPORTED, which is not a failure.
	if lib.deviceFan.succeeded(uintptr(handle), uintptr(unsafe.Pointer(&fan))) {
		card.FanPercent, card.hasFan = float64(fan), true
	}

	card.FanRPM, card.hasFanRPM = fanRPM(handle)

	return card
}

// fanRPM reads the tachometer of every fan on the card and reports the fastest.
//
// The fastest rather than an average: a card idling with one fan parked and one
// spinning would otherwise publish a speed at which no fan is actually turning,
// and a number that is wrong is worse than one that is missing.
//
// A speed of zero is a real reading and is published — modern cards stop their
// fans entirely below a temperature threshold, and a sensor that vanishes at
// idle is a sensor nobody can build on.
func fanRPM(handle nvmlDeviceHandle) (float64, bool) {
	fastest, found := uint32(0), false

	for fan := uint32(0); fan < maxFansPerCard; fan++ {
		info := nvmlFanSpeedInfo{Version: nvmlFanSpeedInfoV1, Fan: fan}

		ret, ok := lib.deviceFanRPM.call(uintptr(handle), uintptr(unsafe.Pointer(&info)))
		if !ok {
			return 0, false // driver predates the tachometer API
		}
		if ret != nvmlSuccess {
			// The first index past the last fan answers INVALID_ARGUMENT, and a
			// card with no fan at all answers NOT_SUPPORTED. Both mean there is
			// nothing further to ask about, and neither is an error worth
			// reporting. NVML leaves the struct untouched on failure, so the
			// speed must not be read here.
			break
		}
		if info.Speed > fastest {
			fastest = info.Speed
		}
		found = true
	}
	return float64(fastest), found
}

func cardName(handle nvmlDeviceHandle) string {
	buf := make([]byte, 96)
	if !lib.deviceName.succeeded(uintptr(handle), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) {
		return "NVIDIA GPU"
	}
	for i, c := range buf {
		if c == 0 {
			return strings.TrimSpace(string(buf[:i]))
		}
	}
	return strings.TrimSpace(string(buf))
}
