package metrics

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/i18n"
)

// everything restores the selection a collector runs under by default, so a
// test that narrowed it cannot leak into the next one.
func everything() map[string]bool { return Resolve(PresetExtended, nil, nil) }

// The rungs have to nest. A slider that took something away by being moved up
// would not be a slider, and a measurement on the minimal rung that the basic
// one does not carry would vanish on the way to more.
func TestTheRungsOfTheLadderNest(t *testing.T) {
	for _, d := range All {
		minimal := PresetContains(PresetMinimal, d.ID)
		basic := PresetContains(PresetBasic, d.ID)

		if minimal && !basic {
			t.Errorf("%q is minimal but not basic — moving the slider up would lose it", d.ID)
		}
		if basic && !PresetContains(PresetExtended, d.ID) {
			t.Errorf("%q is basic but not extended", d.ID)
		}
	}

	sizes := []int{len(PresetIDs(PresetMinimal)), len(PresetIDs(PresetBasic)), len(PresetIDs(PresetExtended))}
	if !(sizes[0] < sizes[1] && sizes[1] < sizes[2]) {
		t.Errorf("the rungs hold %v measurements — each has to carry more than the one below", sizes)
	}
	if sizes[2] != len(All) {
		t.Errorf("the extended rung holds %d of %d measurements; it has to be the whole catalogue",
			sizes[2], len(All))
	}
}

// Every id on a rung has to name a measurement that exists, or the rung is
// quietly smaller than it reads.
func TestEveryRungNamesRealMeasurements(t *testing.T) {
	known := map[string]bool{}
	for _, d := range All {
		known[d.ID] = true
	}
	for name, set := range map[string]map[string]bool{"minimal": minimalSet, "basic": basicSet} {
		for id := range set {
			if !known[id] {
				t.Errorf("%q is on the %s rung but not in the catalogue", id, name)
			}
		}
	}
}

// A rung drops what it does not carry at collection time, not somewhere
// downstream: a measurement that was never taken cannot reach an export.
func TestARungDropsWhatItDoesNotCarry(t *testing.T) {
	t.Cleanup(func() { SetSelection(everything()) })

	add := func() Set {
		var set Set
		set.Add(
			Gauge(FPS, "", 143),                          // every rung
			Gauge(CPULoad, "", 37),                       // every rung
			Gauge(CPUClock, "", 4200),                    // extended only
			Text(Resolution, "", "4K"),                   // extended only
			Gauge(GPUTemperature, "0", 45),               // basic, with an instance
			Gauge(GPUDedicatedMemoryTotal, "0", 128),     // basic
			Text(GPUDriverVersion, "0", "31.0.101.5590"), // extended only
			Gauge(GPUSharedMemoryTotal, "0", 8192),       // extended only
		)
		return set
	}

	SetSelection(everything())
	if got := len(add().Readings); got != 8 {
		t.Fatalf("extended: got %d readings, want all 8", got)
	}

	SetSelection(Resolve(PresetBasic, nil, nil))
	set := add()
	if got := len(set.Readings); got != 4 {
		t.Errorf("basic: got %d readings, want 4", got)
	}
	for _, r := range set.Readings {
		if !PresetContains(PresetBasic, r.Def.ID) {
			t.Errorf("%q survived although the basic rung does not carry it", r.Def.ID)
		}
	}
	for _, gone := range []Definition{CPUClock, GPUDriverVersion, GPUSharedMemoryTotal} {
		if set.Has(gone.ID) {
			t.Errorf("%q survived the basic rung", gone.ID)
		}
	}
	for _, kept := range []Definition{FPS, GPUDedicatedMemoryTotal} {
		if !set.Has(kept.ID) {
			t.Errorf("%q was dropped although the basic rung carries it", kept.ID)
		}
	}
}

// The whole point of storing a rung rather than a list: what the user picked by
// hand survives, and everything else follows the rung.
func TestHandPickedMeasurementsOverrideTheRung(t *testing.T) {
	t.Cleanup(func() { SetSelection(everything()) })

	selected := Resolve(PresetMinimal,
		[]string{CPUClock.ID},              // wanted, although minimal drops it
		[]string{FPS.ID, "retired_in_1_4"}) // unwanted, although minimal carries it

	if !selected[CPUClock.ID] {
		t.Error("a measurement added by hand did not survive")
	}
	if selected[FPS.ID] {
		t.Error("a measurement removed by hand survived")
	}
	if !selected[CPULoad.ID] {
		t.Error("the rung stopped carrying what it carries")
	}

	// An id from a version that no longer has it is an old configuration, not
	// a broken one.
	if selected["retired_in_1_4"] {
		t.Error("an identifier that names nothing was selected")
	}
	if _, ok := Resolve(PresetMinimal, []string{"never_existed"}, nil)["never_existed"]; ok {
		t.Error("an identifier that names nothing was added")
	}
}

// Both lists naming the same measurement is a configuration somebody edited by
// hand. Taking it out is the safer reading of the two.
func TestRemovalWinsOverAddition(t *testing.T) {
	selected := Resolve(PresetBasic, []string{CPUClock.ID}, []string{CPUClock.ID})
	if selected[CPUClock.ID] {
		t.Error("a measurement both added and removed survived")
	}
}

// An unknown rung must report more rather than silently less: a configuration
// nobody can read is not a reason to stop measuring.
func TestAnUnknownRungFallsBackToEverything(t *testing.T) {
	selected := Resolve(Preset("whatever-this-is"), nil, nil)
	if len(selected) != len(All) {
		t.Errorf("an unknown rung selected %d of %d measurements", len(selected), len(All))
	}
}

// Before anything has been applied there is no selection, and a collector must
// then report everything rather than nothing.
func TestNothingSelectedYetMeansEverything(t *testing.T) {
	t.Cleanup(func() { SetSelection(everything()) })

	selection.Store(nil)
	if !Selected(CPUClock.ID) {
		t.Error("a collector running before the configuration was applied reported nothing")
	}
	if SelectedCount() != len(All) {
		t.Errorf("SelectedCount = %d, want the whole catalogue", SelectedCount())
	}
}

// The listing on the settings page is generated, so it has to carry the
// translated name rather than an identifier twice.
func TestTheRungListingsCarryNamesAndFollowTheCatalogueOrder(t *testing.T) {
	basic := PresetDefinitions(PresetBasic)
	if len(basic) == 0 {
		t.Fatal("the basic rung is empty")
	}
	for _, d := range basic {
		if d.Name.In(i18n.DE) == "" || d.Name.In(i18n.EN) == "" {
			t.Errorf("%q has no name in one of the languages", d.ID)
		}
	}

	// Catalogue order, not map order: two renders of the page must not shuffle.
	position := map[string]int{}
	for i, d := range All {
		position[d.ID] = i
	}
	for _, preset := range Presets {
		listing := PresetDefinitions(preset)
		for i := 1; i < len(listing); i++ {
			if position[listing[i-1].ID] > position[listing[i].ID] {
				t.Errorf("%s: %q comes after %q in the catalogue but before it in the listing",
					preset, listing[i-1].ID, listing[i].ID)
			}
		}
	}
}

// The measurements the program itself depends on have to survive the smallest
// rung: the publish rate is chosen from the frame rate, and the dashboard tiles
// read the load out of the same set the exporters do.
func TestEvenTheSmallestRungKeepsWhatTheProgramItselfNeeds(t *testing.T) {
	for _, d := range []Definition{FPS, GameRunning, CPULoad, RAMLoad, Game} {
		if !PresetContains(PresetMinimal, d.ID) {
			t.Errorf("%q is not on the minimal rung, but the program relies on it", d.ID)
		}
	}
}
