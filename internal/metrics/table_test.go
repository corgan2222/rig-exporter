package metrics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleTable() Reading {
	return Table(TopCPU, "", []Row{
		{Label: "firefox.exe", Value: 41.234},
		{Label: "cs2.exe", Value: 12.0},
	})
}

// A table with nothing in it is no reading at all, the same rule the rest of
// the catalogue follows: a measurement that could not be taken is absent, not
// an empty list somebody has to special-case in a template.
func TestAnEmptyTableProducesNoReading(t *testing.T) {
	if r := Table(TopCPU, "", nil); r.Def.ID != "" {
		t.Errorf("an empty ranking produced a reading: %+v", r)
	}
	// Rows without a name are the same case: nothing to show.
	if r := Table(TopCPU, "", []Row{{Label: "  ", Value: 9}, {Label: "", Value: 3}}); r.Def.ID != "" {
		t.Errorf("nameless rows produced a reading: %+v", r)
	}
}

// Rounding happens once, where every other reading is rounded, so the number
// MQTT carries is the number Prometheus carries.
func TestTableValuesAreRoundedLikeEveryOtherReading(t *testing.T) {
	rows := sampleTable().Rows
	if rows[0].Value != 41.2 {
		t.Errorf("value = %v, want it rounded to the definition's one decimal", rows[0].Value)
	}

	t.Cleanup(func() { SetDecimals(true) })
	SetDecimals(false)
	if v := Table(TopCPU, "", []Row{{Label: "a.exe", Value: 41.6}}).Rows[0].Value; v != 42 {
		t.Errorf("value = %v, want the whole number while decimals are off", v)
	}
}

// The state Home Assistant shows is the leader's name, and it is a field of its
// own rather than something a template has to subscript out of the list: a
// template indexing an empty array fails silently and leaves the entity blank.
func TestTheTableCarriesItsLeaderSeparately(t *testing.T) {
	var set Set
	set.Add(sampleTable())

	raw, err := json.Marshal(set.JSON()["top_cpu"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded struct {
		Top  string `json:"top"`
		Apps []struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Top != "firefox.exe" {
		t.Errorf("top = %q, want the leading program", decoded.Top)
	}
	if len(decoded.Apps) != 2 || decoded.Apps[1].Name != "cs2.exe" || decoded.Apps[1].Value != 12 {
		t.Errorf("apps = %+v, want both rows in order", decoded.Apps)
	}
}

// The flat ranks are what makes the history chartable at all.
//
// A card plotting an attribute over time reads attributes[name] out of every
// historical state and expects a number there. Handed a list of objects it has
// nothing to draw, which is a chart that loads forever rather than an error.
func TestTheTableAlsoCarriesEachRankFlat(t *testing.T) {
	var set Set
	set.Add(sampleTable())

	value, ok := set.JSON()["top_cpu"].(map[string]any)
	if !ok {
		t.Fatalf("top_cpu is %T, want an object", set.JSON()["top_cpu"])
	}

	for _, tc := range []struct {
		key  string
		want any
	}{
		{"rank1", 41.2},
		{"rank1_name", "firefox.exe"},
		{"rank2", 12.0},
		{"rank2_name", "cs2.exe"},
	} {
		if got := value[tc.key]; got != tc.want {
			t.Errorf("%s = %v (%T), want %v", tc.key, got, got, tc.want)
		}
	}

	// A shorter list leaves the remaining ranks absent rather than zero: a
	// column of zeroes is a program using nothing, which is not what happened.
	if _, present := value["rank3"]; present {
		t.Error("rank3 exists although only two programs were ranked")
	}
}

// Prometheus gets one series per row. The rank is a label of its own because a
// ranked list raises two questions — "how much did firefox use" and "how much
// did the busiest program use" — and neither is answerable from a series that
// carries only the other.
func TestPrometheusRendersOneSeriesPerRow(t *testing.T) {
	var set Set
	set.Add(sampleTable())
	out := string(set.Prometheus("corganpc2"))

	for _, want := range []string{
		`rig_top_cpu_percent{host="corganpc2",app="firefox.exe",rank="1"} 41.2`,
		`rig_top_cpu_percent{host="corganpc2",app="cs2.exe",rank="2"} 12`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing series:\n%s\ngot:\n%s", want, out)
		}
	}
	// One HELP and TYPE pair for the metric, not one per row.
	if n := strings.Count(out, "# TYPE rig_top_cpu_percent"); n != 1 {
		t.Errorf("TYPE written %d times, want once", n)
	}
}

// The program name is a field, not a tag. A tag is indexed, and every program
// that has ever led the list would stay in that index forever — the textbook
// way to ruin an InfluxDB with cardinality.
func TestInfluxKeepsTheProgramNameOutOfTheTags(t *testing.T) {
	var set Set
	set.Add(sampleTable())
	out := string(set.Influx("rig", "corganpc2", time.Unix(0, 0)))

	if !strings.Contains(out, `top_cpu_1=41.2`) || !strings.Contains(out, `top_cpu_1_app="firefox.exe"`) {
		t.Errorf("fields missing from the line:\n%s", out)
	}

	// Everything before the first space is the measurement and its tags.
	tags := out
	if space := strings.Index(out, " "); space >= 0 {
		tags = out[:space]
	}
	if strings.Contains(tags, "firefox") {
		t.Errorf("the program name was written as a tag: %q", tags)
	}
}

// Quotes in a program name would end the string field early and make the whole
// line unparseable, taking every other measurement in it down too.
func TestInfluxEscapesAProgramNameThatFightsBack(t *testing.T) {
	var set Set
	set.Add(Table(TopCPU, "", []Row{{Label: `say "hi",\ now`, Value: 1}}))
	out := string(set.Influx("rig", "corganpc2", time.Unix(0, 0)))

	if !strings.Contains(out, `top_cpu_1_app="say \"hi\",\\ now"`) {
		t.Errorf("name not escaped:\n%s", out)
	}
}

// One line, because a panel row is one line.
func TestATableReadsAsOneLine(t *testing.T) {
	if got := sampleTable().TableText(); got != "firefox.exe 41.2 % · cs2.exe 12.0 %" {
		t.Errorf("TableText = %q", got)
	}
}
