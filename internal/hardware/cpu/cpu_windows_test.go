//go:build windows

package cpu

import (
	"errors"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// answering makes the power information call return a fixed answer.
func answering(t *testing.T, current, max float64, err error) {
	t.Helper()

	restore := clocksFn
	t.Cleanup(func() { clocksFn = restore })
	clocksFn = func() (float64, float64, error) { return current, max, err }
}

// CallNtPowerInformation can return success and still report zero on every
// core. Hypervisors do it regularly, and virtual machines are a supported
// target — so this is not a hypothetical.
//
// A zero is not a clock. Nothing may reach the set, because a published 0 MHz
// is indistinguishable from a measurement afterwards: a chart over weeks shows
// a clean zero line where it should show a gap. cpu_clock_base was already
// guarded; the other two were not, and the asymmetry between three readings
// from one source was the tell.
func TestAZeroClockIsNotPublished(t *testing.T) {
	answering(t, 0, 0, nil)

	// The zero value is the case: performance is nil, baseMHz and peakMHz are
	// zero, which is exactly the path a machine takes on its first reading.
	var source Source
	var set metrics.Set
	source.collectClock(&set)

	for _, def := range []metrics.Definition{
		metrics.CPUClock, metrics.CPUClockMax, metrics.CPUClockBase,
	} {
		if set.Has(def.ID) {
			t.Errorf("%s published as %v; a zero is not a measurement",
				def.ID, set.Number(def.ID))
		}
	}
}

// The ordinary answer still goes out, and the peak starts where the first
// reading is rather than at zero.
func TestARealClockIsPublished(t *testing.T) {
	answering(t, 3593, 3600, nil)

	var source Source
	var set metrics.Set
	source.collectClock(&set)

	if got := set.Number(metrics.CPUClock.ID); got != 3593 {
		t.Errorf("cpu_clock = %v, want 3593", got)
	}
	if got := set.Number(metrics.CPUClockMax.ID); got != 3593 {
		t.Errorf("cpu_clock_max = %v, want 3593", got)
	}
	if got := set.Number(metrics.CPUClockBase.ID); got != 3600 {
		t.Errorf("cpu_clock_base = %v, want 3600", got)
	}
}

// A failed call was already left out, and has to stay left out.
func TestAFailedCallStillPublishesNothing(t *testing.T) {
	answering(t, 0, 0, errors.New("CallNtPowerInformation returned 0xc0000001"))

	var source Source
	var set metrics.Set
	source.collectClock(&set)

	if set.Has(metrics.CPUClock.ID) {
		t.Errorf("cpu_clock = %v after the call failed", set.Number(metrics.CPUClock.ID))
	}
}

// The peak is a high-water mark and must not fall back when a later reading
// comes in low — nor be dragged to zero by a reading that is left out.
func TestThePeakKeepsTheHighestRealReading(t *testing.T) {
	var source Source

	answering(t, 4900, 3600, nil)
	var boosting metrics.Set
	source.collectClock(&boosting)

	answering(t, 800, 3600, nil)
	var idling metrics.Set
	source.collectClock(&idling)

	if got := idling.Number(metrics.CPUClockMax.ID); got != 4900 {
		t.Errorf("cpu_clock_max = %v after idling, want the 4900 seen while boosting", got)
	}

	answering(t, 0, 0, nil)
	var blind metrics.Set
	source.collectClock(&blind)

	if blind.Has(metrics.CPUClockMax.ID) {
		t.Errorf("cpu_clock_max = %v; a reading that was left out still published the peak",
			blind.Number(metrics.CPUClockMax.ID))
	}
}
