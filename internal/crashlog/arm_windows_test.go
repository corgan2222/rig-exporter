//go:build windows

package crashlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// arm opens a record and gives the process its standard error back afterwards.
//
// Both halves matter. Arm points this process's standard error at a file and
// never takes it back — deliberately, because a program that has started is
// supposed to keep it for the rest of its life. In a test binary that is a
// borrowed stream and an open handle: without the restore below, a later panic
// in the test run would be written into a temporary file instead of the
// terminal, and without the close the temporary directory cannot be removed.
func arm(t *testing.T, dir string) *Recorder {
	t.Helper()

	before, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil {
		t.Fatalf("GetStdHandle: %v", err)
	}
	stderrBefore := os.Stderr

	recorder, err := Arm(dir, "1.9.4", "test", filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("Arm: %v", err)
	}
	t.Cleanup(func() {
		_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, before)
		os.Stderr = stderrBefore
		if recorder.file != nil {
			_ = recorder.file.Close()
		}
	})
	return recorder
}

// The writer of the log section is exercised by nothing else.
//
// It cannot be red while the literal it used to write and the constant Split
// looks with are the same string — that agreement is the coincidence being
// removed here. It is written as the guard for the moment the agreement ends:
// then Split stops finding the divider, hands the whole record back as the
// stack, and the prepared issue puts the trace and the application log into one
// field, cut to the stack's budget. That mistake has been paid for once
// already, and nothing was watching for it.
//
// Checked by mutation rather than by a run: change logMarker and this test goes
// red, where before the fix it stayed green.
func TestTheWrittenLogSectionIsFoundBySplit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rig-exporter.log")
	if err := os.WriteFile(logPath, []byte("first line\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := Report{Text: "panic: boom\n\tmain.main()" + logSection(logPath)}
	crash, log := report.Split()

	if strings.Contains(crash, "second line") {
		t.Error("the application log leaked into the panic field")
	}
	if !strings.Contains(log, "second line") {
		t.Error("Split did not find the marker the writer wrote")
	}
}

// A record left armed is what the next start reads as a crash.
//
// This is the mechanism the whole thing rests on, and it is worth stating on
// its own: the detection is not clever, it is the absence of an ending. A
// session header alone, with nothing after it, means a session that started and
// never got to the line that empties the file.
func TestARecordLeftArmedReadsAsACrashAtTheNextStart(t *testing.T) {
	dir := t.TempDir()

	arm(t, dir) // and no Disarm: the way out that main.go used to have

	next := arm(t, dir)
	report := next.Previous()
	if report == nil {
		t.Fatal("a record left armed was not read back as a crash")
	}
	if report.Kind != KindUnclean {
		t.Errorf("kind = %q, want %q", report.Kind, KindUnclean)
	}
	if report.Version != "1.9.4" {
		t.Errorf("version = %q; the session header did not survive", report.Version)
	}
}

// And a disarmed one is not. Emptying the file is the whole ending.
func TestADisarmedRecordIsNotACrash(t *testing.T) {
	dir := t.TempDir()

	first := arm(t, dir)
	first.Disarm()
	_ = first.Close()

	if report := arm(t, dir).Previous(); report != nil {
		t.Fatalf("a session that ended on purpose left a report: kind=%s", report.Kind)
	}
}
