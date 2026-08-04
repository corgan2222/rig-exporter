//go:build windows

// Package cpu collects processor detail: the model, how many cores and
// threads it has, the clock it is actually running at, and optionally the load
// of each logical processor.
//
// The overall CPU load is not here — that is part of the core group, which is
// always collected.
package cpu

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/corgan2222/rig-exporter/internal/hardware/afterburner"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// Source collects the CPU group.
type Source struct {
	perCore bool

	load     LoadAverage
	lastLoad time.Time

	// Static facts are read once: the model name, core counts and base clock
	// do not change while the machine is running.
	once      sync.Once
	model     string
	physical  int
	logical   int
	baseMHz   float64
	staticErr error

	// performance reports what the processor is actually clocked at, relative
	// to its base frequency.
	performance *perfCounter
	// peakMHz is the highest effective clock seen since the process started,
	// which is the only honest "maximum" available: Windows reports the base
	// frequency as the maximum and never mentions the boost clock.
	peakMHz float64

	// Per-core utilisation is a difference between two samples, so the
	// previous one has to be kept.
	mu       sync.Mutex
	lastCore []processorTimes

	afterburner afterburner.Reader
}

// New builds the CPU source. perCore adds one reading per logical processor,
// which on a 16-core machine is 32 extra entities and is therefore opt-in.
func New(perCore bool) *Source {
	return &Source{perCore: perCore}
}

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupCPU }

// Collect appends the CPU readings.
func (s *Source) Collect(set *metrics.Set) error {
	s.once.Do(s.readStatic)

	if s.model != "" {
		set.Add(metrics.Text(metrics.CPUModel, "", s.model))
		if vendor := vendorOf(s.model); vendor != "" {
			set.Add(metrics.Text(metrics.CPUVendor, "", vendor))
		}
	}
	if s.physical > 0 {
		set.Add(metrics.Gauge(metrics.CPUCoresPhysical, "", float64(s.physical)))
	}
	if s.logical > 0 {
		set.Add(metrics.Gauge(metrics.CPUThreads, "", float64(s.logical)))
	}

	s.collectClock(set)

	// Processor temperature needs a driver, which Windows does not provide.
	// Afterburner does, when it happens to be running — but only if nothing
	// already supplied it. A kernel-backed source runs before this one and
	// reads the register directly; two sources writing the same measurement
	// would put it in the set twice.
	if !set.Has(metrics.CPUTemperature.ID) {
		if temp, ok := s.temperature(); ok {
			// Everything else here is Windows; this one value is not, and the
			// interface should say so rather than let it pass as native.
			reading := metrics.Gauge(metrics.CPUTemperature, "", temp)
			reading.Origin = "MSI Afterburner"
			set.Add(reading)
		}
	}

	s.collectLoad(set)

	if s.perCore {
		s.collectPerCore(set)
	}

	if s.model == "" && s.logical == 0 {
		return fmt.Errorf("no processor detail available: %w", s.staticErr)
	}
	return nil
}

// vendorOf reads the manufacturer out of the processor's brand string.
//
// The brand string is what the part calls itself and always names its maker;
// CPUID's vendor field would give "AuthenticAMD" and "GenuineIntel", which is
// not what anyone wants to see in an automation.
func vendorOf(model string) string {
	lower := strings.ToLower(model)
	for _, candidate := range []struct{ match, vendor string }{
		{"amd", "AMD"},
		{"ryzen", "AMD"},
		{"threadripper", "AMD"},
		{"epyc", "AMD"},
		{"athlon", "AMD"},
		{"intel", "Intel"},
		{"qualcomm", "Qualcomm"},
		{"snapdragon", "Qualcomm"},
		{"apple", "Apple"},
	} {
		if strings.Contains(lower, candidate.match) {
			return candidate.vendor
		}
	}
	return ""
}

// readStatic reads the facts that cannot change at runtime.
func (s *Source) readStatic() {
	s.logical = int(logicalProcessors())
	s.physical = physicalCores()
	s.performance = newPerfCounter(`\Processor Information(_Total)\% Processor Performance`)

	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		s.staticErr = fmt.Errorf("open processor registry key: %w", err)
		return
	}
	defer key.Close()

	if name, _, err := key.GetStringValue("ProcessorNameString"); err == nil {
		// The registry pads the model name out to a fixed width.
		s.model = strings.Join(strings.Fields(name), " ")
	} else {
		s.staticErr = err
	}
	// ~MHz is the nominal frequency the performance counter is relative to.
	if mhz, _, err := key.GetIntegerValue("~MHz"); err == nil {
		s.baseMHz = float64(mhz)
	}
}

// collectClock reports what the processor is running at.
//
// The performance counter gives a percentage of the base frequency and goes
// well past a hundred when the part boosts, which is the whole point: the
// power information API reports the base frequency and never moves off it.
// When the counter is unavailable, that static value is still better than
// nothing.
func (s *Source) collectClock(set *metrics.Set) {
	nominal, max, powerErr := clocks()
	if s.baseMHz == 0 {
		s.baseMHz = max
	}
	if s.baseMHz > 0 {
		set.Add(metrics.Gauge(metrics.CPUClockBase, "", s.baseMHz))
	}

	current := nominal
	if s.performance != nil && s.baseMHz > 0 {
		if percent, err := s.performance.Value(); err == nil && percent > 0 {
			current = s.baseMHz * percent / 100
		}
	}
	if current <= 0 {
		if powerErr != nil {
			return
		}
		current = nominal
	}

	set.Add(metrics.Gauge(metrics.CPUClock, "", current))

	if current > s.peakMHz {
		s.peakMHz = current
	}
	set.Add(metrics.Gauge(metrics.CPUClockMax, "", s.peakMHz))
}

// Close releases the performance counter.
func (s *Source) Close() {
	if s.performance != nil {
		s.performance.Close()
	}
}

// temperature reads the processor temperature from Afterburner, which is the
// only source here that has a driver capable of it. Windows exposes none.
func (s *Source) temperature() (float64, bool) {
	snap, err := s.afterburner.Read()
	if err != nil {
		return 0, false
	}
	// Afterburner spells this differently depending on the platform: a package
	// sensor on some, per-core sensors numbered from one on others.
	entry, ok := snap.Find("CPU temperature", "CPU package temperature", "CPU1 temperature")
	if !ok {
		return 0, false
	}
	return entry.Value, true
}

// collectLoad folds the current utilisation into the three averages.
//
// The utilisation is taken from what the core group already put in the set
// rather than measured again: it is a difference between two samples, and a
// second reader would take half the interval from the first.
func (s *Source) collectLoad(set *metrics.Set) {
	if s.logical == 0 {
		return
	}
	reading, ok := set.Find(metrics.CPULoad.ID, "")
	if !ok {
		return
	}
	percent := reading.Number

	now := time.Now()
	elapsed := 0.0
	if !s.lastLoad.IsZero() {
		elapsed = now.Sub(s.lastLoad).Seconds()
	}
	s.lastLoad = now

	s.load.Add(percent/100*float64(s.logical), elapsed)

	values, ready := s.load.Values()
	if !ready {
		return
	}
	set.Add(
		metrics.Gauge(metrics.CPULoad1, "", values[0]),
		metrics.Gauge(metrics.CPULoad5, "", values[1]),
		metrics.Gauge(metrics.CPULoad15, "", values[2]),
	)
}

func (s *Source) collectPerCore(set *metrics.Set) {
	current, err := processorPerformance()
	if err != nil {
		return
	}

	s.mu.Lock()
	previous := s.lastCore
	s.lastCore = current
	s.mu.Unlock()

	// The first collection has no baseline to subtract from.
	if len(previous) != len(current) {
		return
	}

	for i := range current {
		busy, ok := busyPercent(previous[i], current[i])
		if !ok {
			continue
		}
		set.Add(metrics.Gauge(metrics.CPUCoreLoad, strconv.Itoa(i), busy))
	}
}

// busyPercent turns two samples of one processor's time counters into a
// utilisation percentage.
func busyPercent(previous, current processorTimes) (float64, bool) {
	idle := current.Idle - previous.Idle
	kernel := current.Kernel - previous.Kernel
	user := current.User - previous.User

	// Kernel time includes idle time, exactly as in GetSystemTimes.
	total := kernel + user
	if total <= 0 {
		return 0, false
	}
	busy := float64(total-idle) / float64(total) * 100
	if busy < 0 {
		busy = 0
	}
	if busy > 100 {
		busy = 100
	}
	return busy, true
}

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	powrprof = windows.NewLazySystemDLL("powrprof.dll")
	ntdll    = windows.NewLazySystemDLL("ntdll.dll")

	procGetLogicalProcessorInformationEx = kernel32.NewProc("GetLogicalProcessorInformationEx")
	procGetActiveProcessorCount          = kernel32.NewProc("GetActiveProcessorCount")
	procCallNtPowerInformation           = powrprof.NewProc("CallNtPowerInformation")
	procNtQuerySystemInformation         = ntdll.NewProc("NtQuerySystemInformation")
)

const allProcessorGroups = 0xFFFF

// logicalProcessors counts the logical processors across all processor groups,
// which matters on machines with more than 64 of them.
func logicalProcessors() uint32 {
	count, _, _ := procGetActiveProcessorCount.Call(allProcessorGroups)
	return uint32(count)
}

// relationProcessorCore selects physical cores in GetLogicalProcessorInformationEx.
const relationProcessorCore = 0

// physicalCores counts entries in the processor-core relationship list.
//
// The records are variable length, so the buffer is walked using each record's
// own size field rather than a fixed stride.
func physicalCores() int {
	var size uint32
	procGetLogicalProcessorInformationEx.Call(relationProcessorCore, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return 0
	}

	buf := make([]byte, size)
	ret, _, _ := procGetLogicalProcessorInformationEx.Call(
		relationProcessorCore,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		return 0
	}

	count, offset := 0, uint32(0)
	for offset+8 <= size {
		// Layout: DWORD Relationship, DWORD Size, then the union.
		recordSize := *(*uint32)(unsafe.Pointer(&buf[offset+4]))
		if recordSize < 8 || offset+recordSize > size {
			break
		}
		count++
		offset += recordSize
	}
	return count
}

// processorPowerInformation mirrors PROCESSOR_POWER_INFORMATION.
type processorPowerInformation struct {
	Number           uint32
	MaxMhz           uint32
	CurrentMhz       uint32
	MhzLimit         uint32
	MaxIdleState     uint32
	CurrentIdleState uint32
}

// processorInformationLevel selects PROCESSOR_POWER_INFORMATION.
const processorInformationLevel = 11

// clocks returns the average current clock and the maximum clock, in MHz.
//
// CallNtPowerInformation is used rather than a performance counter because it
// needs no counter subsystem and reports the real P-state per core.
func clocks() (current, max float64, err error) {
	count := logicalProcessors()
	if count == 0 {
		return 0, 0, fmt.Errorf("no processors reported")
	}

	info := make([]processorPowerInformation, count)
	size := uint32(uintptr(count) * unsafe.Sizeof(info[0]))

	// A non-zero NTSTATUS means the call failed.
	status, _, _ := procCallNtPowerInformation.Call(
		processorInformationLevel,
		0, 0,
		uintptr(unsafe.Pointer(&info[0])),
		uintptr(size),
	)
	if status != 0 {
		return 0, 0, fmt.Errorf("CallNtPowerInformation returned 0x%x", status)
	}

	var sum float64
	for _, cpu := range info {
		sum += float64(cpu.CurrentMhz)
		if float64(cpu.MaxMhz) > max {
			max = float64(cpu.MaxMhz)
		}
	}
	return sum / float64(count), max, nil
}

// processorTimes mirrors SYSTEM_PROCESSOR_PERFORMANCE_INFORMATION.
type processorTimes struct {
	Idle           int64
	Kernel         int64
	User           int64
	DPC            int64
	Interrupt      int64
	InterruptCount uint32
	_              uint32 // padding to the 8 byte alignment the kernel uses
}

// systemProcessorPerformanceInformation is the SYSTEM_INFORMATION_CLASS value
// for the per-processor time counters.
const systemProcessorPerformanceInformation = 8

// processorPerformance reads the cumulative time counters of every logical
// processor.
func processorPerformance() ([]processorTimes, error) {
	count := logicalProcessors()
	if count == 0 {
		return nil, fmt.Errorf("no processors reported")
	}

	out := make([]processorTimes, count)
	size := uint32(uintptr(count) * unsafe.Sizeof(out[0]))

	var returned uint32
	status, _, _ := procNtQuerySystemInformation.Call(
		systemProcessorPerformanceInformation,
		uintptr(unsafe.Pointer(&out[0])),
		uintptr(size),
		uintptr(unsafe.Pointer(&returned)),
	)
	if status != 0 {
		return nil, fmt.Errorf("NtQuerySystemInformation returned 0x%x", status)
	}

	if actual := int(returned) / int(unsafe.Sizeof(out[0])); actual < len(out) {
		out = out[:actual]
	}
	return out, nil
}
