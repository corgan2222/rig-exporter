package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// This writes the Windows resource object that gives the executable its icon
// in Explorer, the taskbar and the Alt-Tab switcher.
//
// It is written by hand rather than with rsrc or goversioninfo so that a plain
// `go build` produces a properly iconed binary with no tool to install first.
// The Go linker picks up any .syso in the main package directory; the file
// name suffix restricts it to the architecture it was built for.

// Resource types, from winuser.h.
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// languageNeutral is the LANGID stored on every resource. Nothing here is
// language dependent, and Explorer resolves neutral resources for any locale.
const languageNeutral = 0

// COFF constants.
const (
	machineAMD64 = 0x8664

	// Initialised data that is readable at run time.
	sectionCharacteristics = 0x40000040

	// A 32 bit address relative to the image base, which is exactly what a
	// resource directory stores.
	relocAddr32NB = 0x0003

	symbolClassStatic = 3

	coffHeaderSize     = 20
	sectionHeaderSize  = 40
	relocationSize     = 10
	symbolSize         = 18
	resourceDirSize    = 16
	resourceEntrySize  = 8
	resourceDataSize   = 16
	groupIconEntrySize = 14
)

// encodeSyso builds the resource object for the given icon frames.
func encodeSyso(frames []*image.RGBA) ([]byte, error) {
	if len(frames) == 0 || len(frames) > 0xFFFF {
		return nil, fmt.Errorf("encodeSyso: unsupported frame count %d", len(frames))
	}

	// One RT_ICON per frame, numbered from one, plus one RT_GROUP_ICON that
	// ties them together. Explorer picks the group with the lowest id as the
	// application icon, so the group is id 1.
	blobs := make([][]byte, 0, len(frames)+1)
	for _, frame := range frames {
		blobs = append(blobs, bmpPayload(frame))
	}
	blobs = append(blobs, groupIcon(frames, blobs))

	section, relocations := buildResourceSection(frames, blobs)
	return buildObject(section, relocations), nil
}

// groupIcon builds the GRPICONDIR that names the individual icon resources.
func groupIcon(frames []*image.RGBA, blobs [][]byte) []byte {
	var b bytes.Buffer
	write(&b, uint16(0), uint16(1), uint16(len(frames))) // reserved, type, count

	for i, frame := range frames {
		w, h := frame.Bounds().Dx(), frame.Bounds().Dy()
		b.WriteByte(dimByte(w))
		b.WriteByte(dimByte(h))
		b.WriteByte(0) // palette entries
		b.WriteByte(0) // reserved
		// The entry ends with the resource id rather than a file offset, which
		// is the only structural difference from an ICONDIRENTRY.
		write(&b, uint16(1), uint16(32), uint32(len(blobs[i])), uint16(i+1))
	}
	return b.Bytes()
}

// relocation records that one field in the section holds an image-relative
// address the linker has to fix up.
type relocation struct {
	offset uint32
}

// buildResourceSection lays out the resource directory tree followed by the
// data, and reports where the addresses that need relocating ended up.
//
// The tree has three levels — type, name, language — and every leaf points at
// an IMAGE_RESOURCE_DATA_ENTRY whose first field is the address to fix up.
func buildResourceSection(frames []*image.RGBA, blobs [][]byte) ([]byte, []relocation) {
	iconCount := len(frames)
	leafCount := iconCount + 1 // every icon, plus the group

	// Walk the layout once to work out where each part starts.
	rootSize := resourceDirSize + 2*resourceEntrySize
	iconDirSize := resourceDirSize + iconCount*resourceEntrySize
	groupDirSize := resourceDirSize + resourceEntrySize
	langDirSize := resourceDirSize + resourceEntrySize

	iconDirAt := rootSize
	groupDirAt := iconDirAt + iconDirSize
	langDirsAt := groupDirAt + groupDirSize
	dataEntriesAt := langDirsAt + leafCount*langDirSize

	// Resource data is conventionally eight byte aligned.
	blobsAt := align(dataEntriesAt+leafCount*resourceDataSize, 8)
	blobOffsets := make([]int, len(blobs))
	offset := blobsAt
	for i, blob := range blobs {
		blobOffsets[i] = offset
		offset = align(offset+len(blob), 8)
	}
	sectionSize := offset

	section := make([]byte, sectionSize)

	// Level one: the two resource types.
	putDirectory(section, 0, 2)
	putEntry(section, resourceDirSize, rtIcon, iconDirAt, true)
	putEntry(section, resourceDirSize+resourceEntrySize, rtGroupIcon, groupDirAt, true)

	// Level two: one name per icon, and the single group.
	putDirectory(section, iconDirAt, iconCount)
	for i := 0; i < iconCount; i++ {
		langDir := langDirsAt + i*langDirSize
		putEntry(section, iconDirAt+resourceDirSize+i*resourceEntrySize, uint32(i+1), langDir, true)
	}
	putDirectory(section, groupDirAt, 1)
	putEntry(section, groupDirAt+resourceDirSize, 1, langDirsAt+iconCount*langDirSize, true)

	// Level three: the language of each leaf, pointing at its data entry.
	var relocations []relocation
	for leaf := 0; leaf < leafCount; leaf++ {
		langDir := langDirsAt + leaf*langDirSize
		dataEntry := dataEntriesAt + leaf*resourceDataSize

		putDirectory(section, langDir, 1)
		putEntry(section, langDir+resourceDirSize, languageNeutral, dataEntry, false)

		// The address field is what the linker rewrites once it knows where
		// the section landed in the image.
		binary.LittleEndian.PutUint32(section[dataEntry:], uint32(blobOffsets[leaf]))
		binary.LittleEndian.PutUint32(section[dataEntry+4:], uint32(len(blobs[leaf])))
		relocations = append(relocations, relocation{offset: uint32(dataEntry)})
	}

	for i, blob := range blobs {
		copy(section[blobOffsets[i]:], blob)
	}
	return section, relocations
}

// putDirectory writes an IMAGE_RESOURCE_DIRECTORY with only id entries.
func putDirectory(section []byte, at, idEntries int) {
	// Characteristics, timestamp and version are all zero; only the entry
	// counts carry information.
	binary.LittleEndian.PutUint16(section[at+12:], 0)                 // named entries
	binary.LittleEndian.PutUint16(section[at+14:], uint16(idEntries)) // id entries
}

// putEntry writes an IMAGE_RESOURCE_DIRECTORY_ENTRY. The high bit of the
// offset marks it as pointing at another directory rather than at data.
func putEntry(section []byte, at int, id uint32, offset int, isDirectory bool) {
	value := uint32(offset)
	if isDirectory {
		value |= 0x80000000
	}
	binary.LittleEndian.PutUint32(section[at:], id)
	binary.LittleEndian.PutUint32(section[at+4:], value)
}

// buildObject wraps the resource section in a COFF object file.
func buildObject(section []byte, relocations []relocation) []byte {
	const symbolCount = 2 // the section symbol and its auxiliary record

	sectionDataAt := coffHeaderSize + sectionHeaderSize
	relocationsAt := sectionDataAt + len(section)
	symbolsAt := relocationsAt + len(relocations)*relocationSize

	var b bytes.Buffer

	// COFF header.
	write(&b,
		uint16(machineAMD64),
		uint16(1), // one section
		uint32(0), // timestamp: left at zero so builds are reproducible
		uint32(symbolsAt),
		uint32(symbolCount),
		uint16(0), // no optional header in an object file
		uint16(0), // characteristics
	)

	// Section header.
	b.WriteString(".rsrc\x00\x00\x00")
	write(&b,
		uint32(0), // virtual size
		uint32(0), // virtual address
		uint32(len(section)),
		uint32(sectionDataAt),
		uint32(relocationsAt),
		uint32(0), // line numbers
		uint16(len(relocations)),
		uint16(0), // line number count
		uint32(sectionCharacteristics),
	)

	b.Write(section)

	// Relocations, all against the section symbol at index zero.
	for _, r := range relocations {
		write(&b, r.offset, uint32(0), uint16(relocAddr32NB))
	}

	// The section symbol.
	b.WriteString(".rsrc\x00\x00\x00")
	write(&b,
		uint32(0), // value
		uint16(1), // section number, one based
		uint16(0), // type
	)
	b.WriteByte(symbolClassStatic)
	b.WriteByte(1) // one auxiliary record follows

	// Auxiliary section record.
	write(&b,
		uint32(len(section)),
		uint16(len(relocations)),
		uint16(0), // line numbers
		uint32(0), // checksum
		uint16(0), // associated section
		uint8(0),  // COMDAT selection
	)
	b.Write(make([]byte, 3)) // padding to the 18 byte symbol size

	// An empty string table is still four bytes: its own length.
	write(&b, uint32(4))

	return b.Bytes()
}

func align(value, to int) int {
	if remainder := value % to; remainder != 0 {
		return value + to - remainder
	}
	return value
}
