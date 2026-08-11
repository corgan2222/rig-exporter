package ram

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// builder assembles an SMBIOS structure table the way firmware lays one out:
// a fixed area followed by a string table terminated by two zero bytes.
type builder struct {
	out []byte
}

func (b *builder) add(kind byte, formatted []byte, strings ...string) {
	record := make([]byte, len(formatted))
	copy(record, formatted)
	record[0] = kind
	record[1] = byte(len(record))
	binary.LittleEndian.PutUint16(record[2:], uint16(len(b.out)))
	b.out = append(b.out, record...)

	if len(strings) == 0 {
		b.out = append(b.out, 0, 0)
		return
	}
	for _, s := range strings {
		b.out = append(b.out, []byte(s)...)
		b.out = append(b.out, 0)
	}
	b.out = append(b.out, 0)
}

func (b *builder) end() []byte {
	b.out = append(b.out, typeEndOfTable, 4, 0, 0, 0, 0)
	return b.out
}

// memoryArray builds a type 16 structure declaring how many slots exist.
func memoryArray(slots int) []byte {
	record := make([]byte, arrayNumberOfDevices+2)
	binary.LittleEndian.PutUint16(record[arrayNumberOfDevices:], uint16(slots))
	return record
}

// memoryDevice builds a type 17 structure.
//
// The size field is fifteen bits wide, so anything from 32 GB up has to be
// flagged and carried in the extended field — which is exactly what real
// firmware does.
func memoryDevice(sizeMB uint32, memType byte, rated, configured uint16) []byte {
	record := make([]byte, deviceConfiguredSpeed+2)
	le := binary.LittleEndian

	if sizeMB >= 0x7FFF {
		le.PutUint16(record[deviceSize:], 0x7FFF)
		le.PutUint32(record[deviceExtendedSize:], sizeMB)
	} else {
		le.PutUint16(record[deviceSize:], uint16(sizeMB))
	}
	record[deviceLocator] = 1     // first string
	record[deviceBankLocator] = 2 // second string
	record[deviceMemoryType] = memType
	le.PutUint16(record[deviceSpeed:], rated)
	record[deviceManufacturer] = 3
	record[devicePartNumber] = 4
	le.PutUint16(record[deviceConfiguredSpeed:], configured)
	return record
}

// kilobyteDevice builds a module whose size is expressed in kilobytes, which
// the top bit of the size field signals.
func kilobyteDevice(sizeKB uint16) []byte {
	record := memoryDevice(0, 0x1A, 2133, 2133)
	binary.LittleEndian.PutUint16(record[deviceSize:], sizeKB|0x8000)
	return record
}

func twoModules() []byte {
	var b builder
	b.add(typePhysicalMemoryArray, memoryArray(4))
	b.add(typeMemoryDevice, memoryDevice(32768, 0x1A, 3600, 3334),
		"DIMM 0", "P0 CHANNEL A", "Corsair", "CMK64GX4M2D3600C18")
	b.add(typeMemoryDevice, memoryDevice(32768, 0x1A, 3200, 3200),
		"DIMM 0", "P0 CHANNEL B", "Unknown", "Not Specified")
	// An empty slot: firmware still describes it, with a size of zero.
	b.add(typeMemoryDevice, memoryDevice(0, 0x1A, 0, 0),
		"DIMM 1", "P0 CHANNEL A", "", "")
	return b.end()
}

func TestParseReadsModules(t *testing.T) {
	info, err := Parse(twoModules())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(info.Modules) != 2 {
		t.Fatalf("got %d modules, want 2 (the empty slot must be skipped)", len(info.Modules))
	}
	if info.Slots != 4 {
		t.Errorf("Slots = %d, want 4", info.Slots)
	}
	if info.TotalMB != 65536 {
		t.Errorf("TotalMB = %d, want 65536", info.TotalMB)
	}
	if info.Type != "DDR4" {
		t.Errorf("Type = %q, want DDR4", info.Type)
	}
}

// The controller runs every module at the slowest one's speed, and the rated
// speed is what the fastest module could do.
func TestMixedSpeedsReportTheSlowestConfigured(t *testing.T) {
	info, err := Parse(twoModules())
	if err != nil {
		t.Fatal(err)
	}

	if info.ConfiguredSpeed != 3200 {
		t.Errorf("ConfiguredSpeed = %d, want 3200", info.ConfiguredSpeed)
	}
	if info.RatedSpeed != 3600 {
		t.Errorf("RatedSpeed = %d, want 3600", info.RatedSpeed)
	}
}

// Boards repeat the device locator per channel, so it cannot identify a slot
// on its own.
func TestSlotCombinesBankAndLocator(t *testing.T) {
	info, err := Parse(twoModules())
	if err != nil {
		t.Fatal(err)
	}

	first, second := info.Modules[0].Slot(), info.Modules[1].Slot()
	if first == second {
		t.Errorf("both modules report the slot %q", first)
	}
	if first != "P0 CHANNEL A DIMM 0" {
		t.Errorf("Slot = %q", first)
	}
}

// Firmware writes placeholders where it has nothing to say; showing them is
// worse than showing nothing.
func TestPlaceholderStringsAreDropped(t *testing.T) {
	info, err := Parse(twoModules())
	if err != nil {
		t.Fatal(err)
	}

	if info.Modules[0].Manufacturer != "Corsair" {
		t.Errorf("Manufacturer = %q, want Corsair", info.Modules[0].Manufacturer)
	}
	if info.Modules[1].Manufacturer != "" {
		t.Errorf("Manufacturer = %q, want the Unknown placeholder dropped", info.Modules[1].Manufacturer)
	}
	if info.Modules[1].PartNumber != "" {
		t.Errorf("PartNumber = %q, want the Not Specified placeholder dropped", info.Modules[1].PartNumber)
	}
}

// A module larger than the 15 bit field holds is flagged and carried in the
// extended size instead.
func TestExtendedSizeIsUsedForLargeModules(t *testing.T) {
	var b builder
	b.add(typeMemoryDevice, memoryDevice(65536, 0x22, 5600, 5600), "DIMM 0", "CHANNEL A", "", "")

	info, err := Parse(b.end())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Modules[0].SizeMB != 65536 {
		t.Errorf("SizeMB = %d, want 65536", info.Modules[0].SizeMB)
	}
	if info.Type != "DDR5" {
		t.Errorf("Type = %q, want DDR5", info.Type)
	}
}

// The top bit of the size field switches the unit to kilobytes, which small
// modules on embedded boards use.
func TestKilobyteSizedModules(t *testing.T) {
	var b builder
	b.add(typeMemoryDevice, kilobyteDevice(8192), "DIMM 0", "BANK 0", "", "")

	info, err := Parse(b.end())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Modules[0].SizeMB != 8 {
		t.Errorf("SizeMB = %d, want 8 (8192 KB)", info.Modules[0].SizeMB)
	}
}

// Older firmware stops before the configured speed field; the rated speed is
// then the best answer available.
func TestConfiguredSpeedFallsBackToTheRatedSpeed(t *testing.T) {
	var b builder
	short := memoryDevice(16384, 0x18, 1600, 0)[:deviceMinimumLength]
	b.add(typeMemoryDevice, short, "DIMM 0", "BANK 0", "", "")

	info, err := Parse(b.end())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.ConfiguredSpeed != 1600 {
		t.Errorf("ConfiguredSpeed = %d, want the rated 1600", info.ConfiguredSpeed)
	}
	if info.Type != "DDR3" {
		t.Errorf("Type = %q, want DDR3", info.Type)
	}
}

func TestUnknownMemoryTypeIsReportedByNumber(t *testing.T) {
	var b builder
	b.add(typeMemoryDevice, memoryDevice(8192, 0x7E, 0, 0), "DIMM 0", "BANK 0", "", "")

	info, err := Parse(b.end())
	if err != nil {
		t.Fatal(err)
	}
	// English, like every published value: this is a sensor state a Home
	// Assistant automation may compare against, not interface text, and it must
	// not change when the user switches language.
	if info.Modules[0].Type != "Type 126" {
		t.Errorf("Type = %q, want the raw number rather than a guess", info.Modules[0].Type)
	}
}

func TestEmptyTableIsReported(t *testing.T) {
	var b builder
	b.add(typePhysicalMemoryArray, memoryArray(4))

	if _, err := Parse(b.end()); !errors.Is(err, ErrNoMemoryInfo) {
		t.Errorf("Parse error = %v, want ErrNoMemoryInfo", err)
	}
}

// A truncated or corrupt table must not send the walker into a loop.
func TestMalformedTableTerminates(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		Parse([]byte{17, 2, 0, 0, 0, 0, 0, 0}) // length shorter than a header
		Parse([]byte{17, 200, 0, 0})           // length past the end
		Parse(twoModules()[:20])               // cut mid-structure
		Parse(nil)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Parse did not terminate on a malformed table")
	}
}

// SMBIOS spells "the size is unknown" 0xFFFF, and that is not a size.
//
// The value is neither 0 nor 0x7FFF, so it reaches the kilobyte branch — the
// top bit is set, as it is in every value with 0xFF in the high byte — and
// (0xFFFF & 0x7FFF) / 1024 comes out as 31. A module the firmware refused to
// measure would be published as a 31 MB one and summed into the total, which
// is worse than leaving it out: a missing field says nothing, a plausible
// number says something wrong.
//
// Firmware that reports this is not exotic. It turns up on virtual machines,
// and this program says out loud which hypervisor it is running under.
func TestAModuleOfUnknownSizeIsLeftOutRatherThanGuessed(t *testing.T) {
	var b builder
	b.add(typePhysicalMemoryArray, memoryArray(2))
	b.add(typeMemoryDevice, memoryDevice(16384, 0x1A, 3200, 3200),
		"DIMM 0", "P0 CHANNEL A", "Corsair", "CMK32GX4M2D3200")

	unknown := memoryDevice(0, 0x1A, 3200, 3200)
	binary.LittleEndian.PutUint16(unknown[deviceSize:], 0xFFFF)
	b.add(typeMemoryDevice, unknown,
		"DIMM 1", "P0 CHANNEL B", "Corsair", "CMK32GX4M2D3200")

	info, err := Parse(b.end())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(info.Modules) != 1 {
		t.Fatalf("modules = %d, want 1 — a size the firmware called unknown was invented: %+v",
			len(info.Modules), info.Modules)
	}
	if got := info.Modules[0].SizeMB; got != 16384 {
		t.Errorf("SizeMB = %d, want 16384", got)
	}
	if info.TotalMB != 16384 {
		t.Errorf("TotalMB = %d, want 16384; an invented module was summed in", info.TotalMB)
	}
}
