package cpu

import (
	"math"
	"testing"
)

// The first sample is the starting value rather than something to decay
// towards from zero: otherwise every start would report a minute of load that
// was never there.
func TestFirstSampleSetsAllThreeAverages(t *testing.T) {
	var l LoadAverage

	if _, ready := l.Values(); ready {
		t.Error("an unprimed average reports itself as ready")
	}

	l.Add(4, 0)
	values, ready := l.Values()
	if !ready {
		t.Fatal("the average is not ready after a sample")
	}
	for i, v := range values {
		if v != 4 {
			t.Errorf("window %d = %v, want 4", i, v)
		}
	}
}

// A step from idle to fully busy has to move the one minute average faster
// than the fifteen minute one, and none of them past the new value.
func TestShorterWindowsRespondFaster(t *testing.T) {
	var l LoadAverage
	l.Add(0, 0)

	// One minute of sustained load, sampled every second.
	for i := 0; i < 60; i++ {
		l.Add(8, 1)
	}
	values, _ := l.Values()

	if !(values[0] > values[1] && values[1] > values[2]) {
		t.Errorf("windows did not respond in order: %v", values)
	}
	for i, v := range values {
		if v > 8 {
			t.Errorf("window %d overshot to %v", i, v)
		}
	}
	// One time constant of a step response reaches about 63 percent.
	if values[0] < 4.5 || values[0] > 5.5 {
		t.Errorf("one minute average = %v after one minute of load 8, want about 5", values[0])
	}
}

// The averages describe spans of time, so reading twice as often must not
// make them react twice as fast.
func TestSampleRateDoesNotChangeTheAverage(t *testing.T) {
	var fast, slow LoadAverage
	fast.Add(0, 0)
	slow.Add(0, 0)

	for i := 0; i < 120; i++ {
		fast.Add(6, 0.5) // every half second
	}
	for i := 0; i < 60; i++ {
		slow.Add(6, 1) // every second
	}

	fastValues, _ := fast.Values()
	slowValues, _ := slow.Values()
	for i := range fastValues {
		if math.Abs(fastValues[i]-slowValues[i]) > 0.01 {
			t.Errorf("window %d: %v at 2 Hz against %v at 1 Hz", i, fastValues[i], slowValues[i])
		}
	}
}

// Idle time decays the averages away, each on its own schedule: after an hour
// the one minute figure is long gone while the fifteen minute one still
// carries a trace, exactly as it would on Linux.
func TestLoadDecaysBackToIdle(t *testing.T) {
	var l LoadAverage
	l.Add(16, 0)
	for i := 0; i < 3600; i++ {
		l.Add(0, 1)
	}
	hour, _ := l.Values()

	if hour[0] > 0.001 {
		t.Errorf("one minute average = %v after an hour idle", hour[0])
	}
	// An hour is four time constants for the fifteen minute window, which
	// leaves under two percent of where it started.
	if hour[2] > 16*0.02 {
		t.Errorf("fifteen minute average = %v after an hour idle, want under %v", hour[2], 16*0.02)
	}
	if !(hour[0] < hour[1] && hour[1] < hour[2]) {
		t.Errorf("windows did not decay in order: %v", hour)
	}

	for i := 0; i < 3*3600; i++ {
		l.Add(0, 1)
	}
	values, _ := l.Values()
	for i, v := range values {
		if v > 0.001 {
			t.Errorf("window %d = %v after four hours idle, want about 0", i, v)
		}
	}
}

// A division by zero upstream must not poison the average for good.
func TestNonFiniteSamplesAreIgnored(t *testing.T) {
	var l LoadAverage
	l.Add(2, 0)
	l.Add(math.NaN(), 1)
	l.Add(math.Inf(1), 1)
	l.Add(-1, 1)

	values, _ := l.Values()
	for i, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v != 2 {
			t.Errorf("window %d = %v, want the last good sample 2", i, v)
		}
	}
}

// Two samples with no time between them carry no new information.
func TestZeroIntervalIsIgnored(t *testing.T) {
	var l LoadAverage
	l.Add(3, 0)
	l.Add(9, 0)

	values, _ := l.Values()
	if values[0] != 3 {
		t.Errorf("one minute average = %v, want the sample to have been ignored", values[0])
	}
}
