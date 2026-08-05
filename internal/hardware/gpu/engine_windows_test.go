//go:build windows

package gpu

import (
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The instance names are the whole mapping. Get the parse wrong and every
// engine reading lands on the wrong card, or on none.
func TestInstanceNamesAreReadIntoAdapterAndEngine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		instance string
		want     luidKey
		engine   string
		ok       bool
	}{
		{
			"a utilisation row",
			"pid_20876_luid_0x00000000_0x00017F59_phys_0_eng_5_engtype_VideoEncode",
			luidKey{High: 0, Low: 0x00017F59}, "VideoEncode", true,
		},
		{
			"an engine type carrying a number",
			"pid_23392_luid_0x00000000_0x00016E59_phys_0_eng_7_engtype_OFA_0",
			luidKey{High: 0, Low: 0x00016E59}, "OFA_0", true,
		},
		{
			"a high half that is not nought",
			"pid_1_luid_0x00000001_0x0000ABCD_phys_0_eng_0_engtype_3D",
			luidKey{High: 1, Low: 0x0000ABCD}, "3D", true,
		},
		{"no luid at all", "pid_1_eng_0_engtype_3D", luidKey{}, "", false},
		{"no engine type", "luid_0x00000000_0x00017F59_phys_0", luidKey{Low: 0x00017F59}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, engine, ok := parseEngineInstance(tc.instance)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if key != tc.want {
				t.Errorf("adapter = %+v, want %+v", key, tc.want)
			}
			if engine != tc.engine {
				t.Errorf("engine = %q, want %q", engine, tc.engine)
			}
		})
	}
}

// The memory counter names an adapter without naming a process or an engine.
func TestTheMemoryCounterInstanceNamesAnAdapter(t *testing.T) {
	key, ok := parseLUID("luid_0x00000000_0x00017F59_phys_0")
	if !ok {
		t.Fatal("the adapter was not recognised")
	}
	if key != (luidKey{Low: 0x00017F59}) {
		t.Errorf("adapter = %+v", key)
	}
}

// What DXGI reports and what the counter spells must meet in the middle. This
// is the join the whole feature rests on, and it is compared as numbers so no
// zero padding can break it.
func TestADXGIAdapterMatchesItsCounterInstance(t *testing.T) {
	fromDXGI := keyOf(windows.LUID{HighPart: 0, LowPart: 0x00017F59})
	fromCounter, ok := parseEngineInstanceKey(
		"pid_4242_luid_0x00000000_0x00017F59_phys_0_eng_0_engtype_3D")

	if !ok {
		t.Fatal("the counter instance was not recognised")
	}
	if fromDXGI != fromCounter {
		t.Errorf("DXGI says %+v, the counter says %+v", fromDXGI, fromCounter)
	}
}

func parseEngineInstanceKey(instance string) (luidKey, bool) {
	key, _, ok := parseEngineInstance(instance)
	return key, ok
}

// Summing engines would let a card be more than fully busy; the Task Manager
// takes the busiest one, and so does this.
func TestTheOverallLoadIsTheBusiestEngineNotTheSum(t *testing.T) {
	sampler := &engineSampler{
		last: map[luidKey]adapterLoad{
			{Low: 7}: {
				byEngine: map[string]float64{"3D": 60, "VideoDecode": 55, "Copy": 40},
				busiest:  60,
			},
		},
	}
	// Freshly stamped, so load() serves the cached reading instead of asking
	// PDH for one.
	sampler.at = time.Now()

	set := &metrics.Set{}
	if !sampler.merge(set, map[string]windows.LUID{"0": {LowPart: 7}}) {
		t.Fatal("merge added nothing")
	}

	load, ok := set.Find(metrics.GPULoad.ID, "0")
	if !ok {
		t.Fatal("no overall load was reported")
	}
	if load.Number != 60 {
		t.Errorf("overall load = %v, want the busiest engine 60 and not the sum 155", load.Number)
	}
	assertNumberReading(t, set, metrics.GPUEngine3D, "0", 60)
	assertNumberReading(t, set, metrics.GPUEngineDecode, "0", 55)
	assertNumberReading(t, set, metrics.GPUEngineCopy, "0", 40)
}

// A vendor source measures the same thing closer to the hardware. Where it
// spoke, the counter must stay quiet rather than write a second reading.
func TestTheCounterDoesNotOverwriteAVendorReading(t *testing.T) {
	set := &metrics.Set{}
	set.Add(metrics.Gauge(metrics.GPULoad, "0", 42))

	sampler := &engineSampler{
		last: map[luidKey]adapterLoad{
			{Low: 7}: {byEngine: map[string]float64{"3D": 60}, busiest: 60},
		},
	}
	sampler.at = time.Now()
	sampler.merge(set, map[string]windows.LUID{"0": {LowPart: 7}})

	if got := countReadings(set, metrics.GPULoad.ID, "0"); got != 1 {
		t.Fatalf("GPU load appears %d times, want 1", got)
	}
	load, _ := set.Find(metrics.GPULoad.ID, "0")
	if load.Number != 42 {
		t.Errorf("overall load = %v, want the vendor reading 42", load.Number)
	}
	// The breakdown is its own measurement and is added regardless.
	assertNumberReading(t, set, metrics.GPUEngine3D, "0", 60)
}

// An adapter the counters never mention — a remote or software adapter, or one
// that is simply idle — must not produce zeroes.
func TestAnAdapterWithoutCounterRowsReportsNothing(t *testing.T) {
	sampler := &engineSampler{
		last: map[luidKey]adapterLoad{{Low: 7}: {byEngine: map[string]float64{"3D": 10}}},
	}
	sampler.at = time.Now()

	set := &metrics.Set{}
	sampler.merge(set, map[string]windows.LUID{"0": {LowPart: 999}})

	if len(set.Readings) != 0 {
		t.Errorf("%d readings were made up for an adapter the counters never named", len(set.Readings))
	}
}
