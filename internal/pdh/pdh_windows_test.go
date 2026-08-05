//go:build windows

package pdh

import (
	"strings"
	"testing"
	"time"
)

const testCounter = `\Processor Information(_Total)\% Processor Performance`

// Every process on the machine has this counter, so the wildcard expands
// wherever the tests run — including a build agent without a graphics card,
// which is why it stands in for the GPU engine counters here.
const testWildcard = `\Process(*)\% Processor Time`

// The reading taken straight after opening the counter is the one that used to
// poison the observed peak for the rest of the process, so it has to be refused
// rather than merely be unlikely to spike.
func TestAReadingIsRefusedUntilTheWindowIsMeaningful(t *testing.T) {
	c := NewCounter(testCounter)
	defer c.Close()
	if !c.q.open {
		t.Skip("performance counters are not available on this machine")
	}

	if _, err := c.Value(); err == nil {
		t.Error("a reading was accepted over a window of microseconds")
	}

	time.Sleep(MinInterval + 20*time.Millisecond)

	value, err := c.Value()
	if err != nil {
		t.Fatalf("Value after %s: %v", MinInterval, err)
	}
	// Anything from a parked core to a boosting one, but a percentage.
	if value <= 0 || value > 400 {
		t.Errorf("%% Processor Performance = %v, which is not a plausible percentage", value)
	}
}

// Refusing must not collect: that would move the baseline forward, and a caller
// polling faster than the floor would be starved of readings for ever.
func TestRefusingDoesNotPushTheWindowForward(t *testing.T) {
	c := NewCounter(testCounter)
	defer c.Close()
	if !c.q.open {
		t.Skip("performance counters are not available on this machine")
	}

	opened := c.q.lastRead
	for i := 0; i < 5; i++ {
		c.Value()
	}
	if !c.q.lastRead.Equal(opened) {
		t.Error("a refused call moved the baseline")
	}

	time.Sleep(MinInterval + 20*time.Millisecond)
	if _, err := c.Value(); err != nil {
		t.Errorf("Value after the floor elapsed: %v", err)
	}
}

// A machine without a working counter subsystem must not take the source using
// it down as well; every caller has a fallback.
func TestAnUnopenableCounterFailsQuietly(t *testing.T) {
	c := NewCounter(`\No Such Object(_Total)\No Such Counter`)
	defer c.Close()

	if c.q.open {
		t.Fatal("a counter that does not exist reported itself as open")
	}
	if _, err := c.Value(); err == nil {
		t.Error("Value on an unopened counter returned no error")
	}
}

func TestAWildcardCounterReportsOneValuePerInstance(t *testing.T) {
	a := NewArray(testWildcard)
	defer a.Close()
	if !a.q.open {
		t.Skip("performance counters are not available on this machine")
	}

	time.Sleep(MinInterval + 20*time.Millisecond)

	values, err := a.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	// A machine running this test has at least a handful of processes, and
	// _Total is always among them.
	if len(values) < 5 {
		t.Errorf("only %d instances, which cannot be all the processes on this machine", len(values))
	}
	if _, ok := values["_Total"]; !ok {
		t.Error("the _Total instance is missing")
	}
	for name, value := range values {
		if strings.TrimSpace(name) == "" {
			t.Error("an instance came back without a name")
		}
		if value < 0 {
			t.Errorf("%s reported %v, which is not a share of anything", name, value)
		}
	}
}

// The buffer is kept between reads and the instance count moves with the
// processes on the machine, so a second read has to be as correct as the first.
func TestTheKeptBufferSurvivesASecondRead(t *testing.T) {
	a := NewArray(testWildcard)
	defer a.Close()
	if !a.q.open {
		t.Skip("performance counters are not available on this machine")
	}

	time.Sleep(MinInterval + 20*time.Millisecond)
	first, err := a.Values()
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	time.Sleep(MinInterval + 20*time.Millisecond)
	second, err := a.Values()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}

	if len(second) < len(first)/2 {
		t.Errorf("second read returned %d instances against %d in the first", len(second), len(first))
	}
	if _, ok := second["_Total"]; !ok {
		t.Error("the _Total instance went missing on the second read")
	}
}
