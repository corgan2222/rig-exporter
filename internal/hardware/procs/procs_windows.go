//go:build windows

// Package procs ranks the programs using the most CPU and memory.
//
// It answers "what was eating this machine at nine last night", which is the
// one question the per-device values cannot: they say the processor was at
// eighty per cent, never who put it there.
package procs

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// systemNames are accounted for but are not programs anybody chose to run, and
// letting them into a top five would push out everything the list is for.
//
// Idle is the accounting bucket for unused processor time and would win the CPU
// ranking on an idle machine by a wide margin. Memory Compression and vmmem are
// the kernel and the hypervisor holding memory on behalf of everything else.
var systemNames = map[string]bool{
	"Idle":               true,
	"System":             true,
	"Memory Compression": true,
	"Registry":           true,
	"vmmem":              true,
	"vmmemWSL":           true,
}

// Usage is one completed ranking.
type Usage struct {
	CPU    []metrics.Row
	Memory []metrics.Row
	At     time.Time
	// Err says why there is nothing, when there is nothing.
	Err string
}

// Sampler ranks processes on its own schedule.
//
// Separate from the collection loop for the same reason the latency probe is:
// one pass reads every process on the machine, measured at about 19 ms for 665
// of them. On a one-second loop that is two per cent of a core spent, and 19 ms
// of the interval blocked, to answer a question nobody asks that often.
type Sampler struct {
	count    int
	interval time.Duration
	log      *slog.Logger

	mu     sync.RWMutex
	usage  Usage
	have   bool
	last   map[uint32]uint64
	lastAt time.Time

	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	started atomic.Bool
}

// New builds a sampler. count is how many programs the ranking keeps.
func New(count int, interval time.Duration, log *slog.Logger) *Sampler {
	if count <= 0 {
		count = 5
	}
	if interval < time.Second {
		interval = 10 * time.Second
	}
	return &Sampler{
		count:    count,
		interval: interval,
		log:      log,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start begins sampling. Calling it twice is harmless.
func (s *Sampler) Start() {
	if s.started.CompareAndSwap(false, true) {
		go s.run()
	}
}

// Stop ends sampling, and returns immediately if it never began.
func (s *Sampler) Stop() {
	s.once.Do(func() {
		close(s.stop)
		if s.started.Load() {
			<-s.done
		}
	})
}

// Result is the most recent ranking, and whether one has completed.
func (s *Sampler) Result() (Usage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usage, s.have
}

func (s *Sampler) run() {
	defer close(s.done)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.sample() // establishes the baseline; it reports no CPU yet
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.sample()
		}
	}
}

func (s *Sampler) sample() {
	now := time.Now()
	processes, err := winapi.Processes()
	if err != nil {
		s.record(Usage{At: now, Err: err.Error()})
		return
	}

	s.mu.Lock()
	previous, previousAt := s.last, s.lastAt
	current := make(map[uint32]uint64, len(processes))
	for _, p := range processes {
		current[p.PID] = p.CPUTime
	}
	s.last, s.lastAt = current, now
	s.mu.Unlock()

	elapsed := now.Sub(previousAt).Seconds()
	usage := Usage{At: now}
	if previous != nil && elapsed > 0 {
		usage.CPU = topCPU(processes, previous, elapsed, s.count)
	}
	if _, totalBytes, _, err := winapi.MemoryStatus(); err == nil {
		usage.Memory = topMemory(processes, totalBytes, s.count)
	}
	s.record(usage)
}

func (s *Sampler) record(usage Usage) {
	s.mu.Lock()
	s.usage, s.have = usage, true
	s.mu.Unlock()

	if usage.Err != "" {
		s.log.Debug("process ranking unavailable", "error", usage.Err)
	}
}

// topCPU ranks programs by the processor time they used over the window.
//
// The share is of the whole machine, the same denominator Task Manager uses: a
// program pinning one thread of thirty-two reads as three per cent, not a
// hundred. Anything else would make the numbers of two machines with different
// core counts incomparable.
func topCPU(processes []winapi.ProcessSample, previous map[uint32]uint64, elapsed float64, count int) []metrics.Row {
	capacity := elapsed * float64(winapi.LogicalProcessors())
	if capacity <= 0 {
		return nil
	}

	byName := map[string]float64{}
	for _, p := range processes {
		name := programName(p)
		if name == "" {
			continue
		}
		before, seen := previous[p.PID]
		// A process that started inside the window has no baseline. Counting it
		// from zero would credit it with everything it did since it launched,
		// which for a compiler is several seconds in a two-second window.
		if !seen || p.CPUTime < before {
			continue
		}
		// 100-nanosecond units to seconds.
		byName[name] += float64(p.CPUTime-before) / 1e7
	}

	rows := make([]metrics.Row, 0, len(byName))
	for name, seconds := range byName {
		rows = append(rows, metrics.Row{Label: name, Value: seconds / capacity * 100})
	}
	return rank(rows, count)
}

// topMemory ranks programs by memory they do not share with anyone.
func topMemory(processes []winapi.ProcessSample, totalBytes uint64, count int) []metrics.Row {
	if totalBytes == 0 {
		return nil
	}

	byName := map[string]uint64{}
	for _, p := range processes {
		name := programName(p)
		if name == "" {
			continue
		}
		byName[name] += p.PrivateBytes
	}

	rows := make([]metrics.Row, 0, len(byName))
	for name, bytes := range byName {
		rows = append(rows, metrics.Row{Label: name, Value: float64(bytes) / float64(totalBytes) * 100})
	}
	return rank(rows, count)
}

// programName is what a person calls the thing, which is the executable rather
// than the process: one browser is one entry, not the twenty-eight tabs it
// spread itself over.
func programName(p winapi.ProcessSample) string {
	name := strings.TrimSpace(p.Name)
	if name == "" || systemNames[name] {
		return ""
	}
	return name
}

// rank sorts by value and keeps the top count, breaking ties by name so the
// order does not wobble between two collections that measured the same thing.
func rank(rows []metrics.Row, count int) []metrics.Row {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Value != rows[j].Value {
			return rows[i].Value > rows[j].Value
		}
		return rows[i].Label < rows[j].Label
	})
	if len(rows) > count {
		rows = rows[:count]
	}
	return rows
}

// Source turns the most recent ranking into readings.
type Source struct{ sampler *Sampler }

// NewSource wraps a sampler for the collector.
func NewSource(sampler *Sampler) *Source { return &Source{sampler: sampler} }

// Group files the rankings with the core values: they describe the machine as a
// whole, not one piece of hardware in it.
func (s *Source) Group() metrics.Group { return metrics.GroupCore }

// Collect adds whatever the last sampling pass produced.
func (s *Source) Collect(set *metrics.Set) error {
	usage, ok := s.sampler.Result()
	if !ok {
		return nil // nothing sampled yet; the first pass is seconds away
	}
	if usage.Err != "" {
		return fmt.Errorf("process ranking: %s", usage.Err)
	}

	set.Add(
		metrics.Table(metrics.TopCPU, "", usage.CPU),
		metrics.Table(metrics.TopMemory, "", usage.Memory),
	)
	return nil
}

// Close ends the sampling goroutine when the source set is rebuilt.
func (s *Source) Close() { s.sampler.Stop() }
