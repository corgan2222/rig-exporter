//go:build windows

package main

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/crashlog"
)

// countingRecord stands in for the crash recorder and remembers what was done
// to it. What a disarm does to the file is the crashlog package's business and
// is tested there; what is decided here is when it happens.
type countingRecord struct {
	disarmed int
	closed   int
}

func (c *countingRecord) Disarm()      { c.disarmed++ }
func (c *countingRecord) Close() error { c.closed++; return nil }

// Every planned ending empties the record, whatever code it ends with.
//
// Three of the four ways out of this program used to walk past the one place
// that emptied it: the update that hands over to its helper, and the two exits
// that followed a failed start. Each left a session header behind, and a header
// alone reads as a crash — so a perfectly correct update was followed by a
// crash banner offering to file a bug report about it.
func TestEveryPlannedEndingEmptiesTheRecord(t *testing.T) {
	for _, ending := range []struct {
		name string
		code int
	}{
		{"the tray quit", 0},
		{"the updater handing over", 0},
		{"a start that failed and said so", 1},
	} {
		t.Run(ending.name, func(t *testing.T) {
			record := &countingRecord{}

			if got := endSession(record, func() int { return ending.code }); got != ending.code {
				t.Errorf("exit code = %d, want %d", got, ending.code)
			}
			if record.disarmed != 1 {
				t.Errorf("disarmed %d times, want once", record.disarmed)
			}
			if record.closed != 1 {
				t.Errorf("closed %d times, want once", record.closed)
			}
		})
	}
}

// And a crash does not.
//
// This is the half a deferred call would get wrong, and it would get it wrong
// quietly. A defer also runs while a panic unwinds — before the runtime prints
// the stack — so the record would be emptied in the instant between the fault
// and the evidence, and the kept report would arrive with a stack and no
// session header: no version, no build, no pid. Nothing about it would look
// broken except the fields nobody can supply afterwards.
func TestACrashDoesNotEmptyTheRecordOnItsWayOut(t *testing.T) {
	record := &countingRecord{}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the session did not panic, so this proves nothing")
			}
		}()
		_ = endSession(record, func() int { panic("a fault in the session") })
	}()

	if record.disarmed != 0 {
		t.Errorf("a crash emptied its own record %d times", record.disarmed)
	}
}

// Arming is allowed to fail: the program then runs exactly as it did before
// there was a crash recorder at all. What comes through here is a nil
// *crashlog.Recorder, not a nil interface, and its methods are written to
// survive that — but only this test says so.
func TestASessionWithoutARecordStillEnds(t *testing.T) {
	var missing *crashlog.Recorder

	if got := endSession(missing, func() int { return 3 }); got != 3 {
		t.Errorf("exit code = %d, want 3", got)
	}
}
