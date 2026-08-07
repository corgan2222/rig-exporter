//go:build windows

package gpu

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ADLX is AMD's counterpart to NVML: it ships with the Adrenalin driver and
// reports what Windows itself does not expose — temperature, clocks, fan speed,
// power and video memory.
//
// Everything past the six exported entry points goes through function-pointer
// tables, the same technique dxgi_windows.go already uses, so comCall does the
// dispatching here too. The slot numbers come from the headers of the official
// SDK (github.com/GPUOpen-LibrariesAndSDKs/ADLX, SDK/Include) and are the one
// thing in this file that must not be guessed: a wrong index calls some other
// pointer inside the graphics driver.
const (
	// ADLX_RESULT. A C enum, so a signed 32-bit code.
	adlxOK = 0

	// IADLXSystem is a singleton and therefore the one interface without
	// Acquire/Release — its table starts straight at GetHybridGraphicsType
	// (ISystem.h). Getting this wrong releases something that was never
	// reference counted.
	adlxSystemGetGPUs                = 1
	adlxSystemGetPerformanceServices = 9

	// Every other interface is reference counted and begins with
	// Acquire, Release, QueryInterface. Note this is slot 1, not the slot 2
	// that COM uses — comRelease must not be reused for ADLX.
	adlxReleaseSlot = 1

	// IADLXGPUList (ISystem.h). At_GPUList hands back a typed IADLXGPU,
	// where the inherited At would hand back a bare IADLXInterface.
	adlxListSize     = 3
	adlxListBegin    = 5
	adlxListAtGPU    = 11
	adlxGPUName      = 7
	adlxGPUTotalVRAM = 11

	// IADLXPerformanceMonitoringServices (IPerformanceMonitoring.h).
	adlxPerfCurrentGPUMetrics = 18
	adlxPerfCurrentFPS        = 20

	// IADLXFPS (IPerformanceMonitoring.h).
	adlxFPSValue = 4

	// IADLXGPUMetrics (IPerformanceMonitoring.h). Slot 4 is GPUUsage, which is
	// deliberately not read — see mergeFromADLX.
	adlxMetricCoreClock   = 5
	adlxMetricMemoryClock = 6
	adlxMetricTemperature = 7
	adlxMetricHotspot     = 8
	adlxMetricPower       = 9
	adlxMetricBoardPower  = 10
	adlxMetricFanSpeedRPM = 11
	adlxMetricVRAMUsed    = 12
	adlxMetricVoltage     = 13
)

// adlx wraps the library. Every failure means "no AMD card here", which is the
// ordinary case on an NVIDIA machine and not an error worth reporting.
type adlx struct {
	once sync.Once
	ok   bool

	// system is IADLXSystem, valid for the lifetime of the process.
	//
	// ADLXTerminate is deliberately never called, matching how NVML is
	// initialised once and left running. It also sidesteps a trap: terminating
	// while any reference counted interface is still outstanding answers
	// ADLX_ORPHAN_OBJECTS and leaves every pointer obtained from ADLX dangling.
	// Nothing here outlives a single collection, so there is nothing to unwind.
	system uintptr
}

var adlxLib adlx

// load resolves and initialises ADLX once per process.
func (a *adlx) load() bool {
	a.once.Do(func() {
		dll := windows.NewLazySystemDLL("amdadlx64.dll")
		if dll.Load() != nil {
			return
		}

		// LazyProc.Call panics when a symbol is missing rather than returning
		// an error, and in a binary linked with -H windowsgui that panic takes
		// the tray down without a window or a message. Nothing is called before
		// it is known to exist.
		initialize := dll.NewProc("ADLXInitialize")
		queryVersion := dll.NewProc("ADLXQueryFullVersion")
		if initialize.Find() != nil || queryVersion.Find() != nil {
			return
		}

		// ADLX checks the caller's version against its own, so asking the
		// library which version it is and handing that straight back is the one
		// answer that cannot be refused with ADLX_BAD_VER.
		var version uint64
		if ret, _, _ := queryVersion.Call(uintptr(unsafe.Pointer(&version))); int32(ret) != adlxOK {
			return
		}

		var system uintptr
		ret, _, _ := initialize.Call(uintptr(version), uintptr(unsafe.Pointer(&system)))
		if int32(ret) != adlxOK || system == 0 {
			return
		}
		a.system, a.ok = system, true
	})
	return a.ok
}

// adlxCard is one AMD card as ADLX reports it.
//
// Each reading carries whether it was answered at all. A card that has no such
// sensor replies ADLX_NOT_SUPPORTED, which is a fact about the hardware and not
// a failure — a Radeon RX 570 answers it for hotspot temperature and voltage,
// because Polaris has neither.
type adlxCard struct {
	Index int
	Name  string

	TempC        float64
	hasTemp      bool
	HotspotC     float64
	hasHotspot   bool
	CoreClock    float64
	hasCoreClock bool
	MemClock     float64
	hasMemClock  bool
	PowerW       float64
	hasPower     bool
	FanRPM       float64
	hasFanRPM    bool
	VoltageMV    float64
	hasVoltage   bool
	VRAMUsedMB   float64
	hasVRAMUsed  bool
	VRAMTotalMB  float64
	hasVRAMTotal bool
}

// adlxCards enumerates the AMD cards and their current readings.
func adlxCards() ([]adlxCard, error) {
	if !adlxLib.load() {
		return nil, fmt.Errorf("adlx is not available")
	}
	system := adlxLib.system

	var list uintptr
	if ret := adlxCall(system, adlxSystemGetGPUs,
		uintptr(unsafe.Pointer(&list))); ret != adlxOK {
		return nil, adlxError("IADLXSystem.GetGPUs", ret)
	}
	defer adlxReleaseObject(list)

	var services uintptr
	if ret := adlxCall(system, adlxSystemGetPerformanceServices,
		uintptr(unsafe.Pointer(&services))); ret != adlxOK {
		return nil, adlxError("IADLXSystem.GetPerformanceMonitoringServices", ret)
	}
	defer adlxReleaseObject(services)

	// Size and Begin return their value directly rather than an ADLX_RESULT.
	// ADLX reports one entry per card, unlike the older ADL, which reports one
	// per display output and would turn a single card into seven.
	size := uint32(comCall(list, adlxListSize))
	begin := uint32(comCall(list, adlxListBegin))

	cards := make([]adlxCard, 0, size)
	for location := begin; location < begin+size; location++ {
		var gpu uintptr
		if ret := adlxCall(list, adlxListAtGPU, uintptr(location),
			uintptr(unsafe.Pointer(&gpu))); ret != adlxOK || gpu == 0 {
			continue
		}
		cards = append(cards, readADLXCard(len(cards), gpu, services))
		adlxReleaseObject(gpu)
	}

	if len(cards) == 0 {
		return nil, fmt.Errorf("adlx reports no graphics card")
	}
	return cards, nil
}

// ADLXFrameRate reports the frame rate the AMD driver counts itself, for the
// case RTSS is not running.
//
// Fullscreen only. Without a fullscreen application ADLX answers
// ADLX_NOT_SUPPORTED rather than a zero, and that is a statement about what it
// can measure rather than a failure — which is exactly the distinction the
// caller needs. Measured against a fullscreen Direct3D loop on a Radeon RX 570
// it tracked 55 to 62 at a 60 Hz refresh.
//
// It knows no application name and no process id. Those are RTSS's alone, and
// no amount of driver telemetry replaces them.
func ADLXFrameRate() (float64, bool) {
	if !adlxLib.load() {
		return 0, false
	}

	var services uintptr
	if ret := adlxCall(adlxLib.system, adlxSystemGetPerformanceServices,
		uintptr(unsafe.Pointer(&services))); ret != adlxOK || services == 0 {
		return 0, false
	}
	defer adlxReleaseObject(services)

	var reading uintptr
	if ret := adlxCall(services, adlxPerfCurrentFPS,
		uintptr(unsafe.Pointer(&reading))); ret != adlxOK || reading == 0 {
		return 0, false
	}
	defer adlxReleaseObject(reading)

	return adlxInt(reading, adlxFPSValue)
}

func readADLXCard(index int, gpu, services uintptr) adlxCard {
	card := adlxCard{Index: index, Name: adlxString(gpu, adlxGPUName)}

	// The total sits on the card rather than in the metrics, because it
	// describes the hardware and does not change between readings.
	var totalVRAM uint32
	if ret := adlxCall(gpu, adlxGPUTotalVRAM,
		uintptr(unsafe.Pointer(&totalVRAM))); ret == adlxOK && totalVRAM > 0 {
		card.VRAMTotalMB, card.hasVRAMTotal = float64(totalVRAM), true
	}

	var reading uintptr
	if ret := adlxCall(services, adlxPerfCurrentGPUMetrics, gpu,
		uintptr(unsafe.Pointer(&reading))); ret != adlxOK || reading == 0 {
		return card
	}
	defer adlxReleaseObject(reading)

	card.TempC, card.hasTemp = adlxDouble(reading, adlxMetricTemperature)
	card.HotspotC, card.hasHotspot = adlxDouble(reading, adlxMetricHotspot)
	card.CoreClock, card.hasCoreClock = adlxInt(reading, adlxMetricCoreClock)
	card.MemClock, card.hasMemClock = adlxInt(reading, adlxMetricMemoryClock)
	card.FanRPM, card.hasFanRPM = adlxInt(reading, adlxMetricFanSpeedRPM)
	card.VoltageMV, card.hasVoltage = adlxInt(reading, adlxMetricVoltage)
	card.VRAMUsedMB, card.hasVRAMUsed = adlxInt(reading, adlxMetricVRAMUsed)

	// Two power readings that are not the same quantity: the board figure
	// covers the whole card and is what NVML publishes and what a vendor tool
	// shows, while GPUPower is the ASIC alone. The comparable one wins, and the
	// narrower one only stands in where the board figure is missing — on
	// Polaris that is the only one of the two the card answers.
	if watts, ok := adlxDouble(reading, adlxMetricBoardPower); ok {
		card.PowerW, card.hasPower = watts, true
	} else if watts, ok := adlxDouble(reading, adlxMetricPower); ok {
		card.PowerW, card.hasPower = watts, true
	}

	return card
}

// adlxCall dispatches one vtable method and returns the ADLX_RESULT.
func adlxCall(object uintptr, slot uintptr, args ...uintptr) int32 {
	return int32(uint32(comCall(object, slot, args...)))
}

func adlxReleaseObject(object uintptr) {
	if object != 0 {
		comCall(object, adlxReleaseSlot)
	}
}

// adlxDouble reads an adlx_double output parameter.
func adlxDouble(object uintptr, slot uintptr) (float64, bool) {
	var value float64
	if ret := adlxCall(object, slot, uintptr(unsafe.Pointer(&value))); ret != adlxOK {
		return 0, false
	}
	return value, true
}

// adlxInt reads an adlx_int output parameter, which is 32 bits wide.
func adlxInt(object uintptr, slot uintptr) (float64, bool) {
	var value int32
	if ret := adlxCall(object, slot, uintptr(unsafe.Pointer(&value))); ret != adlxOK {
		return 0, false
	}
	return float64(value), true
}

// adlxString reads a const char* output parameter. The buffer belongs to ADLX
// and is copied here rather than retained.
func adlxString(object uintptr, slot uintptr) string {
	var text *byte
	if ret := adlxCall(object, slot, uintptr(unsafe.Pointer(&text))); ret != adlxOK || text == nil {
		return ""
	}
	return strings.TrimSpace(windows.BytePtrToString(text))
}

func adlxError(operation string, result int32) error {
	return fmt.Errorf("%s failed with ADLX_RESULT %d", operation, result)
}
