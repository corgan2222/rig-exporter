// Package ram reports what the memory modules are, beyond how full they are.
//
// Windows exposes the load through GlobalMemoryStatusEx but says nothing about
// the hardware: speed, type and how many slots are filled all live in the
// SMBIOS tables the firmware publishes. Those are parsed here.
//
// The parser is platform independent so it can be tested against a captured
// table; ram_windows.go supplies the real one.
package ram

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// SMBIOS structure types this cares about.
const (
	typePhysicalMemoryArray = 16
	typeMemoryDevice        = 17
	typeEndOfTable          = 127
)

// Field offsets inside a Memory Device (type 17) structure.
const (
	deviceSize            = 0x0C
	deviceLocator         = 0x10
	deviceBankLocator     = 0x11
	deviceMemoryType      = 0x12
	deviceSpeed           = 0x15
	deviceManufacturer    = 0x17
	devicePartNumber      = 0x1A
	deviceExtendedSize    = 0x1C
	deviceConfiguredSpeed = 0x20

	// deviceMinimumLength covers only the fields every version of the
	// structure has. Firmware truncates the record after whatever its SMBIOS
	// version defines, so everything past this is read only after checking.
	deviceMinimumLength = deviceSpeed + 2
)

// Field offsets inside a Physical Memory Array (type 16) structure.
const (
	arrayNumberOfDevices = 0x0D
	arrayMinimumLength   = arrayNumberOfDevices + 2
)

// ErrNoMemoryInfo means the firmware published no usable memory structures.
var ErrNoMemoryInfo = errors.New("the firmware reports no memory modules")

// Module is one populated memory slot.
type Module struct {
	// Locator is the slot label the board silkscreen uses, e.g. "DIMM 0".
	// On a multi-channel board it repeats per channel, so it does not
	// identify a slot on its own — see Slot.
	Locator string
	// Bank is the channel the slot sits on, e.g. "P0 CHANNEL A".
	Bank string
	// SizeMB is the module capacity.
	SizeMB uint64
	// Type is the technology, e.g. "DDR4".
	Type string
	// ConfiguredSpeed is what the module actually runs at, in MT/s.
	ConfiguredSpeed uint64
	// RatedSpeed is what the module is capable of, in MT/s.
	RatedSpeed   uint64
	Manufacturer string
	PartNumber   string
}

// Info is the memory picture the firmware describes.
type Info struct {
	Modules []Module
	// Slots is how many slots the board has, populated or not.
	Slots int
	// TotalMB is the sum of the module capacities.
	TotalMB uint64
	// ConfiguredSpeed is the speed the modules run at. Mixed configurations
	// report the slowest, which is what the memory controller settles on.
	ConfiguredSpeed uint64
	// RatedSpeed is the highest speed the installed modules are rated for.
	RatedSpeed uint64
	// Type is the technology of the installed modules.
	Type string
}

// memoryTypes maps the SMBIOS memory type byte onto its name. Only the ones a
// machine running this is plausibly using are listed; anything else is
// reported by its number rather than guessed at.
var memoryTypes = map[byte]string{
	0x0F: "SDRAM",
	0x12: "DDR",
	0x13: "DDR2",
	0x18: "DDR3",
	0x1A: "DDR4",
	0x1B: "LPDDR",
	0x1C: "LPDDR2",
	0x1D: "LPDDR3",
	0x1E: "LPDDR4",
	0x22: "DDR5",
	0x23: "LPDDR5",
}

// Parse walks the SMBIOS structure table and pulls out the memory facts.
func Parse(table []byte) (Info, error) {
	var info Info

	for _, s := range structures(table) {
		switch s.kind {
		case typePhysicalMemoryArray:
			if len(s.formatted) >= arrayMinimumLength {
				info.Slots += int(binary.LittleEndian.Uint16(s.formatted[arrayNumberOfDevices:]))
			}
		case typeMemoryDevice:
			if module, ok := parseModule(s); ok {
				info.Modules = append(info.Modules, module)
			}
		}
	}

	if len(info.Modules) == 0 {
		return info, ErrNoMemoryInfo
	}

	for _, m := range info.Modules {
		info.TotalMB += m.SizeMB
		if m.RatedSpeed > info.RatedSpeed {
			info.RatedSpeed = m.RatedSpeed
		}
		// The controller runs every module at the slowest one's speed.
		if m.ConfiguredSpeed > 0 && (info.ConfiguredSpeed == 0 || m.ConfiguredSpeed < info.ConfiguredSpeed) {
			info.ConfiguredSpeed = m.ConfiguredSpeed
		}
		if info.Type == "" {
			info.Type = m.Type
		}
	}
	if info.Slots < len(info.Modules) {
		info.Slots = len(info.Modules)
	}
	return info, nil
}

func parseModule(s structure) (Module, bool) {
	if len(s.formatted) < deviceMinimumLength {
		return Module{}, false
	}
	le := binary.LittleEndian

	// A size of zero means the slot is empty.
	size := uint64(le.Uint16(s.formatted[deviceSize:]))
	switch {
	case size == 0:
		return Module{}, false
	case size == 0x7FFF:
		// Too large for the 15 bit field; the real value is in the extended one.
		if len(s.formatted) < deviceExtendedSize+4 {
			return Module{}, false
		}
		size = uint64(le.Uint32(s.formatted[deviceExtendedSize:]))
	case size&0x8000 != 0:
		// The top bit means the value is in kilobytes, not megabytes.
		size = (size & 0x7FFF) / 1024
	}

	module := Module{
		Locator:    s.text(s.formatted[deviceLocator]),
		Bank:       s.text(s.formatted[deviceBankLocator]),
		SizeMB:     size,
		Type:       memoryTypeName(s.formatted[deviceMemoryType]),
		RatedSpeed: uint64(le.Uint16(s.formatted[deviceSpeed:])),
	}
	// Everything from here on belongs to a later SMBIOS version than the one
	// the minimum length guarantees, so each field is checked for separately.
	if len(s.formatted) > deviceManufacturer {
		module.Manufacturer = meaningful(s.text(s.formatted[deviceManufacturer]))
	}
	if len(s.formatted) > devicePartNumber {
		module.PartNumber = meaningful(s.text(s.formatted[devicePartNumber]))
	}
	// Configured speed arrived with SMBIOS 2.7; older tables stop short of it.
	if len(s.formatted) >= deviceConfiguredSpeed+2 {
		module.ConfiguredSpeed = uint64(le.Uint16(s.formatted[deviceConfiguredSpeed:]))
	}
	if module.ConfiguredSpeed == 0 {
		module.ConfiguredSpeed = module.RatedSpeed
	}
	return module, true
}

// Slot identifies one physical slot. The device locator repeats per channel on
// most boards, so the bank has to come along to tell two of them apart.
func (m Module) Slot() string {
	switch {
	case m.Bank != "" && m.Locator != "":
		return m.Bank + " " + m.Locator
	case m.Locator != "":
		return m.Locator
	default:
		return m.Bank
	}
}

// meaningful drops the placeholders firmware writes when it has nothing to
// say. Showing "Unknown" as a manufacturer is worse than showing nothing.
func meaningful(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unknown", "not specified", "none", "to be filled by o.e.m.", "undefined":
		return ""
	default:
		return strings.TrimSpace(value)
	}
}

// memoryTypeName names the memory technology, e.g. "DDR4".
//
// The fallback stays English: this is a published sensor value, not interface
// text, and it must not change when the user switches language.
func memoryTypeName(value byte) string {
	if name, ok := memoryTypes[value]; ok {
		return name
	}
	return fmt.Sprintf("Type %d", value)
}

// structure is one SMBIOS record: the fixed part plus its string table.
type structure struct {
	kind      byte
	formatted []byte
	strings   []string
}

// text resolves a string reference. Index 0 means "no string".
func (s structure) text(index byte) string {
	if index == 0 || int(index) > len(s.strings) {
		return ""
	}
	return s.strings[index-1]
}

// structures splits the table into records.
//
// Each record is a fixed-length formatted area followed by a string table
// terminated by two zero bytes, so the only way to find the next record is to
// walk this one to its end.
func structures(table []byte) []structure {
	var out []structure

	for offset := 0; offset+4 <= len(table); {
		kind := table[offset]
		length := int(table[offset+1])
		if length < 4 || offset+length > len(table) {
			break
		}
		if kind == typeEndOfTable {
			break
		}

		formatted := table[offset : offset+length]
		strings, next := readStrings(table, offset+length)
		out = append(out, structure{kind: kind, formatted: formatted, strings: strings})

		if next <= offset {
			break // malformed: refuse to loop forever
		}
		offset = next
	}
	return out
}

// readStrings consumes the string table starting at offset and returns the
// strings plus the offset of the next structure.
func readStrings(table []byte, offset int) ([]string, int) {
	// An empty string table is a single pair of zero bytes.
	if offset+1 < len(table) && table[offset] == 0 && table[offset+1] == 0 {
		return nil, offset + 2
	}

	var out []string
	start := offset
	for i := offset; i < len(table); i++ {
		if table[i] != 0 {
			continue
		}
		out = append(out, string(table[start:i]))
		start = i + 1

		// Two zero bytes in a row end the table.
		if start < len(table) && table[start] == 0 {
			return out, start + 1
		}
	}
	return out, len(table)
}
