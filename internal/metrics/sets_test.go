package metrics

import (
	"testing"

	"github.com/corgan/rig-exporter/internal/i18n"
)

// The two sets together have to be the catalogue, exactly. A measurement in
// neither would be unreachable however the user sets the option, and one in
// both would make the listing on the settings page a lie.
func TestTheTwoSensorSetsPartitionTheCatalogue(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range All {
		seen[d.ID] = true
	}

	for id := range standardSet {
		if !seen[id] {
			t.Errorf("%q is in the standard set but not in the catalogue", id)
		}
	}

	standard, extended := StandardDefinitions(), ExtendedDefinitions()
	if got := len(standard) + len(extended); got != len(All) {
		t.Errorf("the two sets hold %d definitions, the catalogue has %d", got, len(All))
	}
	if len(standard) != len(standardSet) {
		t.Errorf("standard set lists %d ids but yields %d definitions — a duplicate id?",
			len(standardSet), len(standard))
	}

	for _, d := range extended {
		if standardSet[d.ID] {
			t.Errorf("%q is in both sets", d.ID)
		}
	}
}

// Switching to the standard set has to drop the extended measurements at
// collection time, not merely hide them somewhere downstream.
func TestTheStandardSetDropsWhatItDoesNotContain(t *testing.T) {
	t.Cleanup(func() { SetStandardOnly(false) })

	add := func() Set {
		var set Set
		set.Add(
			Gauge(FPS, "", 143),            // standard
			Gauge(CPULoad, "", 37),         // standard
			Gauge(CPUClock, "", 4200),      // extended
			Text(Resolution, "", "4K"),     // extended
			Gauge(GPUTemperature, "0", 45), // standard, with an instance
		)
		return set
	}

	if got := len(add().Readings); got != 5 {
		t.Fatalf("extended: got %d readings, want all 5", got)
	}

	SetStandardOnly(true)
	set := add()
	if got := len(set.Readings); got != 3 {
		t.Errorf("standard: got %d readings, want 3", got)
	}
	for _, r := range set.Readings {
		if !r.Def.InStandardSet() {
			t.Errorf("%q survived although it is not in the standard set", r.Def.ID)
		}
	}
	if !set.Has(FPS.ID) {
		t.Error("fps was dropped although it is in the standard set")
	}
	if set.Has(CPUClock.ID) {
		t.Error("cpu_clock survived the standard set")
	}
}

// The listing on the settings page is generated, so it has to carry the
// translated name rather than an identifier twice.
func TestTheSetListingsCarryNamesAndFollowTheCatalogueOrder(t *testing.T) {
	standard := StandardDefinitions()
	if len(standard) == 0 {
		t.Fatal("the standard set is empty")
	}
	for _, d := range standard {
		if d.Name.In(i18n.DE) == "" || d.Name.In(i18n.EN) == "" {
			t.Errorf("%q has no name in one of the languages", d.ID)
		}
	}

	// Catalogue order, not map order: two renders of the page must not shuffle.
	position := map[string]int{}
	for i, d := range All {
		position[d.ID] = i
	}
	for _, set := range [][]Definition{standard, ExtendedDefinitions()} {
		for i := 1; i < len(set); i++ {
			if position[set[i-1].ID] > position[set[i].ID] {
				t.Errorf("%q comes after %q in the catalogue but before it in the listing",
					set[i-1].ID, set[i].ID)
			}
		}
	}
}

// The measurements the program itself depends on have to survive the smaller
// set: the publish rate is chosen from the frame rate, and the dashboard tiles
// read the load out of the same set the exporters do.
func TestTheStandardSetKeepsWhatTheProgramItselfNeeds(t *testing.T) {
	for _, d := range []Definition{FPS, GameRunning, CPULoad, RAMLoad, Game} {
		if !d.InStandardSet() {
			t.Errorf("%q is not in the standard set, but the program relies on it", d.ID)
		}
	}
}
