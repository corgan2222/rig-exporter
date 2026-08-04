//go:build windows

package procs

import (
	"math"
	"testing"

	"github.com/corgan/rig-exporter/internal/winapi"
)

// A browser is one program, however many processes it spread itself over. That
// is the whole reason this groups by name: twenty-eight entries called
// firefox.exe would fill a top five on their own and say nothing.
func TestProcessesOfOneProgramAreCountedTogether(t *testing.T) {
	before := map[uint32]uint64{1: 0, 2: 0, 3: 0}
	now := []winapi.ProcessSample{
		{PID: 1, Name: "firefox.exe", CPUTime: 1e7}, // 1 s
		{PID: 2, Name: "firefox.exe", CPUTime: 1e7}, // 1 s
		{PID: 3, Name: "cs2.exe", CPUTime: 1.5e7},   // 1.5 s
	}

	// One second of wall clock on a machine with four threads: four
	// thread-seconds of capacity.
	rows := topCPU(now, before, 1, 5)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want one per program: %+v", len(rows), rows)
	}
	if rows[0].Label != "firefox.exe" {
		t.Errorf("leader = %q, want the two firefox processes added up", rows[0].Label)
	}
}

// Share of the whole machine, the same denominator Task Manager uses. A program
// pinning one thread of thirty-two is at three per cent, not a hundred, or two
// machines with different core counts could never be compared.
func TestTheCPUShareIsOfTheWholeMachine(t *testing.T) {
	before := map[uint32]uint64{1: 0}
	// One full second of CPU in a one-second window.
	now := []winapi.ProcessSample{{PID: 1, Name: "cs2.exe", CPUTime: 1e7}}

	rows := topCPU(now, before, 1, 5)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	// The test machine decides the denominator, so the assertion is the
	// relationship rather than a number: one busy thread out of all of them.
	want := 100 / float64(winapi.LogicalProcessors())
	if math.Abs(rows[0].Value-want) > 0.01 {
		t.Errorf("share = %.2f %%, want %.2f %% on %d logical processors",
			rows[0].Value, want, winapi.LogicalProcessors())
	}
}

// A process that started inside the window has no baseline. Counting it from
// zero would credit it with everything it has done since it launched, which for
// a compiler that has been running a minute is a share far above a hundred.
func TestAProcessWithoutABaselineIsSkipped(t *testing.T) {
	before := map[uint32]uint64{1: 0}
	now := []winapi.ProcessSample{
		{PID: 1, Name: "cs2.exe", CPUTime: 1e6},
		{PID: 99, Name: "compiler.exe", CPUTime: 600e7}, // a minute of CPU, just seen
	}

	rows := topCPU(now, before, 1, 5)
	for _, row := range rows {
		if row.Label == "compiler.exe" {
			t.Errorf("a process seen for the first time was credited with %.1f %%", row.Value)
		}
	}
}

// The accounting buckets are not programs anybody chose to run, and Idle would
// win the CPU ranking on an idle machine by a wide margin.
func TestSystemBucketsNeverEnterTheRanking(t *testing.T) {
	before := map[uint32]uint64{0: 0, 4: 0, 7: 0, 9: 0}
	now := []winapi.ProcessSample{
		{PID: 0, Name: "Idle", CPUTime: 100e7},
		{PID: 4, Name: "System", CPUTime: 50e7},
		{PID: 7, Name: "Memory Compression", CPUTime: 20e7},
		{PID: 9, Name: "cs2.exe", CPUTime: 1e7},
	}

	rows := topCPU(now, before, 1, 5)
	if len(rows) != 1 || rows[0].Label != "cs2.exe" {
		t.Errorf("ranking = %+v, want only the real program", rows)
	}
}

// Private bytes rather than the working set: the twenty-eight processes of one
// browser each map the same libraries, and adding their working sets up
// overstates the browser by gigabytes.
func TestMemoryIsRankedAsAShareOfTheMachine(t *testing.T) {
	const total = 128 * 1024 * 1024 * 1024
	now := []winapi.ProcessSample{
		{PID: 1, Name: "firefox.exe", PrivateBytes: 4 * 1024 * 1024 * 1024},
		{PID: 2, Name: "firefox.exe", PrivateBytes: 4 * 1024 * 1024 * 1024},
		{PID: 3, Name: "cs2.exe", PrivateBytes: 2 * 1024 * 1024 * 1024},
	}

	rows := topMemory(now, total, 5)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].Label != "firefox.exe" || math.Abs(rows[0].Value-6.25) > 0.01 {
		t.Errorf("leader = %+v, want firefox.exe at 6.25 %% of 128 GB", rows[0])
	}
}

// Without a total there is no share, and reporting the raw byte count under a
// heading that says per cent would be the wrong number rather than none.
func TestNoTotalMeansNoMemoryRanking(t *testing.T) {
	if rows := topMemory([]winapi.ProcessSample{{PID: 1, Name: "a.exe", PrivateBytes: 1}}, 0, 5); rows != nil {
		t.Errorf("ranked %+v without knowing the machine's memory", rows)
	}
}

// Equal values must not make the order wobble between two collections that
// measured exactly the same thing — every wobble is a message to the broker and
// a row in a database.
func TestTiesAreBrokenByNameSoTheOrderHolds(t *testing.T) {
	before := map[uint32]uint64{1: 0, 2: 0}
	now := []winapi.ProcessSample{
		{PID: 1, Name: "zzz.exe", CPUTime: 1e7},
		{PID: 2, Name: "aaa.exe", CPUTime: 1e7},
	}

	for range 5 {
		rows := topCPU(now, before, 1, 5)
		if rows[0].Label != "aaa.exe" {
			t.Fatalf("leader = %q, want the tie broken by name every time", rows[0].Label)
		}
	}
}

func TestTheRankingKeepsOnlyAsManyAsAsked(t *testing.T) {
	var now []winapi.ProcessSample
	before := map[uint32]uint64{}
	for i := 1; i <= 20; i++ {
		before[uint32(i)] = 0
		now = append(now, winapi.ProcessSample{
			PID: uint32(i), Name: string(rune('a'+i)) + ".exe", CPUTime: uint64(i) * 1e6,
		})
	}

	if rows := topCPU(now, before, 1, 5); len(rows) != 5 {
		t.Errorf("got %d rows, want 5", len(rows))
	}
}
