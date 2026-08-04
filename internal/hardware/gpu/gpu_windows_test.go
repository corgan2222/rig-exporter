//go:build windows

package gpu

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

func card(index int, name string) nvmlCard {
	return nvmlCard{Index: index, Name: name}
}

// Two of the same card is the case that broke: names matched against a map, and
// Go randomises map iteration, so both cards could claim instance 0 — one card
// got the other's VRAM and power limit, the other got nothing, and which was
// which changed between runs.
func TestIdenticalCardsKeepSeparateInstances(t *testing.T) {
	cards := map[string]string{"0": "NVIDIA GeForce RTX 4090", "1": "NVIDIA GeForce RTX 4090"}
	nvml := []nvmlCard{card(0, "NVIDIA GeForce RTX 4090"), card(1, "NVIDIA GeForce RTX 4090")}

	// Repeated because the failure it guards against was intermittent.
	for i := 0; i < 50; i++ {
		got := assignInstances(nvml, cards)
		if got[0] != "0" || got[1] != "1" {
			t.Fatalf("assignInstances = %v, want [0 1] every time", got)
		}
	}
}

// A laptop: Afterburner sees the integrated chip first. If NVML spells the
// discrete card differently, it must not be written over the integrated one.
func TestAnUnmatchedCardNeverTakesANamedInstance(t *testing.T) {
	cards := map[string]string{"0": "Intel UHD Graphics 770", "1": "NVIDIA RTX 4070 Laptop GPU"}
	nvml := []nvmlCard{card(0, "NVIDIA GeForce RTX 4070 Laptop GPU")}

	got := assignInstances(nvml, cards)
	if len(got) != 1 {
		t.Fatalf("got %d instances, want 1", len(got))
	}
	if _, collides := cards[got[0]]; collides {
		t.Errorf("instance %q belongs to %q already", got[0], cards[got[0]])
	}
}

func TestNamesMatchRegardlessOfCaseAndPadding(t *testing.T) {
	cards := map[string]string{"0": "  NVIDIA GeForce RTX 2080  "}
	got := assignInstances([]nvmlCard{card(0, "nvidia geforce rtx 2080")}, cards)

	if got[0] != "0" {
		t.Errorf("assignInstances = %v, want the card matched onto instance 0", got)
	}
}

// Without Afterburner there is nothing to join against; the cards keep their own
// indices.
func TestWithoutAfterburnerCardsKeepTheirOwnIndex(t *testing.T) {
	got := assignInstances([]nvmlCard{card(0, "A"), card(1, "B")}, map[string]string{})

	if got[0] != "0" || got[1] != "1" {
		t.Errorf("assignInstances = %v, want [0 1]", got)
	}
}

// Whatever is assigned, no two cards may end up on one instance — that is the
// invariant the readings depend on.
func TestNoInstanceIsEverHandedOutTwice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cards map[string]string
		nvml  []nvmlCard
	}{
		{"all identical", map[string]string{"0": "X", "1": "X", "2": "X"},
			[]nvmlCard{card(0, "X"), card(1, "X"), card(2, "X")}},
		{"more nvml than afterburner", map[string]string{"0": "X"},
			[]nvmlCard{card(0, "X"), card(1, "X"), card(2, "Y")}},
		{"no names match", map[string]string{"0": "A", "1": "B"},
			[]nvmlCard{card(0, "C"), card(1, "D")}},
		{"nvml indices overlap afterburner", map[string]string{"0": "A"},
			[]nvmlCard{card(0, "Z"), card(0, "Z")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := assignInstances(tc.nvml, tc.cards)

			seen := map[string]bool{}
			for i, instance := range got {
				if instance == "" {
					t.Errorf("card %d got no instance", i)
				}
				if seen[instance] {
					t.Errorf("instance %q handed out twice: %v", instance, got)
				}
				seen[instance] = true
			}
		})
	}
}

// Readings must land under the instance that was assigned, not under a name
// that happens to collide.
func TestMergeWritesEachCardToItsOwnInstance(t *testing.T) {
	set := &metrics.Set{}
	cards := map[string]string{"0": "RTX 4090", "1": "RTX 4090"}
	nvml := []nvmlCard{
		{Index: 0, Name: "RTX 4090", VRAMTotalMB: 24576, VRAMUsedMB: 1024, hasVRAM: true},
		{Index: 1, Name: "RTX 4090", VRAMTotalMB: 24576, VRAMUsedMB: 8192, hasVRAM: true},
	}

	mergeFromNVML(set, nvml, cards)

	for instance, want := range map[string]float64{"0": 1024, "1": 8192} {
		reading, ok := set.Find(metrics.GPUVRAMUsed.ID, instance)
		if !ok {
			t.Errorf("no VRAM reading for card %s", instance)
			continue
		}
		if reading.Number != want {
			t.Errorf("card %s VRAM = %v, want %v", instance, reading.Number, want)
		}
	}
}
