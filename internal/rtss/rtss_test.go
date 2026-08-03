package rtss

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// buildEntry lays out one RTSS_SHARED_MEMORY_APP_ENTRY of entrySize bytes.
func buildEntry(entrySize int, e Entry) []byte {
	b := make([]byte, entrySize)
	le := binary.LittleEndian
	le.PutUint32(b[offProcessID:], e.ProcessID)
	copy(b[offName:offName+nameLen-1], e.Path)
	le.PutUint32(b[offFlags:], e.Flags)
	le.PutUint32(b[offTime0:], e.Time0)
	le.PutUint32(b[offTime1:], e.Time1)
	le.PutUint32(b[offFrames:], e.Frames)
	le.PutUint32(b[offFrameTime:], e.FrameTimeUs)
	le.PutUint32(b[offStatFramerateAvg:], e.StatFramerateAvg)
	return b
}

// buildSharedMemory assembles a block shaped like the one RTSS publishes.
func buildSharedMemory(version uint32, entrySize, arrOffset int, entries []Entry) []byte {
	buf := make([]byte, arrOffset+entrySize*len(entries))
	le := binary.LittleEndian
	le.PutUint32(buf[offSignature:], signature)
	le.PutUint32(buf[offVersion:], version)
	le.PutUint32(buf[offAppEntrySize:], uint32(entrySize))
	le.PutUint32(buf[offAppArrOffset:], uint32(arrOffset))
	le.PutUint32(buf[offAppArrSize:], uint32(len(entries)))

	for i, e := range entries {
		copy(buf[arrOffset+i*entrySize:], buildEntry(entrySize, e))
	}
	return buf
}

// Closing RTSS does not remove its shared memory: RTSSHooks stays loaded inside
// every hooked application, so the section survives and RTSS marks it with
// 0xDEAD on the way out. Reported as an unexpected signature, that read to the
// user as "something is wrong" when the truth was "RTSS was closed".
//
// Both entry points are checked because the reader calls MappingSize first: a
// fix applied only to Parse would never run.
func TestAClosedRTSSReadsAsNotRunning(t *testing.T) {
	buf := buildSharedMemory(0x00020015, 512, 4096, []Entry{{ProcessID: 1234}})
	binary.LittleEndian.PutUint32(buf[offSignature:], signatureDead)

	for name, err := range map[string]error{
		"MappingSize": second(MappingSize(buf)),
		"Parse":       second(Parse(buf)),
	} {
		if !errors.Is(err, ErrShutDown) {
			t.Errorf("%s = %v, want ErrShutDown", name, err)
		}
		// Callers branch on these separately; conflating them is the bug.
		if errors.Is(err, ErrBadSignature) {
			t.Errorf("%s reports a shutdown as a bad signature", name)
		}
	}
}

// A genuinely foreign mapping still has to be reported, and has to say what was
// actually there — otherwise nobody can tell you.
func TestAForeignSignatureIsReportedWithItsValue(t *testing.T) {
	buf := buildSharedMemory(0x00020015, 512, 4096, nil)
	binary.LittleEndian.PutUint32(buf[offSignature:], 0x12345678)

	_, err := MappingSize(buf)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("MappingSize = %v, want ErrBadSignature", err)
	}
	if !strings.Contains(err.Error(), "12345678") {
		t.Errorf("error %q does not name the signature it found", err)
	}
}

// In the 1.x layout the offsets read here hold tick counts, which parse into
// nonsense rather than failing. The version has to be checked before they are
// trusted.
func TestAnOldLayoutIsRefusedRatherThanMisread(t *testing.T) {
	buf := buildSharedMemory(0x00010005, 512, 4096, []Entry{{ProcessID: 1234}})

	if _, err := MappingSize(buf); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("MappingSize = %v, want ErrUnsupportedVersion", err)
	}
	if _, err := Parse(buf); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Parse = %v, want ErrUnsupportedVersion", err)
	}
}

// second drops the value so the two entry points can be compared in one table.
func second[T any](_ T, err error) error { return err }

func TestParseReadsEntries(t *testing.T) {
	buf := buildSharedMemory(0x00020007, 512, 4096, []Entry{
		{ProcessID: 0}, // the global slot RTSS keeps at index 0
		{ProcessID: 1234, Path: `C:\Games\Cyberpunk2077.exe`, Time0: 1000, Time1: 2000, Frames: 143, FrameTimeUs: 6980},
	})

	snap, err := Parse(buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := snap.VersionString(); got != "2.7" {
		t.Errorf("VersionString = %q, want 2.7", got)
	}
	if len(snap.Entries) != 1 {
		t.Fatalf("got %d entries, want 1 (pid 0 must be skipped)", len(snap.Entries))
	}

	e := snap.Entries[0]
	if e.ProcessID != 1234 {
		t.Errorf("ProcessID = %d, want 1234", e.ProcessID)
	}
	if e.Name() != "Cyberpunk2077.exe" {
		t.Errorf("Name = %q, want Cyberpunk2077.exe", e.Name())
	}
	if got := e.FPS(); got != 143 {
		t.Errorf("FPS = %v, want 143", got)
	}
	if got := e.FrametimeMs(); got != 6.98 {
		t.Errorf("FrametimeMs = %v, want 6.98", got)
	}
}

func TestFrametimeFallsBackToFPS(t *testing.T) {
	e := Entry{Time0: 0, Time1: 1000, Frames: 50} // no FrameTimeUs
	if got := e.FrametimeMs(); got != 20 {
		t.Errorf("FrametimeMs = %v, want 20", got)
	}
}

func TestFPSWithoutWindowIsZero(t *testing.T) {
	e := Entry{Time0: 5000, Time1: 5000, Frames: 99}
	if got := e.FPS(); got != 0 {
		t.Errorf("FPS = %v, want 0 for an empty measurement window", got)
	}
}

func TestParseRejectsForeignMemory(t *testing.T) {
	buf := make([]byte, 128)
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

// Parse must not walk past the end of the mapping when the header advertises
// more entries than the block actually contains.
func TestParseStopsAtEndOfBuffer(t *testing.T) {
	buf := buildSharedMemory(0x00020007, 512, 4096, []Entry{
		{ProcessID: 1, Path: "a.exe", Time1: 10},
	})
	binary.LittleEndian.PutUint32(buf[offAppArrSize:], 64) // lie about the count

	snap, err := Parse(buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(snap.Entries) != 1 {
		t.Errorf("got %d entries, want 1", len(snap.Entries))
	}
}

func TestMappingSizeCoversTheAppArray(t *testing.T) {
	buf := buildSharedMemory(0x00020007, 512, 4096, []Entry{{ProcessID: 1}, {ProcessID: 2}})

	size, err := MappingSize(buf[:headerSize])
	if err != nil {
		t.Fatalf("MappingSize: %v", err)
	}
	if want := 4096 + 2*512; size != want {
		t.Errorf("MappingSize = %d, want %d", size, want)
	}
}

func TestMappingSizeRejectsImplausibleHeader(t *testing.T) {
	buf := buildSharedMemory(0x00020007, 512, 4096, nil)
	binary.LittleEndian.PutUint32(buf[offAppArrSize:], 1<<30)

	if _, err := MappingSize(buf[:headerSize]); err == nil {
		t.Error("MappingSize accepted an entry count of 2^30")
	}
}

func TestSelectActivePrefersForegroundProcess(t *testing.T) {
	entries := []Entry{
		{ProcessID: 10, Path: "background.exe", Time0: 0, Time1: 9000, Frames: 60},
		{ProcessID: 20, Path: "foreground.exe", Time0: 0, Time1: 8000, Frames: 120},
	}

	got, ok := SelectActive(entries, 20, 9000, 3000)
	if !ok {
		t.Fatal("SelectActive found nothing")
	}
	if got.Name() != "foreground.exe" {
		t.Errorf("selected %q, want foreground.exe", got.Name())
	}
}

func TestSelectActiveFallsBackToMostRecent(t *testing.T) {
	entries := []Entry{
		{ProcessID: 10, Path: "older.exe", Time1: 8000},
		{ProcessID: 20, Path: "newer.exe", Time1: 8900},
	}

	// Foreground is a process RTSS has never seen, e.g. a browser.
	got, ok := SelectActive(entries, 999, 9000, 3000)
	if !ok {
		t.Fatal("SelectActive found nothing")
	}
	if got.Name() != "newer.exe" {
		t.Errorf("selected %q, want newer.exe", got.Name())
	}
}

func TestSelectActiveIgnoresStaleEntries(t *testing.T) {
	entries := []Entry{
		{ProcessID: 10, Path: "closed.exe", Time1: 1000},
	}

	if _, ok := SelectActive(entries, 10, 60000, 3000); ok {
		t.Error("a game that stopped rendering 59s ago was still selected")
	}
}

// GetTickCount wraps every ~49 days; an entry written just before the wrap
// must not look 49 days old immediately after it.
func TestSelectActiveSurvivesTickWraparound(t *testing.T) {
	const beforeWrap = 0xFFFFF000
	entries := []Entry{{ProcessID: 10, Path: "game.exe", Time0: beforeWrap - 1000, Time1: beforeWrap, Frames: 60}}

	afterWrap := uint32(0x00000500) // ~5.4s of ticks later, past the wrap
	if _, ok := SelectActive(entries, 0, afterWrap, 30000); !ok {
		t.Error("entry was discarded across the tick wraparound")
	}
}

func TestSelectActiveSkipsEntriesThatNeverRendered(t *testing.T) {
	entries := []Entry{{ProcessID: 10, Path: "hooked-but-idle.exe", Time1: 0}}

	if _, ok := SelectActive(entries, 10, 5000, 3000); ok {
		t.Error("an entry with no frame timestamp was selected")
	}
}
