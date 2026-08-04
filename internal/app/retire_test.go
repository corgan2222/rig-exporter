//go:build windows

package app

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

func snapshotOf(readings ...metrics.Reading) collector.Snapshot {
	var snap collector.Snapshot
	snap.Add(readings...)
	return snap
}

func sets(old, next string) (config.Config, config.Config) {
	a, b := config.Defaults(), config.Defaults()
	a.SensorSet, b.SensorSet = old, next
	return a, b
}

// Narrowing the set is a statement about what should exist, so what falls out
// of it is retired — with the real instances, which no list of definitions
// could supply.
func TestNarrowingTheSetRetiresWhatItDrops(t *testing.T) {
	snap := snapshotOf(
		metrics.Gauge(metrics.FPS, "", 143),            // standard, stays
		metrics.Gauge(metrics.CPULoad, "", 37),         // standard, stays
		metrics.Gauge(metrics.CPUClock, "", 4200),      // extended, goes
		metrics.Gauge(metrics.GPUCoreClock, "0", 1515), // extended, goes
		metrics.Gauge(metrics.GPUCoreClock, "1", 2700), // extended, goes
		metrics.Text(metrics.Resolution, "", "4K"),     // extended, goes
		metrics.Gauge(metrics.GPUTemperature, "0", 45), // standard, stays
	)

	old, next := sets(config.SensorSetExtended, config.SensorSetStandard)
	dropped := droppedByStandardSet(old, next, snap)

	if len(dropped) != 4 {
		t.Fatalf("got %d entities to retire, want 4", len(dropped))
	}

	keys := map[string]bool{}
	for _, r := range dropped {
		keys[r.Key()] = true
		if r.Def.InStandardSet() {
			t.Errorf("%q was retired although it is in the standard set", r.Key())
		}
	}
	// Both cards, separately — an instance is part of the identity.
	for _, want := range []string{"cpu_clock", "gpu0_core_clock", "gpu1_core_clock", "resolution"} {
		if !keys[want] {
			t.Errorf("%q was not retired", want)
		}
	}
}

// Every other configuration change leaves entities alone. Widening only adds
// measurements, and a group being switched off or hardware falling silent must
// not cost anybody their history — those come back.
func TestNothingElseRetiresAnything(t *testing.T) {
	snap := snapshotOf(
		metrics.Gauge(metrics.CPUClock, "", 4200),
		metrics.Gauge(metrics.FPS, "", 143),
	)

	cases := []struct {
		name     string
		old, new string
	}{
		{"widening", config.SensorSetStandard, config.SensorSetExtended},
		{"unchanged extended", config.SensorSetExtended, config.SensorSetExtended},
		{"unchanged standard", config.SensorSetStandard, config.SensorSetStandard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old, next := sets(tc.old, tc.new)
			if dropped := droppedByStandardSet(old, next, snap); len(dropped) != 0 {
				t.Errorf("retired %d entities, want none", len(dropped))
			}
		})
	}
}

// A machine that reported nothing has nothing to retire, and must not send a
// burst of empty payloads for entities that never existed.
func TestAnEmptyReadingRetiresNothing(t *testing.T) {
	old, next := sets(config.SensorSetExtended, config.SensorSetStandard)
	if dropped := droppedByStandardSet(old, next, snapshotOf()); len(dropped) != 0 {
		t.Errorf("retired %d entities from an empty reading", len(dropped))
	}
}
