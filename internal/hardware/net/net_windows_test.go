//go:build windows

package net

import (
	"errors"
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

// Losing the default route must not widen the adapter list.
//
// Measured on the development machine: ten interfaces pass the "up, not
// loopback, has IPv4" filter — one physical card, six Hyper-V switches,
// Tailscale, ZeroTier and an Npcap loopback. At ten catalogued readings each
// that is 100 entities instead of 10, and 90 of them are retained discovery
// messages that survive the cable being plugged back in, survive Home Assistant
// forgetting them, and come back on every restart. For a five second outage.
func TestTheLastAdapterIsKeptWhenTheDefaultRouteGoes(t *testing.T) {
	ethernet := Adapter{Name: "Ethernet 2", LUID: 1}
	virtual := []Adapter{
		{Name: "vEthernet (WLAN)", LUID: 2},
		{Name: "Tailscale", LUID: 3},
	}
	all := append([]Adapter{ethernet}, virtual...)

	s := &Source{}
	route := uint64(1)
	s.defaultRoute = func() (uint64, error) { return route, nil }

	// A normal tick picks the one carrying the default route.
	got, err := s.chooseAdapters(all)
	if err != nil || len(got) != 1 || got[0].LUID != 1 {
		t.Fatalf("with a route: %+v (err %v), want only Ethernet 2", got, err)
	}

	// Cable out: no route to be found.
	route = 0
	s.defaultRoute = func() (uint64, error) { return 0, errors.New("network unreachable") }

	got, err = s.chooseAdapters(all)
	if err != nil {
		t.Fatalf("without a route: %v", err)
	}
	if len(got) != 1 || got[0].LUID != 1 {
		t.Errorf("without a route: %+v, want the last known adapter only", got)
	}
}

// A cold start with no route at all has nothing to fall back on. Reporting
// every virtual adapter would create exactly the entities this avoids, so it
// reports none and says why — the source error path already carries that to the
// status page.
func TestNoAdapterIsReportedBeforeARouteWasEverSeen(t *testing.T) {
	s := &Source{}
	s.defaultRoute = func() (uint64, error) { return 0, errors.New("network unreachable") }

	got, err := s.chooseAdapters([]Adapter{
		{Name: "vEthernet (WLAN)", LUID: 2},
		{Name: "Tailscale", LUID: 3},
	})

	if len(got) != 0 {
		t.Errorf("adapters = %+v, want none before a route was ever seen", got)
	}
	if err == nil {
		t.Error("no reason was given for reporting nothing")
	}
}

// The remembered adapter has to be dropped when it really is gone, or a card
// that was removed would be reported forever.
func TestTheRememberedAdapterIsDroppedOnceItIsGone(t *testing.T) {
	s := &Source{}
	s.defaultRoute = func() (uint64, error) { return 1, nil }
	if _, err := s.chooseAdapters([]Adapter{{Name: "Ethernet 2", LUID: 1}}); err != nil {
		t.Fatal(err)
	}

	s.defaultRoute = func() (uint64, error) { return 0, errors.New("network unreachable") }
	got, err := s.chooseAdapters([]Adapter{{Name: "Tailscale", LUID: 3}})

	if len(got) != 0 {
		t.Errorf("adapters = %+v, want none: the remembered card is not there", got)
	}
	if err == nil {
		t.Error("no reason was given for reporting nothing")
	}
}
