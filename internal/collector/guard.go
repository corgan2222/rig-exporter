package collector

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// defaultSourceDeadlineShare is how much of one poll interval a single source
// may take before the tick moves on without it.
//
// A share rather than a fixed duration: a fixed one would cut a source short on
// a slow poll interval and never fire on a fast one. Half leaves room for the
// rest of the tick even when one source uses its whole allowance.
const defaultSourceDeadlineShare = 2

// guardedSource wraps one optional source with the two guarantees the collector
// comment has always promised and only half delivered: a source that fails
// contributes nothing and is recorded, and the others keep reporting.
//
// Failing used to mean "returned an error". It now also means "panicked" and
// "did not answer in time".
type guardedSource struct {
	Source

	// stuck is set while a goroutine is still inside Collect after its deadline
	// passed. Go cannot cancel that goroutine — it is sitting in a Win32 call —
	// so the only thing left is to not start another one beside it. Without
	// this a permanently dead drive would leak one goroutine per tick.
	stuck atomic.Bool

	// disabled is set after a panic and never cleared. Catching a panic every
	// tick and carrying on would be a program running broken in silence, which
	// is precisely what the crash handler exists to prevent.
	disabled bool
	reason   string
}

// collect runs the source under its deadline and reports what to record.
//
// The set handed to the source is the real one, deliberately: sources read it.
// The processor source asks whether a temperature is already there before
// spending a PDH query on it, and the GPU sources ask before every reading —
// that is how the cheapest source wins. Giving each source a private set would
// silently break the rule.
//
// What makes that safe against a late writer is the three-index slice below.
func (g *guardedSource) collect(set *metrics.Set, deadline time.Duration, log *slog.Logger) error {
	if g.disabled {
		return fmt.Errorf("%s", g.reason)
	}
	if g.stuck.Load() {
		return fmt.Errorf("still inside the previous collection, skipped")
	}

	// Capped at its own length, so the source's first append has to allocate a
	// new array instead of writing into the one the tick is about to export.
	// Reads see everything the earlier sources supplied; writes cannot reach
	// them. That is what lets a source overrun its deadline without corrupting
	// the snapshot it was already dropped from.
	own := metrics.Set{
		Origin:   set.Origin,
		Readings: set.Readings[:len(set.Readings):len(set.Readings)],
	}
	before := len(own.Readings)

	done := make(chan sourceOutcome, 1) // buffered: nobody reads it after a timeout
	go func() {
		outcome := sourceOutcome{}
		defer func() {
			if r := recover(); r != nil {
				outcome.panicked = true
				outcome.value = r
				outcome.stack = debug.Stack()
			}
			done <- outcome
		}()
		outcome.err = g.Collect(&own)
	}()

	timer := time.NewTimer(deadline)
	defer timer.Stop()

	select {
	case outcome := <-done:
		if outcome.panicked {
			g.disabled = true
			g.reason = fmt.Sprintf("disabled after a panic: %v", outcome.value)
			// The stack goes to the log, not into SourceErrors: that string is
			// shown on the status page next to the group, and a goroutine dump
			// belongs in a file. Loud either way — a caught panic that left no
			// trace would be the silent failure this guard is accused of
			// creating.
			log.Error("source panicked and was disabled",
				"group", g.Group(), "panic", outcome.value,
				"stack", string(outcome.stack))
			return fmt.Errorf("%s", g.reason)
		}
		set.Readings = append(set.Readings, own.Readings[before:]...)
		return outcome.err

	case <-timer.C:
		g.stuck.Store(true)
		go func() {
			<-done
			g.stuck.Store(false)
		}()
		return fmt.Errorf("no answer within %s", deadline)
	}
}

// sourceOutcome is what one run of a source produced, panic included.
type sourceOutcome struct {
	err      error
	panicked bool
	value    any
	stack    []byte
}
