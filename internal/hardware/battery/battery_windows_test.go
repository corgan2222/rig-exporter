//go:build windows

package battery

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

func TestAPackOfOneIsItself(t *testing.T) {
	pack := aggregate([]winapi.BatteryInfo{{
		DesignedMWh: 57000, FullMWh: 48000, CycleCount: 312,
		Chemistry: "LION", VoltageMV: 11400,
	}})

	if pack.designedMWh != 57000 || pack.fullMWh != 48000 {
		t.Errorf("capacities: got %d of %d mWh", pack.fullMWh, pack.designedMWh)
	}
	if pack.cycles != 312 {
		t.Errorf("cycles: got %d, want 312", pack.cycles)
	}
	if pack.chemistry != "LION" || pack.voltageMV != 11400 {
		t.Errorf("chemistry %q, voltage %d mV", pack.chemistry, pack.voltageMV)
	}
	if pack.relative {
		t.Error("nothing said the capacities were relative")
	}
}

// Two batteries make one pack, and the two halves are folded differently:
// capacity is something the machine has twice, age is not.
func TestTwoBatteriesAddCapacityButNotAge(t *testing.T) {
	pack := aggregate([]winapi.BatteryInfo{
		{DesignedMWh: 24000, FullMWh: 20000, CycleCount: 120, Chemistry: "LION"},
		{DesignedMWh: 36000, FullMWh: 27000, CycleCount: 480, VoltageMV: 11100},
	})

	if pack.designedMWh != 60000 || pack.fullMWh != 47000 {
		t.Errorf("capacities should add up: got %d of %d mWh", pack.fullMWh, pack.designedMWh)
	}
	// The worn cell is the one worth knowing about; averaging would hide it
	// and adding would invent a battery that has been charged six hundred
	// times.
	if pack.cycles != 480 {
		t.Errorf("cycles: got %d, want the higher count 480", pack.cycles)
	}
	if pack.chemistry != "LION" {
		t.Errorf("chemistry: got %q, want the first one that reported", pack.chemistry)
	}
	if pack.voltageMV != 11100 {
		t.Errorf("voltage: got %d, want the first one that reported", pack.voltageMV)
	}
}

// One controller counting in its own units taints the whole pack: the
// capacities can no longer be added up into watt-hours.
func TestOneRelativeControllerMarksThePack(t *testing.T) {
	pack := aggregate([]winapi.BatteryInfo{
		{DesignedMWh: 24000, FullMWh: 20000},
		{DesignedMWh: 100, FullMWh: 88, Relative: true},
	})
	if !pack.relative {
		t.Error("a relative controller has to mark the pack relative")
	}
}

func TestAnEmptyMachineHasAnEmptyPack(t *testing.T) {
	if pack := aggregate(nil); pack != (packInfo{}) {
		t.Errorf("no batteries should fold to nothing, got %+v", pack)
	}
}

// Whatever this machine is, the source has to agree with Windows about
// whether it has a battery — and report nothing at all when it does not.
// The assertion holds on a desktop and on a laptop, which is the only way to
// test this without one of each.
func TestNothingIsReportedWithoutABattery(t *testing.T) {
	state, err := winapi.Battery()
	if err != nil {
		t.Skipf("no power information on this machine: %v", err)
	}

	var set metrics.Set
	if err := New(nil).Collect(&set); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if state.Present && len(set.Readings) == 0 {
		t.Error("this machine has a battery and the source reported nothing")
	}
	if !state.Present && len(set.Readings) != 0 {
		t.Errorf("this machine has no battery, yet %d readings were made up", len(set.Readings))
	}
}
