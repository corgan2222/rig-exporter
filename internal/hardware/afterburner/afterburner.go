// Package afterburner reads the MAHMSharedMemory block published by MSI
// Afterburner's hardware monitor.
//
// This is the same trick rig-exporter already uses for RTSS, and it is the
// only vendor-neutral way to get graphics temperature, clocks and fan speed on
// Windows: the operating system exposes none of them. Afterburner supports
// NVIDIA, AMD and Intel cards, and RTSS ships with it, so a machine set up for
// the FPS overlay usually already has this source.
//
// The parsing half is platform independent so it can be tested without
// Afterburner installed; reader_windows.go supplies the memory mapping.
package afterburner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// MappingName is the shared memory object Afterburner publishes.
const MappingName = "MAHMSharedMemory"

const (
	// 'MAHM' as a multi-character constant, most significant byte first.
	signature = 0x4D41484D

	// Header field offsets. Everything past dwEntrySize is located through
	// dwHeaderSize instead, because the header contains a time_t whose width
	// differs between Afterburner builds.
	offSignature  = 0
	offVersion    = 4
	offHeaderSize = 8
	offNumEntries = 12
	offEntrySize  = 16
	minHeaderSize = 20

	// Entry field offsets, from MAHM_SHARED_MEMORY_ENTRY.
	//
	// The full layout is listed, not only the fields that are read. These are
	// the description of somebody else's memory: entryMinSize and
	// gpuEntryMinSize are derived from the last offset in each block, so
	// dropping an unread one either breaks that arithmetic or turns the size
	// into a magic number nobody can check against the struct. offGPUID is the
	// clearest case — it points at a stable PCI id (VEN_10DE&DEV_1E87&SUBSYS_…)
	// that would make a better instance key than the card name, which is why it
	// is written down rather than removed.
	entryNameLen     = 260 // MAX_PATH
	offEntryName     = 0
	offEntryUnits    = entryNameLen
	offEntryData     = entryNameLen * 5
	offEntryMinLimit = offEntryData + 4
	offEntryMaxLimit = offEntryData + 8
	offEntryFlags    = offEntryData + 12
	offEntryGPU      = offEntryData + 16
	offEntrySrcID    = offEntryData + 20
	entryMinSize     = offEntrySrcID + 4

	// GPU entry field offsets, from MAHM_SHARED_MEMORY_GPU_ENTRY.
	offGPUID        = 0
	offGPUFamily    = entryNameLen
	offGPUDevice    = entryNameLen * 2
	offGPUDriver    = entryNameLen * 3
	offGPUBIOS      = entryNameLen * 4
	offGPUMemAmount = entryNameLen * 5
	gpuEntryMinSize = offGPUMemAmount + 4
)

// Sanity limits, so a corrupt header cannot make us allocate wildly.
const (
	maxEntries      = 4096
	maxEntrySize    = 1 << 16
	maxGPUEntries   = 16
	maxGPUEntrySize = 1 << 16
	maxMapping      = 64 << 20
)

// Errors callers branch on.
var (
	// ErrNotRunning means the shared memory object does not exist, which in
	// practice means Afterburner is not running.
	ErrNotRunning = errors.New("MSI Afterburner is not running")
	// ErrAccessDenied means Afterburner runs at a higher integrity level.
	ErrAccessDenied = errors.New("access to the Afterburner shared memory was denied")
	// ErrBadSignature means something else owns that shared memory name.
	//
	// The product is named in full here, as in ErrNotRunning. A lint that
	// rejects a capitalised error string cannot tell a proper noun from
	// sloppiness, and "MSI" reads as the acronym it is.
	ErrBadSignature = errors.New("MSI Afterburner shared memory has an unexpected signature")
	// ErrTruncated means the mapping is smaller than the header it advertises.
	ErrTruncated = errors.New("MSI Afterburner shared memory is truncated")
)

// naValue is what Afterburner writes for a sensor that is not available.
const naValue = math.MaxFloat32

// Entry is one sensor reading.
//
// Only what is read ends up here. The block carries more per sensor — a unit
// string, the owning card index, minimum and maximum limits — and decoding it
// cost a 260-byte scan per sensor per tick for values nothing ever asked for.
// The offsets below still describe those fields, so adding one back is a line,
// not an investigation.
type Entry struct {
	// Source is the sensor name, e.g. "GPU temperature" or "Core clock".
	Source string
	Value  float64
}

// Valid reports whether Afterburner actually measured this sensor.
func (e Entry) Valid() bool {
	return !math.IsNaN(e.Value) && !math.IsInf(e.Value, 0) && e.Value < naValue
}

// GPU describes one graphics card Afterburner monitors.
type GPU struct {
	// Device is the card name, e.g. "NVIDIA GeForce RTX 4090".
	Device string
	Family string
	// MemoryMB is the amount of graphics memory, when reported.
	MemoryMB uint64
}

// Snapshot is one read of the shared memory block.
type Snapshot struct {
	Version uint32
	Entries []Entry
	GPUs    []GPU
}

// VersionString renders Version the way Afterburner documents it, e.g. "2.0".
func (s Snapshot) VersionString() string {
	return fmt.Sprintf("%d.%d", s.Version>>16, s.Version&0xFFFF)
}

// Find returns the first valid sensor whose name matches one of the given
// candidates, case-insensitively.
//
// Matching is by name only. The dwGpu field a sensor carries is not reliable:
// Afterburner assigns it to machine-wide sensors too, so "RAM usage" and
// "CPU1 temperature" both claim to belong to the first graphics card.
func (s Snapshot) Find(candidates ...string) (Entry, bool) {
	for _, want := range candidates {
		for _, e := range s.Entries {
			if e.Valid() && strings.EqualFold(e.Source, want) {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// FindGPU returns a sensor belonging to one graphics card.
//
// Afterburner numbers its per-card sensors from one: the first card's
// temperature is "GPU1 temperature". Older builds with a single card drop the
// number, so that spelling is tried as well when there is only one card.
func (s Snapshot) FindGPU(index int, suffixes ...string) (Entry, bool) {
	var candidates []string
	for _, suffix := range suffixes {
		candidates = append(candidates, fmt.Sprintf("GPU%d %s", index+1, suffix))
	}
	if len(s.GPUs) <= 1 {
		for _, suffix := range suffixes {
			candidates = append(candidates, "GPU "+suffix, suffix)
		}
	}
	return s.Find(candidates...)
}

// CardCount is how many graphics cards the sensor list describes, which is at
// least one whenever any sensor was published at all.
func (s Snapshot) CardCount() int {
	if len(s.GPUs) > 0 {
		return len(s.GPUs)
	}
	if len(s.Entries) > 0 {
		return 1
	}
	return 0
}

// MappingSize reports how many bytes of the mapping Parse needs.
func MappingSize(header []byte) (int, error) {
	if len(header) < minHeaderSize {
		return 0, ErrTruncated
	}
	le := binary.LittleEndian
	if le.Uint32(header[offSignature:]) != signature {
		return 0, ErrBadSignature
	}

	headerSize := uint64(le.Uint32(header[offHeaderSize:]))
	numEntries := uint64(le.Uint32(header[offNumEntries:]))
	entrySize := uint64(le.Uint32(header[offEntrySize:]))

	if headerSize < minHeaderSize || numEntries > maxEntries || entrySize > maxEntrySize {
		return 0, fmt.Errorf("%w: header %d, %d entries of %d bytes",
			ErrBadSignature, headerSize, numEntries, entrySize)
	}

	// The GPU array follows the sensor array. Its descriptor sits behind a
	// time_t of unknown width, so reserve room for the largest plausible one
	// rather than trying to locate it from the header alone.
	total := headerSize + numEntries*entrySize + maxGPUEntries*gpuEntryMinSize
	if total > maxMapping {
		return 0, fmt.Errorf("%w: mapping would be %d bytes", ErrBadSignature, total)
	}
	return int(total), nil
}

// Parse decodes a copy of the shared memory block.
func Parse(buf []byte) (Snapshot, error) {
	if len(buf) < minHeaderSize {
		return Snapshot{}, ErrTruncated
	}
	le := binary.LittleEndian
	if le.Uint32(buf[offSignature:]) != signature {
		return Snapshot{}, ErrBadSignature
	}

	snap := Snapshot{Version: le.Uint32(buf[offVersion:])}

	headerSize := uint64(le.Uint32(buf[offHeaderSize:]))
	numEntries := uint64(le.Uint32(buf[offNumEntries:]))
	entrySize := uint64(le.Uint32(buf[offEntrySize:]))
	if headerSize < minHeaderSize || entrySize < entryMinSize || numEntries > maxEntries {
		return snap, nil
	}

	numGPUs, gpuEntrySize, gpuOK := gpuDescriptor(buf, headerSize)
	entriesEnd := headerSize + numEntries*entrySize

	for i := uint64(0); i < numEntries; i++ {
		start := headerSize + i*entrySize
		if start+entryMinSize > uint64(len(buf)) {
			break
		}
		snap.Entries = append(snap.Entries, parseEntry(buf[start:start+entryMinSize]))
	}

	if gpuOK {
		for i := uint64(0); i < numGPUs; i++ {
			start := entriesEnd + i*gpuEntrySize
			if start+gpuEntryMinSize > uint64(len(buf)) {
				break
			}
			snap.GPUs = append(snap.GPUs, parseGPU(buf[start:start+gpuEntryMinSize]))
		}
	}
	return snap, nil
}

// gpuDescriptor locates dwNumGpuEntries and dwGpuEntrySize.
//
// They sit behind a time_t whose width depends on how Afterburner was built,
// so both candidate offsets are tried and the one that yields plausible values
// wins. Getting this wrong only costs the card names, not the sensors.
func gpuDescriptor(buf []byte, headerSize uint64) (count, size uint64, ok bool) {
	le := binary.LittleEndian

	for _, timeWidth := range []uint64{4, 8} {
		countOffset := uint64(offEntrySize) + 4 + timeWidth
		sizeOffset := countOffset + 4
		if sizeOffset+4 > headerSize || sizeOffset+4 > uint64(len(buf)) {
			continue
		}

		count = uint64(le.Uint32(buf[countOffset:]))
		size = uint64(le.Uint32(buf[sizeOffset:]))
		if count >= 1 && count <= maxGPUEntries && size >= gpuEntryMinSize && size <= maxGPUEntrySize {
			return count, size, true
		}
	}
	return 0, 0, false
}

func parseEntry(b []byte) Entry {
	return Entry{
		Source: cString(b[offEntryName : offEntryName+entryNameLen]),
		Value:  float64(math.Float32frombits(binary.LittleEndian.Uint32(b[offEntryData:]))),
	}
}

func parseGPU(b []byte) GPU {
	return GPU{
		Device:   cString(b[offGPUDevice : offGPUDevice+entryNameLen]),
		Family:   cString(b[offGPUFamily : offGPUFamily+entryNameLen]),
		MemoryMB: uint64(binary.LittleEndian.Uint32(b[offGPUMemAmount:])) / 1024,
	}
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return strings.TrimSpace(string(b[:i]))
		}
	}
	return strings.TrimSpace(string(b))
}
