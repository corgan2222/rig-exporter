package metrics

import (
	"encoding/json"
	"math"
	"sort"
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

	if reading.Key() != "gpu0_temperature" {
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
	if _, ok := set.JSON()["gpu0_temperature"]; !ok {
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
		// The overall figures sum every volume, so there is one of each however
		// many drives are plugged in.
		DiskOverallCapacity.ID:    true,
		DiskOverallUsed.ID:        true,
		DiskOverallFree.ID:        true,
		DiskOverallUsage.ID:       true,
		DiskOverallFreePercent.ID: true,
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

// The instance comes straight after the thing being enumerated, so a list of
// keys groups by device instead of scattering one drive across the alphabet.
func TestTheInstanceFollowsTheThingBeingEnumerated(t *testing.T) {
	cases := map[string]Reading{
		"fps": Gauge(FPS, "", 1),

		// The hardware reads as one word: gpu0, diskc.
		"gpu0_temperature":   Gauge(GPUTemperature, "0", 1),
		"gpu1_vram_used":     Gauge(GPUVRAMUsed, "1", 1),
		"diskc_used_percent": Gauge(DiskUsedPercent, "C:", 1),
		"diskd_free":         Gauge(DiskFree, "D:", 1),

		// Unless the instance is several words itself, where netethernet_2
		// would be unreadable.
		"net_ethernet_2_rx": Gauge(NetRx, "Ethernet 2", 1),

		// A processor core is enumerated by the noun cpu_core, which already
		// reads correctly — cpu_5_core would not. Same for a memory module.
		"cpu_core_5":           Gauge(CPUCoreLoad, "5", 1),
		"ram_module_bank_0_a1": Text(RAMModule, "BANK 0 A1", "x"),

		// Filed under the network group but not carrying its noun, so the
		// instance stays where appending puts it.
		"ping_rtt": Gauge(PingRTT, "", 1),
	}
	for want, reading := range cases {
		if got := reading.Key(); got != want {
			t.Errorf("Key() = %q, want %q", got, want)
		}
	}
}

// Every name a reading has ever been published under has to stay derivable. A
// retained discovery message outlives this program and survives deleting the
// entity by hand — it simply comes back when Home Assistant restarts.
func TestEveryPreviousIdentifierIsStillDerivable(t *testing.T) {
	for _, tc := range []struct {
		reading Reading
		current string
		legacy  []string
	}{
		{Gauge(GPUTemperature, "0", 1), "gpu0_temperature",
			[]string{"gpu_temperature_0", "gpu_0_temperature"}},
		{Gauge(DiskUsedPercent, "C:", 1), "diskc_used_percent",
			[]string{"disk_used_percent_c", "disk_c_used_percent"}},
		// The network form never changed, so there is only the original.
		{Gauge(NetRx, "Ethernet 2", 1), "net_ethernet_2_rx",
			[]string{"net_rx_ethernet_2"}},
	} {
		if got := tc.reading.Key(); got != tc.current {
			t.Errorf("Key() = %q, want %q", got, tc.current)
		}
		got := tc.reading.LegacyKeys()
		if len(got) != len(tc.legacy) {
			t.Errorf("%s: LegacyKeys() = %v, want %v", tc.current, got, tc.legacy)
			continue
		}
		for i := range tc.legacy {
			if got[i] != tc.legacy[i] {
				t.Errorf("%s: LegacyKeys()[%d] = %q, want %q", tc.current, i, got[i], tc.legacy[i])
			}
		}
	}

	// The current name must never appear among the old ones: retiring it would
	// delete the entity that was just announced.
	for _, reading := range []Reading{
		Gauge(FPS, "", 1),
		Gauge(CPUCoreLoad, "5", 1),
		Gauge(PingRTT, "", 1),
		Gauge(GPUTemperature, "0", 1),
		Gauge(NetRx, "Ethernet 2", 1),
	} {
		for _, legacy := range reading.LegacyKeys() {
			if legacy == reading.Key() {
				t.Errorf("%s would retire its own current name %q", reading.Def.ID, legacy)
			}
		}
	}
}

// Home Assistant sorts a device page alphabetically by entity name, so the
// hardware has to come first — otherwise one graphics card's readings are
// scattered among every other measurement that starts the same way.
func TestTheHardwareLeadsTheDisplayName(t *testing.T) {
	for _, tc := range []struct {
		reading Reading
		want    string
	}{
		{Gauge(GPUTemperature, "0", 61), "GPU 0 Temperatur"},
		{Gauge(GPUFan, "0", 30), "GPU 0 Lüfter"},
		{Gauge(GPUTemperature, "1", 55), "GPU 1 Temperatur"},
		{Gauge(DiskFree, "C:", 1), "Laufwerk C: Frei"},
		{Gauge(DiskBusy, "C:", 1), "Laufwerk C: Auslastung"},
		{Gauge(CPUCoreLoad, "5", 1), "Kern 5 Last"},

		// The adapter already names itself; "NIC Ethernet 2" would only add noise.
		{Gauge(NetRx, "Ethernet 2", 1), "Ethernet 2 Empfangen"},

		// Nothing to group, nothing to prefix.
		{Gauge(FPS, "", 1), "FPS"},
	} {
		if got := tc.reading.DisplayName(i18n.DE); got != tc.want {
			t.Errorf("DisplayName = %q, want %q", got, tc.want)
		}
	}

	// Sorting one card's readings must keep them adjacent.
	names := []string{
		Gauge(GPUTemperature, "0", 1).DisplayName(i18n.DE),
		Gauge(GPUTemperature, "1", 1).DisplayName(i18n.DE),
		Gauge(GPUFan, "0", 1).DisplayName(i18n.DE),
		Gauge(GPUFan, "1", 1).DisplayName(i18n.DE),
	}
	sort.Strings(names)
	if !strings.HasPrefix(names[0], "GPU 0") || !strings.HasPrefix(names[1], "GPU 0") {
		t.Errorf("sorted names do not group by card: %v", names)
	}
}

// The hardware must not be named twice. "GPU 0 GPU-Temperatur" and "CPU
// CPU-Modell" are what happens when the prefix and the measurement name both
// claim it — which is exactly what the catalogue used to do.
func TestTheDisplayNameNeverRepeatsTheHardware(t *testing.T) {
	for _, def := range All {
		if def.NoEntity {
			continue
		}
		for _, lang := range []i18n.Lang{i18n.DE, i18n.EN} {
			var prefix string
			if def.InstanceLabel != "" {
				prefix = deviceLabels[def.InstanceLabel].In(lang)
			} else {
				prefix = groupPrefixes[def.PanelGroup()].In(lang)
			}
			if prefix == "" {
				continue
			}
			if strings.HasPrefix(def.Name.In(lang), prefix) {
				t.Errorf("%s (%s) is called %q, which repeats the prefix %q",
					def.ID, lang, def.Name.In(lang), prefix)
			}
		}
	}
}

// Readings that exist only once still need grouping: a device page listing
// "Takt" and "Belegt" among ninety others tells nobody which part they describe.
func TestSingletonsAreGroupedByTheirPanel(t *testing.T) {
	for _, tc := range []struct {
		reading Reading
		want    string
	}{
		{Gauge(CPULoad, "", 24.5), "CPU Auslastung"},
		{Gauge(CPUClock, "", 4200), "CPU Takt"},
		{Gauge(CPUClockBase, "", 3394), "CPU Basistakt"},
		{Gauge(CPUClockMax, "", 4300), "CPU Takt max. (beobachtet)"},
		{Text(CPUModel, "", "x"), "CPU Modell"},
		{Gauge(RAMLoad, "", 51), "RAM Belegung"},
		{Gauge(RAMUsed, "", 1), "RAM Belegt"},
		{Gauge(RAMFree, "", 1), "RAM Frei"},
		{Gauge(RAMTotal, "", 1), "RAM Gesamt"},
		{Text(RAMType, "", "DDR4"), "RAM Typ"},
		{Text(GPUSource, "", "x"), "GPU Datenquelle"},

		// The headline values carry no prefix; they are what the tool is for.
		{Gauge(FPS, "", 143), "FPS"},
		{Text(Game, "", "x"), "Spiel"},
		{Text(Resolution, "", "x"), "Auflösung"},
	} {
		if got := tc.reading.DisplayName(i18n.DE); got != tc.want {
			t.Errorf("DisplayName = %q, want %q", got, tc.want)
		}
	}
}

// The name a person reads translates, including its hardware prefix. The
// identifier never does — that split is what lets a German installation read
// German without moving a single dashboard reference.
func TestNamesTranslateAndIdentifiersDoNot(t *testing.T) {
	for _, tc := range []struct {
		reading Reading
		de, en  string
	}{
		{Gauge(GPUTemperature, "0", 61), "GPU 0 Temperatur", "GPU 0 Temperature"},
		{Gauge(DiskFree, "C:", 1), "Laufwerk C: Frei", "Drive C: Free"},
		{Gauge(CPUClockBase, "", 3394), "CPU Basistakt", "CPU Base clock"},
		{Gauge(RAMUsed, "", 1), "RAM Belegt", "RAM Used"},
	} {
		if got := tc.reading.DisplayName(i18n.DE); got != tc.de {
			t.Errorf("German name = %q, want %q", got, tc.de)
		}
		if got := tc.reading.DisplayName(i18n.EN); got != tc.en {
			t.Errorf("English name = %q, want %q", got, tc.en)
		}
	}

	// The prefix translates too — a German page reading "Drive C: Frei" would
	// be half-translated, which is worse than either language alone.
	if got := (Gauge(DiskFree, "C:", 1)).DisplayName(i18n.DE); !strings.HasPrefix(got, "Laufwerk") {
		t.Errorf("the hardware prefix did not translate: %q", got)
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

// Two measurements shown side by side under one heading have to be tellable
// apart by their labels alone. "Frei 68882 MB" above "Frei 52.6 %" is one row
// too many named Frei — the unit is not the label, and in Home Assistant the
// entity name is all a person sees.
func TestNoTwoMeasurementsOnAPanelShareAName(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.DE, i18n.EN} {
		seen := map[Group]map[string]string{}
		for _, def := range All {
			panel := def.PanelGroup()
			if seen[panel] == nil {
				seen[panel] = map[string]string{}
			}
			name := def.Name.In(lang)
			if other, taken := seen[panel][name]; taken {
				t.Errorf("%s panel, %s: %q and %q are both called %q",
					panel, lang, other, def.ID, name)
				continue
			}
			seen[panel][name] = def.ID
		}
	}
}

// Where a number came from is the interface's business and nobody else's.
//
// The whole point of tracking it is to answer "what do I lose if I close this
// program" on screen. The moment it reaches an export, a dashboard can start
// depending on which helpers happen to run on a given machine — which is the
// opposite of the guarantee that the same measurement looks identical from
// every source.
func TestTheSupplierNeverReachesAnExport(t *testing.T) {
	set := sampleSet()
	for i := range set.Readings {
		set.Readings[i].Origin = "MSI Afterburner"
	}

	document, err := json.Marshal(set.JSON())
	if err != nil {
		t.Fatal(err)
	}

	for name, rendered := range map[string]string{
		"JSON":       string(document),
		"Prometheus": string(set.Prometheus("corganpc2")),
		"InfluxDB":   string(set.Influx("rig", "corganpc2", time.Unix(0, 0))),
	} {
		for _, leak := range []string{"MSI Afterburner", "Afterburner", "origin", "Origin"} {
			if strings.Contains(rendered, leak) {
				t.Errorf("%s output contains %q", name, leak)
			}
		}
	}
}

// Readings are stamped as they are added, and a source that switches supplier
// mid-collection stamps the rest differently — which is how one group can
// honestly credit two programs.
func TestReadingsAreStampedWithWhateverWasSupplying(t *testing.T) {
	var set Set

	set.Origin = "Windows"
	set.Add(Gauge(CPULoad, "", 24.5))
	set.Origin = "MSI Afterburner"
	set.Add(Gauge(CPUTemperature, "", 61))
	set.Origin = "PawnIO"
	set.Add(Gauge(CPUPower, "", 95))

	// An explicit stamp on the reading itself wins, for the single value that
	// does not come from the same place as its neighbours.
	odd := Gauge(GPUFan, "0", 30)
	odd.Origin = "NVIDIA NVML"
	set.Add(odd)

	want := map[string]string{
		CPULoad.ID:        "Windows",
		CPUTemperature.ID: "MSI Afterburner",
		CPUPower.ID:       "PawnIO",
		GPUFan.ID:         "NVIDIA NVML",
	}
	for _, r := range set.Readings {
		if got := r.Origin; got != want[r.Def.ID] {
			t.Errorf("%s came from %q, want %q", r.Def.ID, got, want[r.Def.ID])
		}
	}

	origins := set.Origins()
	if len(origins) != 4 {
		t.Fatalf("got %d suppliers, want 4", len(origins))
	}

	// Sorted, so the panel does not reshuffle between polls.
	for i := 1; i < len(origins); i++ {
		if origins[i-1].Name > origins[i].Name {
			t.Errorf("suppliers are not in a stable order: %v", origins)
			break
		}
	}
}

// A supplier reporting one value per graphics card must not list that value
// four times.
func TestASupplierNamesEachMeasurementOnce(t *testing.T) {
	var set Set
	set.Origin = "NVIDIA NVML"
	set.Add(
		Gauge(GPUTemperature, "0", 61),
		Gauge(GPUTemperature, "1", 55),
		Gauge(GPUFan, "0", 30),
	)

	origins := set.Origins()
	if len(origins) != 1 {
		t.Fatalf("got %d suppliers, want 1", len(origins))
	}
	if names := origins[0].Names(i18n.DE); len(names) != 2 {
		t.Errorf("names = %v, want two distinct measurements", names)
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
	if document["gpu0_temperature"] != 61.5 {
		t.Errorf("gpu0_temperature = %v", document["gpu0_temperature"])
	}
	if document["diskc_used_percent"] != 61.2 || document["diskd_used_percent"] != 12.7 {
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

// Map iteration order is deliberately random in Go, so a renderer that walks a
// map without sorting produces a different document every time — which turns
// every scrape into a diff and hides real changes.
func TestPrometheusOutputIsStable(t *testing.T) {
	set := sampleSet()

	// Two separate calls, kept in variables: comparing the calls inline reads
	// as comparing something with itself, and a reader cannot tell that the
	// point is exactly that the two must agree.
	first, second := string(set.Prometheus("pc")), string(set.Prometheus("pc"))
	if first != second {
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
