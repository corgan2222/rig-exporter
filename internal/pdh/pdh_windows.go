//go:build windows

// Package pdh reads Windows performance counters.
//
// Two shapes are needed, and they are different enough to be separate types.
// A Counter has a fixed path and yields one number: "% Processor Performance"
// is the only way Windows reports what a processor is actually clocked at, and
// there is exactly one of it. An Array has a wildcard in its path and yields
// one number per instance — the GPU engine counters exist once per process,
// adapter and engine, which on an ordinary machine is several hundred rows.
//
// PdhAddEnglishCounter is used throughout rather than PdhAddCounter, because
// counter paths are localised: on a German Windows the same counter is called
// "% Prozessorleistung".
//
// Nothing here treats an unavailable counter as an error worth surfacing. The
// counter subsystem can be broken on a machine with nothing else wrong with
// it, and every caller has something else to fall back on.
package pdh

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pdh = windows.NewLazySystemDLL("pdh.dll")

	procOpenQueryW               = pdh.NewProc("PdhOpenQueryW")
	procAddEnglishCounterW       = pdh.NewProc("PdhAddEnglishCounterW")
	procCollectQueryData         = pdh.NewProc("PdhCollectQueryData")
	procGetFormattedCounterValue = pdh.NewProc("PdhGetFormattedCounterValue")
	procGetFormattedCounterArray = pdh.NewProc("PdhGetFormattedCounterArrayW")
	procCloseQuery               = pdh.NewProc("PdhCloseQuery")
)

const (
	success   = 0
	fmtDouble = 0x00000200
	// moreData is PDH_MORE_DATA: the buffer was too small, and the size
	// argument now says how large it needs to be.
	moreData = 0x800007D2
)

// MinInterval is the shortest window a reading is accepted over.
//
// These are rate counters: a ratio of two deltas. Collected twice in quick
// succession both deltas are near zero, and their quotient is noise that
// happens to look like a percentage — a spike of 193 % on a processor that
// boosts to 145 % was what prompted this. The floor sits well below the
// shortest poll the exporter allows, so it only ever rejects the reading taken
// immediately after opening the counter.
const MinInterval = 100 * time.Millisecond

// counterValue mirrors PDH_FMT_COUNTERVALUE. The union is eight bytes and
// eight byte aligned, so the status field is followed by padding.
type counterValue struct {
	Status uint32
	_      uint32
	Value  float64
}

// counterValueItem mirrors PDH_FMT_COUNTERVALUE_ITEM_W: the instance name,
// then the value structure above.
type counterValueItem struct {
	Name *uint16
	counterValue
}

// query is the part a Counter and an Array have in common: an open handle,
// the baseline collection, and closing it again.
type query struct {
	mu       sync.Mutex
	handle   uintptr
	counter  uintptr
	open     bool
	lastRead time.Time
}

// openQuery hands back a pointer rather than a value: the structure carries a
// mutex, and a mutex that has been copied is a mutex that locks nothing.
func openQuery(path string) *query {
	q := &query{}
	// Load answers whether pdh.dll is there, not whether these six symbols
	// are, and LazyProc.Call panics on a symbol that is missing. Checking all
	// six here covers the package: every other call in this file runs only
	// once open is true, and only this function sets it.
	for _, p := range []*windows.LazyProc{
		procOpenQueryW, procAddEnglishCounterW, procCollectQueryData,
		procGetFormattedCounterValue, procGetFormattedCounterArray, procCloseQuery,
	} {
		if p.Find() != nil {
			return q
		}
	}

	if ret, _, _ := procOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&q.handle))); ret != success {
		return q
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		procCloseQuery.Call(q.handle)
		return q
	}
	if ret, _, _ := procAddEnglishCounterW.Call(
		q.handle,
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&q.counter)),
	); ret != success {
		procCloseQuery.Call(q.handle)
		return q
	}

	// The first collection establishes the baseline the next one is measured
	// against.
	procCollectQueryData.Call(q.handle)
	q.lastRead = time.Now()
	q.open = true
	return q
}

// collect advances the query, refusing a call that arrives before the window
// has had time to mean anything.
//
// Refused without collecting, on purpose: collecting would move the baseline
// forward, and a caller polling faster than MinInterval would then never get a
// reading at all.
func (q *query) collect() error {
	if !q.open {
		return fmt.Errorf("performance counter is not available")
	}
	if elapsed := time.Since(q.lastRead); elapsed < MinInterval {
		return fmt.Errorf("only %s since the last collection, need %s", elapsed, MinInterval)
	}
	if ret, _, _ := procCollectQueryData.Call(q.handle); ret != success {
		return fmt.Errorf("PdhCollectQueryData returned 0x%x", ret)
	}
	q.lastRead = time.Now()
	return nil
}

func (q *query) close() {
	if q.open {
		procCloseQuery.Call(q.handle)
		q.open = false
	}
}

// Counter is one performance counter with a fixed path, held open for the
// lifetime of the process.
type Counter struct{ q *query }

// NewCounter opens a counter. A counter that could not be opened is not nil;
// it simply reports an error from every Value call.
func NewCounter(path string) *Counter { return &Counter{q: openQuery(path)} }

// Value collects and reads the counter.
func (c *Counter) Value() (float64, error) {
	c.q.mu.Lock()
	defer c.q.mu.Unlock()

	if err := c.q.collect(); err != nil {
		return 0, err
	}

	var value counterValue
	ret, _, _ := procGetFormattedCounterValue.Call(
		c.q.counter,
		fmtDouble,
		0,
		uintptr(unsafe.Pointer(&value)),
	)
	if ret != success {
		return 0, fmt.Errorf("PdhGetFormattedCounterValue returned 0x%x", ret)
	}
	return value.Value, nil
}

// Close releases the query.
func (c *Counter) Close() {
	c.q.mu.Lock()
	defer c.q.mu.Unlock()
	c.q.close()
}

// Array is a counter whose path contains a wildcard, and which therefore
// yields one value per instance.
type Array struct {
	q *query
	// buf is kept between reads. The GPU engine counters expand to several
	// hundred rows, and allocating that buffer on every sample would be
	// paying for the same memory over and over.
	buf []byte
}

// NewArray opens a wildcard counter, e.g.
// `\GPU Engine(*)\Utilization Percentage`.
func NewArray(path string) *Array { return &Array{q: openQuery(path)} }

// Values collects and reads every instance, keyed by instance name.
//
// The names PDH hands back point into the buffer it just filled, so they are
// copied into the returned map before anything can reuse it.
func (a *Array) Values() (map[string]float64, error) {
	a.q.mu.Lock()
	defer a.q.mu.Unlock()

	if err := a.q.collect(); err != nil {
		return nil, err
	}

	// One attempt with the buffer from last time, which is the usual case
	// once the instance count has settled; PDH says how much it needs when
	// that is not enough.
	items, count, err := a.read()
	if err != nil {
		return nil, err
	}

	out := make(map[string]float64, count)
	for i := range items {
		item := &items[i]
		// A dead instance keeps its row with a status other than success.
		// Reading its value would report whatever was in the union.
		if item.Status != success || item.Name == nil {
			continue
		}
		out[windows.UTF16PtrToString(item.Name)] = item.Value
	}
	return out, nil
}

// read fills the buffer, growing it once if PDH asks for more room.
func (a *Array) read() ([]counterValueItem, uint32, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var size, count uint32
		var first uintptr
		if len(a.buf) > 0 {
			size = uint32(len(a.buf))
			first = uintptr(unsafe.Pointer(&a.buf[0]))
		}

		ret, _, _ := procGetFormattedCounterArray.Call(
			a.q.counter,
			fmtDouble,
			uintptr(unsafe.Pointer(&size)),
			uintptr(unsafe.Pointer(&count)),
			first,
		)
		switch {
		case ret == success:
			if count == 0 {
				return nil, 0, nil
			}
			return unsafe.Slice((*counterValueItem)(unsafe.Pointer(&a.buf[0])), count), count, nil
		case uint32(ret) == moreData:
			// Grown by a margin, because the instance count moves with the
			// processes on the machine and resizing on every sample would be
			// the same waste in slow motion.
			a.buf = make([]byte, size+size/4)
		default:
			return nil, 0, fmt.Errorf("PdhGetFormattedCounterArray returned 0x%x", ret)
		}
	}
	return nil, 0, fmt.Errorf("PdhGetFormattedCounterArray kept asking for a larger buffer")
}

// Close releases the query.
func (a *Array) Close() {
	a.q.mu.Lock()
	defer a.q.mu.Unlock()
	a.q.close()
}
