package metrics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleDetails() Reading {
	return Details(GameDetails, "",
		Detail{Name: DetailPlatform, Value: "steam"},
		Detail{Name: DetailTitle, Value: "The Ascent"},
		Detail{Name: DetailAppID, Value: "979690"},
	)
}

// The rule the whole catalogue follows, applied to the half of a reading:
// what is not known is left out rather than published as an empty string. A
// game the store has never heard of keeps its title and has no app id, and an
// app id nobody knows is not the same as an app id that is empty — the second
// would send Home Assistant looking for artwork that cannot exist.
func TestAnUnknownDetailIsAbsentRatherThanEmpty(t *testing.T) {
	reading := Details(GameDetails, "",
		Detail{Name: DetailPlatform, Value: "gog"},
		Detail{Name: DetailTitle, Value: "Cyberpunk 2077"},
		Detail{Name: DetailAppID, Value: ""},
	)

	value, ok := reading.Value().(map[string]any)
	if !ok {
		t.Fatalf("Value() = %T, want an object", reading.Value())
	}
	if _, present := value[DetailAppID]; present {
		t.Errorf("the app id is present as %v, want it left out", value[DetailAppID])
	}
	if value[DetailTitle] != "Cyberpunk 2077" || value[DetailPlatform] != "gog" {
		t.Errorf("value = %v, want the two halves that are known", value)
	}
}

// Nothing known at all is no reading, not an empty object: a template that has
// to tell "{}" from "not there" is a template somebody gets wrong.
func TestDetailsWithNothingKnownProduceNoReading(t *testing.T) {
	if r := Details(GameDetails, ""); r.Def.ID != "" {
		t.Errorf("an empty detail set produced a reading: %+v", r)
	}
	blank := Details(GameDetails, "",
		Detail{Name: DetailPlatform, Value: "  "},
		Detail{Name: "", Value: "steam"},
	)
	if blank.Def.ID != "" {
		t.Errorf("blank details produced a reading: %+v", blank)
	}
}

// The JSON key is the one the game entity's attributes template names, and its
// value is an object. Home Assistant reads attributes out of an object; a
// string there sets no attributes at all.
func TestDetailsAreOneObjectUnderTheirOwnKey(t *testing.T) {
	var set Set
	set.Add(sampleDetails())

	raw, err := json.Marshal(set.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	details, ok := document["game_details"].(map[string]any)
	if !ok {
		t.Fatalf("game_details = %T, want an object", document["game_details"])
	}
	for name, want := range map[string]string{
		DetailPlatform: "steam",
		DetailTitle:    "The Ascent",
		DetailAppID:    "979690",
	} {
		if details[name] != want {
			t.Errorf("%s = %v, want %q", name, details[name], want)
		}
	}
}

// The game measurement is untouched by all of this. Its state is the executable
// and its key is "game" — an established entity that dashboards, automations
// and history are built on.
func TestTheGameStateStaysTheExecutable(t *testing.T) {
	var set Set
	set.Add(
		Text(Game, "", "Cyberpunk2077.exe"),
		sampleDetails(),
	)

	document := set.JSON()
	if document["game"] != "Cyberpunk2077.exe" {
		t.Errorf("game = %v, want the executable RTSS reports", document["game"])
	}
	if _, ok := document["game"].(string); !ok {
		t.Errorf("game = %T, want a plain string", document["game"])
	}
}

// Prometheus has a convention for facts that are not numbers, and this is it:
// one info metric, a label per fact, the sample always 1.
func TestDetailsBecomeOneInfoMetricWithALabelEach(t *testing.T) {
	var set Set
	set.Add(sampleDetails())

	text := string(set.Prometheus("corganpc2"))
	want := `rig_game_details_info{host="corganpc2",platform="steam",title="The Ascent",app_id="979690"} 1`
	if !strings.Contains(text, want) {
		t.Errorf("Prometheus output does not contain\n  %s\ngot:\n%s", want, text)
	}
}

// A field per detail in InfluxDB, and a string field rather than a tag: a tag
// is the identity of the series, so a title that changes when another game
// starts would split it and take the point's other fields with it.
func TestDetailsBecomeStringFieldsInInflux(t *testing.T) {
	var set Set
	set.Add(sampleDetails())

	line := string(set.Influx("rig", "corganpc2", time.Unix(0, 0)))
	for _, want := range []string{
		`game_details_platform="steam"`,
		`game_details_title="The Ascent"`,
		`game_details_app_id="979690"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("line protocol does not contain %s\ngot: %s", want, line)
		}
	}
}

// A details reading exists to be another entity's attributes. Both halves of
// that have to hold, or the details are published to a topic nobody reads and
// the entity advertises attributes that never arrive.
func TestEveryDetailsMeasurementIsSomeEntitysAttributes(t *testing.T) {
	named := map[string]int{}
	for _, d := range All {
		if d.AttributesFrom == "" {
			continue
		}
		named[d.AttributesFrom]++
	}

	byID := map[string]Definition{}
	for _, d := range All {
		byID[d.ID] = d
	}

	for _, d := range All {
		if d.Kind != KindDetails {
			continue
		}
		if !d.NoEntity {
			t.Errorf("%s is a details measurement and would become an entity whose state is an object", d.ID)
		}
		if named[d.ID] == 0 {
			t.Errorf("%s is published and no entity carries it as attributes", d.ID)
		}
	}

	for _, d := range All {
		if d.AttributesFrom == "" {
			continue
		}
		companion, exists := byID[d.AttributesFrom]
		if !exists {
			t.Errorf("%s names %q as its attributes, and no such measurement exists", d.ID, d.AttributesFrom)
			continue
		}
		if companion.Kind != KindDetails {
			t.Errorf("%s names %q as its attributes, which is not a details measurement", d.ID, d.AttributesFrom)
		}
		if companion.InstanceLabel != "" {
			t.Errorf("%s names %q, which repeats per device — the key would need an instance",
				d.ID, d.AttributesFrom)
		}
	}
}
