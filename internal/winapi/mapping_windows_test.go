//go:build windows

package winapi

import (
	"testing"

	"golang.org/x/sys/windows"
)

// CommittedBytes has to report the region that was really mapped, because the
// callers use it to decide how much they may copy.
//
// Both shared-memory readers take the size to copy out of a header written by
// another process. RTSS and Afterburner publish their mappings under
// session-local names with no Global\ prefix, so any process running as the
// same user can create one first and put any number in that header. A copy
// past the region is an access violation, which Go does not recover from and
// which under -H windowsgui ends the program with no window and no log line.
//
// A mapping of a known size is the only way to check this without a second
// process: ask for one page, and the answer has to describe that page rather
// than whatever the caller hoped for.
func TestCommittedBytesReportsTheMappedRegion(t *testing.T) {
	const want = 4096

	mapping, err := windows.CreateFileMapping(
		windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, want, nil)
	if err != nil {
		t.Fatalf("CreateFileMapping: %v", err)
	}
	defer windows.CloseHandle(mapping)

	addr, err := windows.MapViewOfFile(mapping, windows.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		t.Fatalf("MapViewOfFile: %v", err)
	}
	defer windows.UnmapViewOfFile(addr)

	got := CommittedBytes(addr)
	if got < want {
		t.Errorf("CommittedBytes = %d, want at least the %d bytes that were mapped",
			got, want)
	}
	// The number is a clamp, so one that is too large is the dangerous
	// direction: it would let a caller copy past the region it describes.
	// A view of one page is rounded up to the allocation granularity at most,
	// never to megabytes.
	if got > 1<<20 {
		t.Errorf("CommittedBytes = %d for a %d byte mapping; a clamp that large clamps nothing",
			got, want)
	}
}

// A pointer into no mapping at all must answer 0 rather than a number a caller
// would act on. 0 is what both readers treat as "cannot be determined", and
// they then fall back to the header — which is the pre-existing behaviour and
// no worse than before.
func TestCommittedBytesAnswersZeroForUnmappedMemory(t *testing.T) {
	if got := CommittedBytes(0); got != 0 {
		t.Errorf("CommittedBytes(0) = %d, want 0", got)
	}
}
