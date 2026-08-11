//go:build windows

package net

import (
	"math"
	"testing"
)

// What the code does today, before anything is changed.
//
// Written first and on purpose: the trigger cannot be produced here. Ten
// adapters on this machine report speeds between 100 Mbit and 100 Gbit and not
// one of them reports the sentinel, so the only honest evidence is what the
// arithmetic makes of it. This is the number net_link_speed publishes.
func TestTheSentinelSurvivesTheDivisionUnchecked(t *testing.T) {
	published := float64(uint64(math.MaxUint64)) / 1_000_000

	if published < 1e9 {
		t.Fatalf("the sentinel does not survive the division: %v", published)
	}
	t.Logf("an adapter that says its speed is unknown publishes %v Mbit/s", published)

	// And why nothing downstream catches it: both checks look for a value that
	// is too small. This one is not small, it is enormous.
	if !(published > 0) {
		t.Error("the > 0 check in Collect would have caught it")
	}
	if packetRateLimit(published) <= packetRateLimit(0) {
		t.Error("the plausibility limit would not have been raised by it")
	}
	t.Logf("the packet plausibility limit becomes %v/s instead of %v/s",
		packetRateLimit(published), packetRateLimit(0))
}

// Windows uses 0xFFFFFFFFFFFFFFFF for "link speed unknown". A tunnel, some
// virtual switches and a WAN miniport report it, and it must not be read as a
// number.
func TestLinkMbitRejectsTheUnknownSentinel(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  uint64
		want float64
	}{
		{"100 Mbit", 100_000_000, 100},
		{"gigabit", 1_000_000_000, 1000},
		{"100 Gbit", 100_000_000_000, 100_000},
		{"not reported", 0, 0},
		{"speed unknown", math.MaxUint64, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkMbit(tc.raw); got != tc.want {
				t.Errorf("linkMbit(%d) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// Zero here is not the same sin as a published zero. LinkMbit is a field of an
// internal struct, not a measurement, and both readers already treat zero as
// "unknown": the measurement is left out, and the packet filter falls back to
// its assumed ten gigabit. The sentinel has to arrive there as zero.
func TestTheUnknownSentinelFallsBackToTheAssumedLimit(t *testing.T) {
	if packetRateLimit(linkMbit(math.MaxUint64)) != packetRateLimit(0) {
		t.Error("the sentinel disabled the plausibility filter")
	}
}
