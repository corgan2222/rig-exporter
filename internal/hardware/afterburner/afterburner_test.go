package afterburner

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

// buildEntry lays out one MAHM_SHARED_MEMORY_ENTRY of entrySize bytes.
func buildEntry(entrySize int, source, units string, value float32, gpu int32) []byte {
	b := make([]byte, entrySize)
	copy(b[offEntryName:offEntryName+entryNameLen-1], source)
	copy(b[offEntryUnits:offEntryUnits+entryNameLen-1], units)
	binary.LittleEndian.PutUint32(b[offEntryData:], math.Float32bits(value))
	binary.LittleEndian.PutUint32(b[offEntryGPU:], uint32(gpu))
	return b
}

// buildGPU lays out one MAHM_SHARED_MEMORY_GPU_ENTRY.
func buildGPU(entrySize int, device, family string, memKB uint32) []byte {
	b := make([]byte, entrySize)
	copy(b[offGPUFamily:offGPUFamily+entryNameLen-1], family)
	copy(b[offGPUDevice:offGPUDevice+entryNameLen-1], device)
	binary.LittleEndian.PutUint32(b[offGPUMemAmount:], memKB)
	return b
}

type sensor struct {
	source string
	units  string
	value  float32
	gpu    int32
}

// buildSharedMemory assembles a block shaped like the one Afterburner
// publishes. timeWidth selects which of the two header layouts to produce.
func buildSharedMemory(timeWidth int, sensors []sensor, gpus [][]byte, gpuEntrySize int) []byte {
	const entrySize = 1400
	headerSize := offEntrySize + 4 + timeWidth + 8

	total := headerSize + len(sensors)*entrySize + len(gpus)*gpuEntrySize
	buf := make([]byte, total)

	le := binary.LittleEndian
	le.PutUint32(buf[offSignature:], signature)
	le.PutUint32(buf[offVersion:], 0x00020000)
	le.PutUint32(buf[offHeaderSize:], uint32(headerSize))
	le.PutUint32(buf[offNumEntries:], uint32(len(sensors)))
	le.PutUint32(buf[offEntrySize:], entrySize)

	countOffset := offEntrySize + 4 + timeWidth
	le.PutUint32(buf[countOffset:], uint32(len(gpus)))
	le.PutUint32(buf[countOffset+4:], uint32(gpuEntrySize))

	for i, s := range sensors {
		copy(buf[headerSize+i*entrySize:], buildEntry(entrySize, s.source, s.units, s.value, s.gpu))
	}
	entriesEnd := headerSize + len(sensors)*entrySize
	for i, gpu := range gpus {
		copy(buf[entriesEnd+i*gpuEntrySize:], gpu)
	}
	return buf
}

const testGPUEntrySize = 1304

func twoCardBlock(timeWidth int) []byte {
	return buildSharedMemory(timeWidth,
		[]sensor{
			{"GPU1 temperature", "\xb0C", 33, 0},
			{"GPU2 temperature", "\xb0C", 54, 1},
			{"GPU1 usage", "%", 2, 0},
			{"GPU2 usage", "%", 43, 1},
			{"GPU1 core clock", "MHz", 300, 0},
			{"GPU2 core clock", "MHz", 2730, 1},
			// Machine-wide sensors carry a misleading card index.
			{"CPU usage", "%", 43.31, -1},
			{"RAM usage", "MB", 52917, 0},
		},
		[][]byte{
			buildGPU(testGPUEntrySize, "NVIDIA GeForce RTX 2080", "TU104-A", 8*1024*1024),
			buildGPU(testGPUEntrySize, "NVIDIA GeForce RTX 5070 Ti", "GB203-A", 16*1024*1024),
		},
		testGPUEntrySize)
}

func TestParseReadsSensorsAndCards(t *testing.T) {
	snap, err := Parse(twoCardBlock(8))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if snap.VersionString() != "2.0" {
		t.Errorf("VersionString = %q, want 2.0", snap.VersionString())
	}
	if len(snap.Entries) != 8 {
		t.Fatalf("got %d entries, want 8", len(snap.Entries))
	}
	if len(snap.GPUs) != 2 {
		t.Fatalf("got %d cards, want 2", len(snap.GPUs))
	}
	if snap.GPUs[0].Device != "NVIDIA GeForce RTX 2080" || snap.GPUs[1].Family != "GB203-A" {
		t.Errorf("cards = %+v", snap.GPUs)
	}
	if snap.GPUs[0].MemoryMB != 8192 {
		t.Errorf("MemoryMB = %d, want 8192", snap.GPUs[0].MemoryMB)
	}
	if snap.CardCount() != 2 {
		t.Errorf("CardCount = %d, want 2", snap.CardCount())
	}
}

// The GPU array descriptor sits behind a time_t whose width differs between
// Afterburner builds, so both layouts have to decode.
func TestParseHandlesBothHeaderLayouts(t *testing.T) {
	for _, timeWidth := range []int{4, 8} {
		snap, err := Parse(twoCardBlock(timeWidth))
		if err != nil {
			t.Fatalf("time_t width %d: %v", timeWidth, err)
		}
		if len(snap.GPUs) != 2 {
			t.Errorf("time_t width %d: got %d cards, want 2", timeWidth, len(snap.GPUs))
		}
	}
}

// Sensors are numbered per card, and the card index the entry carries is not
// trustworthy, so lookups must go by name.
func TestFindGPUMatchesTheNumberedName(t *testing.T) {
	snap, err := Parse(twoCardBlock(8))
	if err != nil {
		t.Fatal(err)
	}

	first, ok := snap.FindGPU(0, "temperature")
	if !ok || first.Value != 33 {
		t.Errorf("card 0 temperature = %v (found %v), want 33", first.Value, ok)
	}
	second, ok := snap.FindGPU(1, "temperature")
	if !ok || second.Value != 54 {
		t.Errorf("card 1 temperature = %v (found %v), want 54", second.Value, ok)
	}
	if _, ok := snap.FindGPU(2, "temperature"); ok {
		t.Error("a third card was found where there are only two")
	}
}

// With one card, older builds drop the number from the sensor name.
func TestFindGPUAcceptsUnnumberedNamesForASingleCard(t *testing.T) {
	buf := buildSharedMemory(8,
		[]sensor{{"GPU temperature", "\xb0C", 61, 0}},
		[][]byte{buildGPU(testGPUEntrySize, "Radeon RX 7900", "Navi31", 0)},
		testGPUEntrySize)

	snap, err := Parse(buf)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := snap.FindGPU(0, "temperature")
	if !ok || entry.Value != 61 {
		t.Errorf("temperature = %v (found %v), want 61", entry.Value, ok)
	}
}

func TestFindIgnoresTheCardIndexOnMachineWideSensors(t *testing.T) {
	snap, err := Parse(twoCardBlock(8))
	if err != nil {
		t.Fatal(err)
	}

	// "RAM usage" claims to belong to card 0; it must still be findable.
	entry, ok := snap.Find("RAM usage")
	if !ok || entry.Value != 52917 {
		t.Errorf("RAM usage = %v (found %v), want 52917", entry.Value, ok)
	}
	if _, ok := snap.Find("nonexistent sensor"); ok {
		t.Error("a sensor that is not there was found")
	}
}

// Afterburner writes FLT_MAX for a sensor it could not read.
func TestUnavailableSensorsAreNotValid(t *testing.T) {
	buf := buildSharedMemory(8,
		[]sensor{{"GPU1 power", "W", math.MaxFloat32, 0}},
		[][]byte{buildGPU(testGPUEntrySize, "Card", "Fam", 0)},
		testGPUEntrySize)

	snap, err := Parse(buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.FindGPU(0, "power"); ok {
		t.Error("an unavailable sensor was reported as a reading")
	}
}

func TestParseRejectsForeignMemory(t *testing.T) {
	buf := make([]byte, 64)
	binary.LittleEndian.PutUint32(buf, 0xDEADBEEF)

	if _, err := Parse(buf); !errors.Is(err, ErrBadSignature) {
		t.Errorf("Parse error = %v, want ErrBadSignature", err)
	}
}

func TestParseRejectsShortBuffer(t *testing.T) {
	if _, err := Parse(make([]byte, 8)); !errors.Is(err, ErrTruncated) {
		t.Errorf("Parse error = %v, want ErrTruncated", err)
	}
}

func TestMappingSizeCoversHeaderEntriesAndCards(t *testing.T) {
	buf := twoCardBlock(8)

	size, err := MappingSize(buf[:minHeaderSize])
	if err != nil {
		t.Fatalf("MappingSize: %v", err)
	}
	if size < len(buf) {
		t.Errorf("MappingSize = %d, which is short of the %d byte block", size, len(buf))
	}
}

func TestMappingSizeRejectsImplausibleHeader(t *testing.T) {
	buf := twoCardBlock(8)
	binary.LittleEndian.PutUint32(buf[offNumEntries:], 1<<20)

	if _, err := MappingSize(buf[:minHeaderSize]); err == nil {
		t.Error("MappingSize accepted an entry count of 2^20")
	}
}

// Parse must not walk past the end of the block when the header claims more
// entries than are present.
func TestParseStopsAtEndOfBuffer(t *testing.T) {
	buf := twoCardBlock(8)
	binary.LittleEndian.PutUint32(buf[offNumEntries:], 4096)

	snap, err := Parse(buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(snap.Entries) > 4096 {
		t.Errorf("got %d entries", len(snap.Entries))
	}
}
