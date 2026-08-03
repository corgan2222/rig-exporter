// Package rtss reads the RTSSSharedMemoryV2 block published by RivaTuner
// Statistics Server (the FPS counter behind MSI Afterburner's overlay).
//
// The parsing half is platform independent so it can be tested without RTSS
// installed; reader_windows.go supplies the actual memory mapping.
package rtss

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// MappingName is the shared memory object RTSS publishes.
const MappingName = "RTSSSharedMemoryV2"

// Header layout of RTSS_SHARED_MEMORY. Only the fields up to the app array
// descriptor are needed; everything after it is located through those.
const (
	signature  = 0x52545353 // 'RTSS'
	headerSize = 36

	offSignature    = 0
	offVersion      = 4
	offAppEntrySize = 8
	offAppArrOffset = 12
	offAppArrSize   = 16 // entry count, not bytes
)

// Layout of RTSS_SHARED_MEMORY_APP_ENTRY. Offsets are fixed by the RTSS ABI.
const (
	offProcessID        = 0
	offName             = 4
	nameLen             = 260 // MAX_PATH
	offFlags            = 264
	offTime0            = 268
	offTime1            = 272
	offFrames           = 276
	offFrameTime        = 280
	offStatFramerateAvg = 308

	// entryMinSize is how much of an entry this package reads.
	entryMinSize = 312
)

// Sanity limits, so a corrupt header cannot make us allocate wildly.
const (
	maxEntries   = 8192
	maxEntrySize = 1 << 16
	maxMapping   = 256 << 20
)

// Errors callers are expected to branch on.
var (
	// ErrNotRunning means the shared memory object does not exist, which in
	// practice means RTSS is not running.
	ErrNotRunning = errors.New("RivaTuner Statistics Server is not running")
	// ErrAccessDenied means RTSS runs at a higher integrity level than us.
	ErrAccessDenied = errors.New("access to the RTSS shared memory was denied")
	// ErrBadSignature means something else owns that shared memory name.
	ErrBadSignature = errors.New("RTSS shared memory has an unexpected signature")
	// ErrTruncated means the mapping is smaller than the header it advertises.
	ErrTruncated = errors.New("RTSS shared memory is truncated")
)

// Entry is one process RTSS has hooked.
type Entry struct {
	ProcessID uint32
	// Path is the executable path exactly as RTSS reports it.
	Path  string
	Flags uint32
	// Time0/Time1 bracket the measurement window, in GetTickCount milliseconds.
	Time0 uint32
	Time1 uint32
	// Frames is how many frames were presented inside that window.
	Frames uint32
	// FrameTimeUs is the last frame time in microseconds.
	FrameTimeUs uint32
	// StatFramerateAvg is RTSS's own averaged framerate, when benchmarking.
	StatFramerateAvg uint32
}

// Name is the bare executable name, e.g. "Cyberpunk2077.exe".
func (e Entry) Name() string {
	p := e.Path
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// FPS is frames divided by the measurement window. RTSS resets the window
// roughly once a second, so this tracks the on-screen counter closely.
func (e Entry) FPS() float64 {
	if e.Time1 <= e.Time0 {
		return 0
	}
	return 1000 * float64(e.Frames) / float64(e.Time1-e.Time0)
}

// FrametimeMs prefers the frame time RTSS measured and falls back to the
// inverse of FPS, which is all that is available on older RTSS builds.
func (e Entry) FrametimeMs() float64 {
	if e.FrameTimeUs > 0 {
		return float64(e.FrameTimeUs) / 1000
	}
	if fps := e.FPS(); fps > 0 {
		return 1000 / fps
	}
	return 0
}

// Snapshot is one read of the shared memory block.
type Snapshot struct {
	// Version is the RTSS shared memory version, 0x0002xxxx.
	Version uint32
	Entries []Entry
}

// VersionString renders Version the way RTSS documents it, e.g. "2.7".
func (s Snapshot) VersionString() string {
	return fmt.Sprintf("%d.%d", s.Version>>16, s.Version&0xFFFF)
}

// MappingSize reports how many bytes of the mapping Parse needs, based on the
// header alone. It is used to size the copy taken out of shared memory.
func MappingSize(header []byte) (int, error) {
	if len(header) < headerSize {
		return 0, ErrTruncated
	}
	le := binary.LittleEndian
	if le.Uint32(header[offSignature:]) != signature {
		return 0, ErrBadSignature
	}

	entrySize := le.Uint32(header[offAppEntrySize:])
	arrOffset := le.Uint32(header[offAppArrOffset:])
	arrCount := le.Uint32(header[offAppArrSize:])

	if entrySize > maxEntrySize || arrCount > maxEntries {
		return 0, fmt.Errorf("%w: implausible app array (%d entries of %d bytes)",
			ErrBadSignature, arrCount, entrySize)
	}

	total := uint64(arrOffset) + uint64(arrCount)*uint64(entrySize)
	if total < headerSize {
		total = headerSize
	}
	if total > maxMapping {
		return 0, fmt.Errorf("%w: mapping would be %d bytes", ErrBadSignature, total)
	}
	return int(total), nil
}

// Parse decodes a copy of the shared memory block. Entries that do not fit in
// buf are skipped rather than treated as an error, because RTSS writes the
// block concurrently and the tail can be short-lived garbage.
func Parse(buf []byte) (Snapshot, error) {
	if len(buf) < headerSize {
		return Snapshot{}, ErrTruncated
	}
	le := binary.LittleEndian
	if le.Uint32(buf[offSignature:]) != signature {
		return Snapshot{}, ErrBadSignature
	}

	snap := Snapshot{Version: le.Uint32(buf[offVersion:])}

	entrySize := uint64(le.Uint32(buf[offAppEntrySize:]))
	arrOffset := uint64(le.Uint32(buf[offAppArrOffset:]))
	arrCount := uint64(le.Uint32(buf[offAppArrSize:]))
	if entrySize < entryMinSize || arrCount == 0 || arrCount > maxEntries {
		return snap, nil
	}

	for i := uint64(0); i < arrCount; i++ {
		start := arrOffset + i*entrySize
		end := start + entryMinSize
		if end > uint64(len(buf)) {
			break
		}
		entry := parseEntry(buf[start:end])
		if entry.ProcessID == 0 {
			continue // slot 0 is the global profile, and freed slots are zeroed
		}
		snap.Entries = append(snap.Entries, entry)
	}
	return snap, nil
}

func parseEntry(b []byte) Entry {
	le := binary.LittleEndian
	return Entry{
		ProcessID:        le.Uint32(b[offProcessID:]),
		Path:             cString(b[offName : offName+nameLen]),
		Flags:            le.Uint32(b[offFlags:]),
		Time0:            le.Uint32(b[offTime0:]),
		Time1:            le.Uint32(b[offTime1:]),
		Frames:           le.Uint32(b[offFrames:]),
		FrameTimeUs:      le.Uint32(b[offFrameTime:]),
		StatFramerateAvg: le.Uint32(b[offStatFramerateAvg:]),
	}
}

// cString reads a NUL-terminated string out of a fixed-width field.
//
// RTSS declares this field as char[MAX_PATH], so the bytes are in the system
// ANSI code page, not UTF-8. Reinterpreting them directly would put invalid
// UTF-8 into every export the moment a path contains anything outside ASCII —
// a game under C:\Users\Jürgen is enough — and Prometheus rejects a whole
// scrape over one bad byte. decodeANSI converts properly on Windows.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			b = b[:i]
			break
		}
	}
	return decodeANSI(b)
}

// SelectActive picks the entry whose FPS should be reported.
//
// The foreground process wins when RTSS knows about it, which is what the user
// is looking at. Otherwise the most recently rendered entry wins, so a game
// that keeps rendering while alt-tabbed still reports. Entries whose last
// frame is older than maxAgeMs are ignored entirely; that is what makes a
// closed game fall back to "no game running" instead of freezing at its last
// framerate.
func SelectActive(entries []Entry, foregroundPID uint32, nowTick uint32, maxAgeMs uint32) (Entry, bool) {
	var best Entry
	var found bool

	for _, e := range entries {
		if e.ProcessID == 0 || e.Time1 == 0 {
			continue
		}
		if maxAgeMs > 0 && ageMs(nowTick, e.Time1) > maxAgeMs {
			continue
		}
		if foregroundPID != 0 && e.ProcessID == foregroundPID {
			return e, true
		}
		if !found || after(e.Time1, best.Time1) {
			best = e
			found = true
		}
	}
	return best, found
}

// ageMs is a tick difference that survives the 32-bit GetTickCount wrap. A
// timestamp in the future (which RTSS can produce for a few milliseconds while
// it updates the block) counts as age zero rather than ~49 days.
func ageMs(now, then uint32) uint32 {
	d := now - then
	if d > 1<<31 {
		return 0
	}
	return d
}

// after reports whether tick a is later than b, wrap-around aware.
func after(a, b uint32) bool {
	return a-b < 1<<31 && a != b
}
