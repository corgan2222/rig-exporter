//go:build windows

package disk

import (
	"math"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

func numberOf(t *testing.T, set metrics.Set, def metrics.Definition) float64 {
	t.Helper()

	if !set.Has(def.ID) {
		t.Fatalf("%s was not reported", def.ID)
	}
	return set.Number(def.ID)
}

// The overall figures are the sum of the volumes that were actually reported.
func TestTheOverallFiguresSumTheVolumes(t *testing.T) {
	var set metrics.Set
	// Two drives: 1000 GB with 250 GB free, and 500 GB with 250 GB free.
	addOverall(&set, 1500*gb, 500*gb)

	for _, tc := range []struct {
		def  metrics.Definition
		want float64
	}{
		{metrics.DiskOverallCapacity, 1500},
		{metrics.DiskOverallUsed, 1000},
		{metrics.DiskOverallFree, 500},
		{metrics.DiskOverallUsage, 100.0 / 1.5}, // 1000 of 1500
		{metrics.DiskOverallFreePercent, 100.0 / 3},
	} {
		if got := numberOf(t, set, tc.def); math.Abs(got-tc.want) > 0.05 {
			t.Errorf("%s = %v, want %v", tc.def.ID, got, tc.want)
		}
	}
}

// The two percentages have to add up, or one of them is lying.
func TestTheOverallPercentagesAddUp(t *testing.T) {
	for _, free := range []uint64{0, 1, 37, 512, 1023, 1024} {
		var set metrics.Set
		addOverall(&set, 1024*gb, free*gb)

		sum := numberOf(t, set, metrics.DiskOverallUsage) + numberOf(t, set, metrics.DiskOverallFreePercent)
		if math.Abs(sum-100) > 0.05 {
			t.Errorf("free %d GB: usage and free%% add up to %v, want 100", free, sum)
		}
	}
}

// A machine whose volumes all read zero capacity reports nothing rather than a
// division by zero — and rather than a confident "0 %" that means nothing.
func TestNothingIsReportedWithoutCapacity(t *testing.T) {
	var set metrics.Set
	addOverall(&set, 0, 0)

	if len(set.Readings) != 0 {
		t.Errorf("got %d readings for a machine with no capacity, want none", len(set.Readings))
	}
}

// The figures carry no instance: there is one of each however many drives are
// plugged in. A stray instance would turn them into per-device entities.
func TestTheOverallFiguresHaveNoInstance(t *testing.T) {
	var set metrics.Set
	addOverall(&set, 100*gb, 50*gb)

	for _, r := range set.Readings {
		if r.Instance != "" {
			t.Errorf("%s carries instance %q", r.Def.ID, r.Instance)
		}
		if r.Key() != r.Def.ID {
			t.Errorf("key %q differs from the id %q", r.Key(), r.Def.ID)
		}
	}
}
