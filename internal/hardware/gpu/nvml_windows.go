//go:build windows

// Package gpu collects graphics card telemetry.
//
// Two sources, tried in that order: MSI Afterburner's shared memory, which
// works for NVIDIA, AMD and Intel and is usually already running on a machine
// set up for the RTSS overlay; and NVML, which ships with the NVIDIA driver
// and needs nothing installed but only covers NVIDIA cards.
//
// Windows itself exposes neither temperature nor clocks, so a machine with an
// AMD card and no Afterburner simply reports no GPU group. That is what the
// "only report what is there" behaviour is for.
package gpu

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NVML return codes that matter here.
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

	init          *windows.LazyProc
	shutdown      *windows.LazyProc
	deviceCount   *windows.LazyProc
	deviceByIndex *windows.LazyProc
	deviceName    *windows.LazyProc
	deviceUtil    *windows.LazyProc
	deviceMemory  *windows.LazyProc
	deviceTemp    *windows.LazyProc
	deviceClock   *windows.LazyProc
	devicePower   *windows.LazyProc
	powerLimit    *windows.LazyProc
	deviceFan     *windows.LazyProc
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

		n.init = n.dll.NewProc("nvmlInit_v2")
		n.shutdown = n.dll.NewProc("nvmlShutdown")
		n.deviceCount = n.dll.NewProc("nvmlDeviceGetCount_v2")
		n.deviceByIndex = n.dll.NewProc("nvmlDeviceGetHandleByIndex_v2")
		n.deviceName = n.dll.NewProc("nvmlDeviceGetName")
		n.deviceUtil = n.dll.NewProc("nvmlDeviceGetUtilizationRates")
		n.deviceMemory = n.dll.NewProc("nvmlDeviceGetMemoryInfo")
		n.deviceTemp = n.dll.NewProc("nvmlDeviceGetTemperature")
		n.deviceClock = n.dll.NewProc("nvmlDeviceGetClockInfo")
		n.devicePower = n.dll.NewProc("nvmlDeviceGetPowerUsage")
		// The enforced limit is what the card is actually held to, which is
		// the board power limit as configured — the number people mean when
		// they say TDP.
		n.powerLimit = n.dll.NewProc("nvmlDeviceGetEnforcedPowerLimit")
		n.deviceFan = n.dll.NewProc("nvmlDeviceGetFanSpeed")

		if n.init.Find() != nil {
			return
		}
		ret, _, _ := n.init.Call()
		n.ok = ret == nvmlSuccess
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
}

// nvmlCards enumerates the NVIDIA cards and their current readings.
func nvmlCards() ([]nvmlCard, error) {
	if !lib.load() {
		return nil, fmt.Errorf("nvml is not available")
	}

	var count uint32
	if ret, _, _ := lib.deviceCount.Call(uintptr(unsafe.Pointer(&count))); ret != nvmlSuccess {
		return nil, fmt.Errorf("nvmlDeviceGetCount returned %d", ret)
	}
	if count == 0 {
		return nil, fmt.Errorf("nvml reports no devices")
	}

	cards := make([]nvmlCard, 0, count)
	for i := uint32(0); i < count; i++ {
		var handle nvmlDeviceHandle
		if ret, _, _ := lib.deviceByIndex.Call(uintptr(i), uintptr(unsafe.Pointer(&handle))); ret != nvmlSuccess {
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
	if ret, _, _ := lib.deviceUtil.Call(uintptr(handle), uintptr(unsafe.Pointer(&util))); ret == nvmlSuccess {
		card.LoadPercent, card.hasLoad = float64(util.GPU), true
	}

	var mem nvmlMemory
	if ret, _, _ := lib.deviceMemory.Call(uintptr(handle), uintptr(unsafe.Pointer(&mem))); ret == nvmlSuccess {
		const mb = 1024 * 1024
		card.VRAMUsedMB = float64(mem.Used / mb)
		card.VRAMTotalMB = float64(mem.Total / mb)
		card.hasVRAM = true
	}

	var temp uint32
	if ret, _, _ := lib.deviceTemp.Call(uintptr(handle), nvmlTempGPU, uintptr(unsafe.Pointer(&temp))); ret == nvmlSuccess {
		card.TempC, card.hasTemp = float64(temp), true
	}

	var clock uint32
	if ret, _, _ := lib.deviceClock.Call(uintptr(handle), nvmlClockGraphics, uintptr(unsafe.Pointer(&clock))); ret == nvmlSuccess {
		card.CoreClock = float64(clock)
	}
	if ret, _, _ := lib.deviceClock.Call(uintptr(handle), nvmlClockMem, uintptr(unsafe.Pointer(&clock))); ret == nvmlSuccess {
		card.MemClock = float64(clock)
	}

	var milliwatts uint32
	if ret, _, _ := lib.devicePower.Call(uintptr(handle), uintptr(unsafe.Pointer(&milliwatts))); ret == nvmlSuccess {
		card.PowerW, card.hasPower = float64(milliwatts)/1000, true
	}

	var limit uint32
	if ret, _, _ := lib.powerLimit.Call(uintptr(handle), uintptr(unsafe.Pointer(&limit))); ret == nvmlSuccess {
		card.PowerLimitW, card.hasLimit = float64(limit)/1000, true
	}

	var fan uint32
	// Cards without a fan report NOT_SUPPORTED, which is not a failure.
	if ret, _, _ := lib.deviceFan.Call(uintptr(handle), uintptr(unsafe.Pointer(&fan))); ret == nvmlSuccess {
		card.FanPercent, card.hasFan = float64(fan), true
	}

	return card
}

func cardName(handle nvmlDeviceHandle) string {
	buf := make([]byte, 96)
	ret, _, _ := lib.deviceName.Call(uintptr(handle), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret != nvmlSuccess {
		return "NVIDIA GPU"
	}
	for i, c := range buf {
		if c == 0 {
			return strings.TrimSpace(string(buf[:i]))
		}
	}
	return strings.TrimSpace(string(buf))
}
