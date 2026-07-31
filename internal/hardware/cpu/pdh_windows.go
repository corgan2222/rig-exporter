//go:build windows

package cpu

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A minimal performance counter reader.
//
// It exists for one counter: "% Processor Performance", which is the only way
// Windows reports what the processor is actually clocked at. The obvious
// alternative, CallNtPowerInformation, hands back the nominal frequency on
// every modern AMD and most Intel parts — a Ryzen sitting at 4.2 GHz still
// reports its 3.4 GHz base, so the reading never moves.
//
// PdhAddEnglishCounter is used rather than PdhAddCounter: counter paths are
// localised, and on a German Windows the same counter is called
// "% Prozessorleistung".

var (
	pdh = windows.NewLazySystemDLL("pdh.dll")

	procPdhOpenQueryW               = pdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounterW       = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData         = pdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = pdh.NewProc("PdhGetFormattedCounterValue")
	procPdhCloseQuery               = pdh.NewProc("PdhCloseQuery")
)

const (
	pdhSuccess   = 0
	pdhFmtDouble = 0x00000200
)

// pdhCounterValue mirrors PDH_FMT_COUNTERVALUE. The union is eight bytes and
// eight byte aligned, so the status field is followed by padding.
type pdhCounterValue struct {
	Status uint32
	_      uint32
	Value  float64
}

// minInterval is the shortest window a reading is accepted over.
//
// This counter is a ratio of two deltas. Collected twice in quick succession
// both deltas are near zero, and their quotient is noise that happens to look
// like a percentage — a spike of 193 % on a part that boosts to 145 % was what
// prompted this. The floor sits well below the shortest poll the exporter
// allows, so it only ever rejects the reading taken immediately after opening
// the counter.
const minInterval = 100 * time.Millisecond

// perfCounter holds one open counter for the lifetime of the process.
//
// Rate counters need two collections to produce a value, so one is taken when
// the counter is opened and the reading is the difference from there on.
type perfCounter struct {
	mu       sync.Mutex
	query    uintptr
	counter  uintptr
	open     bool
	lastRead time.Time
}

// newPerfCounter opens a counter. A failure is not an error worth surfacing:
// the counter subsystem can be broken on a machine without anything else
// being wrong, and the caller has a fallback.
func newPerfCounter(path string) *perfCounter {
	c := &perfCounter{}
	if err := pdh.Load(); err != nil {
		return c
	}

	if ret, _, _ := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&c.query))); ret != pdhSuccess {
		return c
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		procPdhCloseQuery.Call(c.query)
		return c
	}
	if ret, _, _ := procPdhAddEnglishCounterW.Call(
		c.query,
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&c.counter)),
	); ret != pdhSuccess {
		procPdhCloseQuery.Call(c.query)
		return c
	}

	// The first collection establishes the baseline the next one is measured
	// against.
	procPdhCollectQueryData.Call(c.query)
	c.lastRead = time.Now()
	c.open = true
	return c
}

// Value collects and reads the counter.
//
// A call that arrives before the window has had time to mean anything is
// refused without collecting: collecting would move the baseline forward and a
// caller polling faster than minInterval would then never get a reading at all.
func (c *perfCounter) Value() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.open {
		return 0, fmt.Errorf("performance counter is not available")
	}
	if elapsed := time.Since(c.lastRead); elapsed < minInterval {
		return 0, fmt.Errorf("only %s since the last collection, need %s", elapsed, minInterval)
	}
	if ret, _, _ := procPdhCollectQueryData.Call(c.query); ret != pdhSuccess {
		return 0, fmt.Errorf("PdhCollectQueryData returned 0x%x", ret)
	}
	c.lastRead = time.Now()

	var value pdhCounterValue
	ret, _, _ := procPdhGetFormattedCounterValue.Call(
		c.counter,
		pdhFmtDouble,
		0,
		uintptr(unsafe.Pointer(&value)),
	)
	if ret != pdhSuccess {
		return 0, fmt.Errorf("PdhGetFormattedCounterValue returned 0x%x", ret)
	}
	return value.Value, nil
}

// Close releases the query.
func (c *perfCounter) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.open {
		procPdhCloseQuery.Call(c.query)
		c.open = false
	}
}
