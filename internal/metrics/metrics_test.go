package metrics

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/corgan/rig-exporter/internal/i18n"
)

// sampleSet is a reading set with singletons and two instanced groups, which
// is what every renderer has to cope with.
func sampleSet() Set {
	var set Set
	set.Add(
		Gauge(FPS, "", 143.2),
		Gauge(Frametime, "", 6.98),
		Text(Game, "", "Cyberpunk2077.exe"),
		Bool(GameRunning, "", true),
		Text(Resolution, "", "2560x1440"),
		Gauge(RefreshRate, "", 165),
		Gauge(CPULoad, "", 24.5),
		Gauge(RAMLoad, "", 51.3),
		Bool(RTSSUp, "", true),

		Text(GPUName, "0", "NVIDIA GeForce RTX 4090"),
		Gauge(GPULoad, "0", 97.4),
		Gauge(GPUTemperature, "0", 61.5),

		Text(DiskMedia, "C:", "NVMe"),
		Gauge(DiskUsedPercent, "C:", 61.2),
		Gauge(DiskUsedPercent, "D:", 12.7),

		Gauge(NetRx, "Ethernet", 12.4),
		Gauge(PingRTT, "", 3.2),
	)
	return set
}

func TestCatalogueIsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	proms := map[string]string{}

	for _, d := range All {
		if d.ID == "" || d.Name.Empty() || d.Prom == "" || d.Help == "" {
			t.Errorf("definition %+v is missing an id, name, prom name or help text", d)
		}
		if d.Kind == KindText && d.PromLabel == "" {
			t.Errorf("text definition %q has no Prometheus label to carry its value", d.ID)
		}
		if ids[d.ID] {
			t.Errorf("duplicate definition id %q", d.ID)
		}
		ids[d.ID] = true

		if other, dup := proms[d.Prom]; dup {
			t.Errorf("%s and %s share the Prometheus name %s", d.ID, other, d.Prom)
		}
		proms[d.Prom] = d.ID
	}
}

// A half-translated catalogue would show German in an English interface, so
// every definition carries both languages.
func TestEveryDefinitionIsTranslated(t *testing.T) {
	for _, d := range All {
		if d.Name.DE == "" {
			t.Errorf("%s has no German name", d.ID)
		}
		if d.Name.EN == "" {
			t.Errorf("%s has no English name", d.ID)
		}
	}
	for _, g := range Groups {
		if g.Label(i18n.DE) == string(g) || g.Label(i18n.EN) == string(g) {
			t.Errorf("group %s falls back to its key in one language", g)
		}
	}
}

// The language may change what a person reads and nothing else: an entity id
// that moved with the language would break every dashboard on a switch.
func TestLanguageDoesNotAffectIdentifiers(t *testing.T) {
	reading := Gauge(GPUTemperature, "0", 61)

	if reading.Key() != "gpu_temperature_0" {
		t.Errorf("Key = %q", reading.Key())
	}
	if reading.DisplayName(i18n.DE) == reading.DisplayName(i18n.EN) {
		t.Error("the German and English names are identical, so nothing was translated")
	}
	for _, lang := range []i18n.Lang{i18n.DE, i18n.EN} {
		if !strings.Contains(reading.DisplayName(lang), "0") {
			t.Errorf("%s name %q lost the instance", lang, reading.DisplayName(lang))
		}
	}

	var set Set
	set.Add(reading)
	if _, ok := set.JSON()["gpu_temperature_0"]; !ok {
		t.Error("the JSON key changed with the language")
	}
}

// A definition that repeats per device must name the dimension, otherwise the
// Prometheus series of two disks would be indistinguishable.
func TestInstancedDefinitionsNameTheirDimension(t *testing.T) {
	// A handful of definitions describe the group rather than a device in it,
	// and are therefore singletons despite their group.
	groupWide := map[string]bool{
		GPUSource.ID: true,
	}

	for _, d := range All {
		if groupWide[d.ID] {
			continue
		}
		switch d.Group {
		case GroupDisk, GroupGPU:
			if d.InstanceLabel == "" {
				t.Errorf("%s repeats per device but has no instance label", d.ID)
			}
		}
	}
}

func TestKeyAppendsTheInstance(t *testing.T) {
	cases := map[string]Reading{
		"fps":                 Gauge(FPS, "", 1),
		"gpu_temperature_0":   Gauge(GPUTemperature, "0", 1),
		"disk_used_percent_c": Gauge(DiskUsedPercent, "C:", 1),
		"net_rx_ethernet_2":   Gauge(NetRx, "Ethernet 2", 1),
	}
	for want, reading := range cases {
		if got := reading.Key(); got != want {
			t.Errorf("Key() = %q, want %q", got, want)
		}
	}
}

// A plain string sort puts core 10 between core 1 and core 2, which makes a
// list of sixteen cores unreadable.
func TestNumericInstancesSortAsNumbers(t *testing.T) {
	var set Set
	for _, core := range []string{"10", "2", "0", "1", "11"} {
		set.Add(Gauge(CPUCoreLoad, core, 1))
	}

	got := set.GroupInstances(GroupCPU)
	want := []string{"0", "1", "2", "10", "11"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("GroupInstances = %v, want %v", got, want)
		}
	}

	// Entities() sorts too, and has to agree.
	var order []string
	for _, r := range set.Entities() {
		order = append(order, r.Instance)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("Entities order = %v, want %v", order, want)
			break
		}
	}
}

// Names still sort as names, and numbers come before them.
func TestNonNumericInstancesKeepTextOrder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"C:", "D:", true},
		{"D:", "C:", false},
		{"0", "C:", true}, // numbers first
		{"C:", "0", false},
		{"2", "10", true},
		{"Ethernet", "WLAN", true},
	}
	for _, tc := range cases {
		if got := LessInstance(tc.a, tc.b); got != tc.want {
			t.Errorf("LessInstance(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"C:":         "c",
		"Ethernet 2": "ethernet_2",
		"WLAN":       "wlan",
		"0":          "0",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// An adapter named in a script this filter keeps nothing of used to slug to the
// empty string. That collapsed the key to "net_type_" and, worse, made every
// such adapter share a single Home Assistant entity.
func TestSlugNeverReturnsNothing(t *testing.T) {
	for _, in := range []string{"  ", "イーサネット", "以太网", "!!!", "…"} {
		got := Slug(in)
		if got == "" {
			t.Errorf("Slug(%q) is empty", in)
			continue
		}
		if got != Slug(in) {
			t.Errorf("Slug(%q) is not stable", in)
		}
		for _, r := range got {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
				t.Errorf("Slug(%q) = %q contains %q, which is not safe in a topic", in, got, r)
			}
		}
	}

	// Distinct names must stay distinct, which is the whole point.
	if Slug("イーサネット") == Slug("以太网") {
		t.Error("two different adapter names collapsed onto one instance")
	}
}

func TestJSONKeysEveryReading(t *testing.T) {
	document := sampleSet().JSON()

	if document["fps"] != 143.2 {
		t.Errorf("fps = %v", document["fps"])
	}
	if document["gpu_temperature_0"] != 61.5 {
		t.Errorf("gpu_temperature_0 = %v", document["gpu_temperature_0"])
	}
	if document["disk_used_percent_c"] != 61.2 || document["disk_used_percent_d"] != 12.7 {
		t.Errorf("two disks collapsed into one key: %v", document)
	}
	if document["rtss"] != PayloadOn {
		t.Errorf("rtss = %v, want %q", document["rtss"], PayloadOn)
	}
}

func TestJSONIsSerialisable(t *testing.T) {
	raw, err := json.Marshal(sampleSet().JSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"fps":143.2`) {
		t.Errorf("document: %s", raw)
	}
}

func TestPrometheusExposition(t *testing.T) {
	out := string(sampleSet().Prometheus("corganpc2"))

	for _, line := range []string{
		"# TYPE rig_fps gauge",
		`rig_fps{host="corganpc2"} 143.2`,
		`rig_game_info{host="corganpc2",game="Cyberpunk2077.exe"} 1`,
		`rig_gpu_percent{host="corganpc2",gpu="0"} 97.4`,
		`rig_gpu_temperature_celsius{host="corganpc2",gpu="0"} 61.5`,
		`rig_disk_used_percent{host="corganpc2",disk="C:"} 61.2`,
		`rig_disk_used_percent{host="corganpc2",disk="D:"} 12.7`,
		`rig_net_receive_megabits_per_second{host="corganpc2",nic="Ethernet"} 12.4`,
		`rig_rtss_up{host="corganpc2"} 1`,
	} {
		if !strings.Contains(out, line) {
			t.Errorf("exposition is missing:\n%s\n\ngot:\n%s", line, out)
		}
	}
}

// Two disks share one metric name, so the HELP and TYPE lines must appear
// exactly once or Prometheus rejects the scrape.
func TestPrometheusEmitsOneHeaderPerMetric(t *testing.T) {
	out := string(sampleSet().Prometheus("pc"))

	if got := strings.Count(out, "# TYPE rig_disk_used_percent gauge"); got != 1 {
		t.Errorf("got %d TYPE lines for rig_disk_used_percent, want 1:\n%s", got, out)
	}
}

func TestPrometheusEscapesLabelValues(t *testing.T) {
	var set Set
	set.Add(Text(Game, "", `we"ird\game.exe`))

	if out := string(set.Prometheus("pc")); !strings.Contains(out, `game="we\"ird\\game.exe"`) {
		t.Errorf("label was not escaped:\n%s", out)
	}
}

func TestPrometheusSkipsEmptyInfoMetrics(t *testing.T) {
	var set Set
	set.Add(Text(Game, "", ""))

	if out := string(set.Prometheus("pc")); strings.Contains(out, `game=""`) {
		t.Errorf("an empty info metric was emitted:\n%s", out)
	}
}

func TestPrometheusOutputIsStable(t *testing.T) {
	set := sampleSet()
	if string(set.Prometheus("pc")) != string(set.Prometheus("pc")) {
		t.Error("two renderings of the same set differ")
	}
}

func TestInfluxSplitsGroupsIntoMeasurements(t *testing.T) {
	at := time.Unix(1700000000, 123)
	lines := strings.Split(strings.TrimSpace(string(sampleSet().Influx("rig", "corganpc2", at))), "\n")

	byMeasurement := map[string]string{}
	for _, line := range lines {
		byMeasurement[strings.SplitN(line, ",", 2)[0]] = line
	}

	for _, want := range []string{"rig", "rig_gpu", "rig_disk", "rig_net"} {
		if _, ok := byMeasurement[want]; !ok {
			t.Errorf("no %s point:\n%s", want, strings.Join(lines, "\n"))
		}
	}

	core := byMeasurement["rig"]
	if !strings.Contains(core, "game=Cyberpunk2077.exe") || !strings.Contains(core, "fps=143.2") {
		t.Errorf("core point = %s", core)
	}
	if !strings.HasSuffix(core, " 1700000000000000123") {
		t.Errorf("core point has the wrong timestamp: %s", core)
	}
}

// Each disk is its own point, tagged with the drive, so a query can group by
// it rather than parse a field name.
func TestInfluxGivesEachInstanceItsOwnPoint(t *testing.T) {
	out := string(sampleSet().Influx("rig", "pc", time.Unix(0, 0)))

	var diskLines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "rig_disk,") {
			diskLines = append(diskLines, line)
		}
	}
	if len(diskLines) != 2 {
		t.Fatalf("got %d disk points, want 2:\n%s", len(diskLines), out)
	}

	for _, want := range []struct{ tag, field string }{
		{"disk=C:", "used_percent=61.2"},
		{"disk=D:", "used_percent=12.7"},
	} {
		found := false
		for _, line := range diskLines {
			if strings.Contains(line, want.tag) && strings.Contains(line, want.field) {
				found = true
			}
		}
		if !found {
			t.Errorf("no point with %s and %s:\n%s", want.tag, want.field, out)
		}
	}

	// The drive letter identifies the point; the media type rides along as a
	// second tag on the same point rather than becoming its own series.
	if !strings.Contains(out, "media=NVMe") {
		t.Errorf("the media tag was dropped:\n%s", out)
	}
}

func TestInfluxEscapesTagValues(t *testing.T) {
	var set Set
	set.Add(
		Text(Game, "", "Red Dead, Redemption.exe"),
		Gauge(FPS, "", 60),
	)

	out := string(set.Influx("rig", "corgan pc", time.Unix(0, 0)))
	if !strings.Contains(out, `host=corgan\ pc`) {
		t.Errorf("host tag was not escaped:\n%s", out)
	}
	if !strings.Contains(out, `game=Red\ Dead\,\ Redemption.exe`) {
		t.Errorf("game tag was not escaped:\n%s", out)
	}
}

// A point consisting only of tags is not valid line protocol.
func TestInfluxSkipsPointsWithoutFields(t *testing.T) {
	var set Set
	set.Add(Text(GPUName, "0", "RTX 4090"))

	if out := string(set.Influx("rig", "pc", time.Unix(0, 0))); strings.Contains(out, "rig_gpu") {
		t.Errorf("a fieldless point was emitted:\n%s", out)
	}
}

func TestInfluxFallsBackToTheDefaultMeasurement(t *testing.T) {
	out := string(sampleSet().Influx("", "pc", time.Unix(0, 0)))

	if !strings.HasPrefix(out, DefaultMeasurement+",") {
		t.Errorf("measurement = %q, want %q", strings.SplitN(out, ",", 2)[0], DefaultMeasurement)
	}
}

func TestEntitiesLeaveOutInternalReadings(t *testing.T) {
	var set Set
	set.Add(
		Gauge(FPS, "", 60),
		Gauge(GamePID, "", 1234), // NoEntity
	)

	for _, r := range set.Entities() {
		if r.Def.ID == GamePID.ID {
			t.Error("an internal reading was offered as an entity")
		}
	}
	if _, ok := set.Find(GamePID.ID, ""); !ok {
		t.Error("the internal reading was dropped from the set entirely")
	}
}

func TestGroupInstancesListsWhatWasFound(t *testing.T) {
	got := sampleSet().GroupInstances(GroupDisk)

	if len(got) != 2 || got[0] != "C:" || got[1] != "D:" {
		t.Errorf("GroupInstances(disk) = %v, want [C: D:]", got)
	}
	if sampleSet().HasGroup(GroupCPU) {
		t.Error("HasGroup(cpu) = true with no CPU readings")
	}
}

func TestRoundDropsNonFiniteValues(t *testing.T) {
	if got := Round(1.0/3.0, 2); got != 0.33 {
		t.Errorf("Round = %v, want 0.33", got)
	}
	// A division by zero upstream must not poison every output format.
	if got := Gauge(FPS, "", math.Inf(1)).Number; got != 0 {
		t.Errorf("Gauge(+Inf) = %v, want 0", got)
	}
	if got := Gauge(FPS, "", math.NaN()).Number; got != 0 {
		t.Errorf("Gauge(NaN) = %v, want 0", got)
	}
}
