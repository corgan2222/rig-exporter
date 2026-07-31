//go:build windows

package cpu

import (
	"testing"
	"time"
)

const testCounter = `\Processor Information(_Total)\% Processor Performance`

// The reading taken straight after opening the counter is the one that used to
// poison the observed peak for the rest of the process, so it has to be refused
// rather than merely be unlikely to spike.
func TestAReadingIsRefusedUntilTheWindowIsMeaningful(t *testing.T) {
	c := newPerfCounter(testCounter)
	defer c.Close()
	if !c.open {
		t.Skip("performance counters are not available on this machine")
	}

	if _, err := c.Value(); err == nil {
		t.Error("a reading was accepted over a window of microseconds")
	}

	time.Sleep(minInterval + 20*time.Millisecond)

	value, err := c.Value()
	if err != nil {
		t.Fatalf("Value after %s: %v", minInterval, err)
	}
	// Anything from a parked core to a boosting one, but a percentage.
	if value <= 0 || value > 400 {
		t.Errorf("%% Processor Performance = %v, which is not a plausible percentage", value)
	}
}

// Refusing must not collect: that would move the baseline forward, and a caller
// polling faster than the floor would be starved of readings for ever.
func TestRefusingDoesNotPushTheWindowForward(t *testing.T) {
	c := newPerfCounter(testCounter)
	defer c.Close()
	if !c.open {
		t.Skip("performance counters are not available on this machine")
	}

	opened := c.lastRead
	for i := 0; i < 5; i++ {
		c.Value()
	}
	if !c.lastRead.Equal(opened) {
		t.Error("a refused call moved the baseline")
	}

	time.Sleep(minInterval + 20*time.Millisecond)
	if _, err := c.Value(); err != nil {
		t.Errorf("Value after the floor elapsed: %v", err)
	}
}

// A machine without a working counter subsystem must not take the CPU source
// down with it; the caller falls back to the nominal frequency.
func TestAnUnopenableCounterFailsQuietly(t *testing.T) {
	c := newPerfCounter(`\No Such Object(_Total)\No Such Counter`)
	defer c.Close()

	if c.open {
		t.Fatal("a counter that does not exist reported itself as open")
	}
	if _, err := c.Value(); err == nil {
		t.Error("Value on an unopened counter returned no error")
	}
}
