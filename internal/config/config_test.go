package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corgan/rig-exporter/internal/i18n"
	"github.com/corgan/rig-exporter/internal/metrics"
)

// A release number does not move between commits, so it cannot answer "is this
// the binary that has the fix". The build identifier can.
func TestTheBuildIdentifierAppearsBehindTheVersion(t *testing.T) {
	original := Build
	t.Cleanup(func() { Build = original })

	// A plain `go build` sets nothing, and claiming a build there would be a
	// number nobody could look up.
	Build = ""
	if got := VersionString(); got != Version {
		t.Errorf("VersionString = %q, want the bare version %q", got, Version)
	}

	Build = "247.a1b2c3d"
	if got, want := VersionString(), Version+"+247.a1b2c3d"; got != want {
		t.Errorf("VersionString = %q, want %q", got, want)
	}
	if !strings.HasPrefix(VersionString(), Version) {
		t.Error("the version no longer leads")
	}
}

// An entity id has to read left to right: which program put this here, which
// machine it came from, which piece of hardware, what is measured. Scanning a
// list of a hundred entities, that is the order the questions arrive in — and
// the hostname trailing at the end was no help at all in telling two PCs apart.
func TestTheEntityIdNamesItsOriginFirst(t *testing.T) {
	cfg := Config{NodeID: "corgan_pc3"}

	for _, tc := range []struct {
		reading metrics.Reading
		want    string
	}{
		{metrics.Text(metrics.GPUVendor, "0", "NVIDIA"), "re_corgan_pc3_gpu0_vendor"},
		{metrics.Text(metrics.GPUVendor, "1", "NVIDIA"), "re_corgan_pc3_gpu1_vendor"},
		{metrics.Gauge(metrics.DiskFree, "C:", 1), "re_corgan_pc3_diskc_free"},
		{metrics.Gauge(metrics.NetRx, "Ethernet 2", 1), "re_corgan_pc3_net_ethernet_2_rx"},
		{metrics.Gauge(metrics.FPS, "", 1), "re_corgan_pc3_fps"},
	} {
		key := tc.reading.Key()
		if got := cfg.ObjectID(key); got != tc.want {
			t.Errorf("ObjectID = %q, want %q", got, tc.want)
		}
		// Home Assistant keys identity on the unique id and the displayed id on
		// the object id; having them agree means what a person reads is exactly
		// what the integration knows the entity as.
		if cfg.UniqueID(key) != cfg.ObjectID(key) {
			t.Errorf("UniqueID %q and ObjectID %q disagree", cfg.UniqueID(key), cfg.ObjectID(key))
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"CorganPC2":      "corganpc2",
		"Corgan-PC 2":    "corgan_pc_2",
		"  Gaming Rig  ": "gaming_rig",
		"ÄÖÜ":            "pc", // nothing usable survives
		"":               "pc",
		"__a__":          "a",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeClampsAndFillsIn(t *testing.T) {
	cfg := Config{
		MQTTPort:          0,
		PollIntervalMs:    10,
		PublishIntervalMs: 10,
		IdleTimeoutMs:     900_000,
		WebPort:           -1,
		NodeID:            "Corgan PC2",
		TopicPrefix:       "/rig-exporter/",
	}
	cfg.Normalize()

	if cfg.MQTTPort != 1883 {
		t.Errorf("MQTTPort = %d, want 1883", cfg.MQTTPort)
	}
	if cfg.PollIntervalMs != MinIntervalMs {
		t.Errorf("PollIntervalMs = %d, want %d", cfg.PollIntervalMs, MinIntervalMs)
	}
	if cfg.IdleTimeoutMs != MaxIdleMs {
		t.Errorf("IdleTimeoutMs = %d, want %d", cfg.IdleTimeoutMs, MaxIdleMs)
	}
	if cfg.WebPort != Defaults().WebPort {
		t.Errorf("WebPort = %d, want %d", cfg.WebPort, Defaults().WebPort)
	}
	if cfg.NodeID != "corgan_pc2" {
		t.Errorf("NodeID = %q, want corgan_pc2", cfg.NodeID)
	}
	if cfg.TopicPrefix != "rig-exporter" {
		t.Errorf("TopicPrefix = %q, want rig-exporter (slashes trimmed)", cfg.TopicPrefix)
	}
	if cfg.ClientID == "" {
		t.Error("ClientID was left empty")
	}
}

// Reading is what feeds the tray; publishing is what leaves the machine.
// Publishing faster than reading would only repeat the same numbers, and a
// publish interval that is not a whole number of reads would drift.
// Both publish rates get the same treatment: the loop counts reads rather than
// running a timer of its own, so an interval that is not a whole number of
// reads simply cannot be honoured.
func TestBothPublishIntervalsAreAWholeNumberOfReads(t *testing.T) {
	cases := []struct{ poll, publish, wantPublish int }{
		{500, 2000, 2000}, // already a multiple
		{500, 300, 500},   // shorter than a read: raised to one read
		{500, 1700, 2000}, // not a multiple: rounded up
		{1000, 1000, 1000},
		{500, 10000, 10000}, // the idle default
		{300, 10000, 10200}, // an odd read rate rounds the idle pace up too
	}

	for _, tc := range cases {
		for _, field := range []string{"game", "idle"} {
			cfg := Defaults()
			cfg.PollIntervalMs = tc.poll
			cfg.PublishIntervalMs = tc.publish
			cfg.IdlePublishIntervalMs = tc.publish
			cfg.Normalize()

			got := cfg.PublishIntervalMs
			if field == "idle" {
				got = cfg.IdlePublishIntervalMs
			}
			if got != tc.wantPublish {
				t.Errorf("%s: poll %d, publish %d → %d, want %d",
					field, tc.poll, tc.publish, got, tc.wantPublish)
			}
			if got%cfg.PollIntervalMs != 0 {
				t.Errorf("%s: publish %d is not a multiple of poll %d", field, got, cfg.PollIntervalMs)
			}
		}
	}
}

// The two rates are independent. Nothing stops someone publishing faster when
// idle than in a game, and Normalize must not quietly reorder them.
func TestTheTwoPublishRatesDoNotConstrainEachOther(t *testing.T) {
	cfg := Defaults()
	cfg.PollIntervalMs = 500
	cfg.PublishIntervalMs = 10000
	cfg.IdlePublishIntervalMs = 1000
	cfg.Normalize()

	if cfg.PublishIntervalMs != 10000 || cfg.IdlePublishIntervalMs != 1000 {
		t.Errorf("game %d / idle %d, want them left as given",
			cfg.PublishIntervalMs, cfg.IdlePublishIntervalMs)
	}
}

// An older configuration file predates both settings. The idle rate has to
// arrive at its default, and decimals must not silently switch themselves off
// just because the key is absent.
func TestAConfigurationWithoutTheNewKeysGetsTheDefaults(t *testing.T) {
	path := t.TempDir() + `\config.json`
	if err := os.WriteFile(path, []byte(`{"interval_ms":2000,"poll_interval_ms":500}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IdlePublishIntervalMs != Defaults().IdlePublishIntervalMs {
		t.Errorf("idle interval = %d, want the default %d",
			cfg.IdlePublishIntervalMs, Defaults().IdlePublishIntervalMs)
	}
	if !cfg.Decimals {
		t.Error("decimals switched themselves off for a file that never mentioned them")
	}
	if cfg.SensorSet != SensorSetExtended {
		t.Errorf("sensor set = %q, want the extended default for a file without the key", cfg.SensorSet)
	}
}

// PowerShell's Set-Content -Encoding utf8 and several editors put a byte order
// mark in front of the file. encoding/json refuses it, and the error names a
// character the reader will never find in their editor.
func TestAConfigurationWithAByteOrderMarkStillLoads(t *testing.T) {
	path := t.TempDir() + `\config.json`
	raw := append([]byte{0xEF, 0xBB, 0xBF}, `{"web_port":9999,"node_id":"withbom"}`...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebPort != 9999 || cfg.NodeID != "withbom" {
		t.Errorf("port %d, node %q — the file was not read", cfg.WebPort, cfg.NodeID)
	}
}

// A setting nobody chose has to mean more rather than silently less.
func TestAnUnknownSensorSetFallsBackToExtended(t *testing.T) {
	for _, given := range []string{"", "standard", "extended", "STANDARD", "nonsense"} {
		cfg := Defaults()
		cfg.SensorSet = given
		cfg.Normalize()

		want := SensorSetExtended
		if given == SensorSetStandard {
			want = SensorSetStandard
		}
		if cfg.SensorSet != want {
			t.Errorf("%q normalised to %q, want %q", given, cfg.SensorSet, want)
		}
	}
}

func TestLanguageFallsBackToTheDefault(t *testing.T) {
	for _, value := range []string{"", "klingon", "  EN  "} {
		cfg := Defaults()
		cfg.Language = value
		cfg.Normalize()

		if cfg.Lang() != i18n.Parse(cfg.Language) {
			t.Errorf("Lang() disagrees with the stored value %q", cfg.Language)
		}
	}

	cfg := Defaults()
	cfg.Language = "en"
	cfg.Normalize()
	if cfg.Lang() != i18n.EN {
		t.Errorf("Lang() = %q, want en", cfg.Lang())
	}
}

func TestNormalizeChoosesTLSPortWhenTLSIsOn(t *testing.T) {
	cfg := Config{MQTTTLS: true}
	cfg.Normalize()

	if cfg.MQTTPort != 8883 {
		t.Errorf("MQTTPort = %d, want 8883", cfg.MQTTPort)
	}
}

func TestTopicsAndIdentifiers(t *testing.T) {
	cfg := Defaults()
	cfg.NodeID = "corganpc2"
	cfg.TopicPrefix = "rig-exporter"
	cfg.DiscoveryPrefix = "homeassistant"

	if got, want := cfg.StateTopic(), "rig-exporter/corganpc2/state"; got != want {
		t.Errorf("StateTopic = %q, want %q", got, want)
	}
	if got, want := cfg.AvailabilityTopic(), "rig-exporter/corganpc2/availability"; got != want {
		t.Errorf("AvailabilityTopic = %q, want %q", got, want)
	}
	if got, want := cfg.DiscoveryTopic("sensor", "fps"), "homeassistant/sensor/rig_corganpc2/fps/config"; got != want {
		t.Errorf("DiscoveryTopic = %q, want %q", got, want)
	}
	// This is the naming the entities are requested with:
	// sensor.re_corganpc2_fps.
	if got, want := cfg.ObjectID("fps"), "re_corganpc2_fps"; got != want {
		t.Errorf("ObjectID = %q, want %q", got, want)
	}
	if got, want := cfg.UniqueID("fps"), "re_corganpc2_fps"; got != want {
		t.Errorf("UniqueID = %q, want %q", got, want)
	}
	// Instanced entities keep the same shape:
	// sensor.re_corganpc2_diskc_used_percent.
	if got, want := cfg.ObjectID("diskc_used_percent"), "re_corganpc2_diskc_used_percent"; got != want {
		t.Errorf("ObjectID = %q, want %q", got, want)
	}
}

func TestWantsDiskMatchesRegardlessOfSpelling(t *testing.T) {
	cfg := Config{DiskInclude: []string{"c:", ` D:\ `}}
	cfg.Normalize()

	for _, letter := range []string{"C", "c:", `C:\`, "D"} {
		if !cfg.WantsDisk(letter) {
			t.Errorf("WantsDisk(%q) = false", letter)
		}
	}
	if cfg.WantsDisk("E") {
		t.Error("WantsDisk(E) = true, want false")
	}
}

func TestWantsDiskAcceptsEverythingWhenUnset(t *testing.T) {
	cfg := Defaults()
	for _, letter := range []string{"C", "D", "Z"} {
		if !cfg.WantsDisk(letter) {
			t.Errorf("WantsDisk(%q) = false with no include list", letter)
		}
	}
}

func TestBrokerURLSwitchesSchemeForTLS(t *testing.T) {
	cfg := Config{MQTTHost: "broker.local", MQTTPort: 1883}
	if got, want := cfg.BrokerURL(), "tcp://broker.local:1883"; got != want {
		t.Errorf("BrokerURL = %q, want %q", got, want)
	}

	cfg.MQTTTLS, cfg.MQTTPort = true, 8883
	if got, want := cfg.BrokerURL(), "ssl://broker.local:8883"; got != want {
		t.Errorf("BrokerURL = %q, want %q", got, want)
	}
}

func TestLoadCreatesDefaultsThenReadsThemBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg, created, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !created {
		t.Error("Load did not report that it created the file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file was not written: %v", err)
	}

	cfg.MQTTHost = "broker.example"
	cfg.MQTTPassword = "s3cret"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, created, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if created {
		t.Error("reload reported the file as newly created")
	}
	if reloaded.MQTTHost != "broker.example" || reloaded.MQTTPassword != "s3cret" {
		t.Errorf("reloaded = %+v, want the saved host and password", reloaded)
	}
}

// A file written by hand can omit fields; those must fall back to defaults
// rather than to Go zero values.
func TestLoadFillsMissingFieldsFromDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mqtt_host":"broker.example"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MQTTHost != "broker.example" {
		t.Errorf("MQTTHost = %q", cfg.MQTTHost)
	}
	if cfg.PublishIntervalMs != Defaults().PublishIntervalMs {
		t.Errorf("PublishIntervalMs = %d, want the default %d", cfg.PublishIntervalMs, Defaults().PublishIntervalMs)
	}
	if cfg.DiscoveryPrefix != "homeassistant" {
		t.Errorf("DiscoveryPrefix = %q, want homeassistant", cfg.DiscoveryPrefix)
	}
}

// An upgrade from the previous application name must keep the broker
// settings, and must flag that the old Home Assistant entities need retiring.
func TestMigrateLegacyAdoptsTheOldConfiguration(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AppData", appData)

	legacyDir := filepath.Join(appData, LegacyAppName)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"mqtt_host":"broker.example","mqtt_password":"s3cret","node_id":"corganpc2",` +
		`"topic_prefix":"` + LegacyAppName + `","influx_measurement":"` + LegacyAppName + `"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(appData, AppName, "config.json")
	migrated, err := MigrateLegacy(path)
	if err != nil {
		t.Fatalf("MigrateLegacy: %v", err)
	}
	if !migrated {
		t.Fatal("MigrateLegacy reported nothing to do")
	}

	cfg, created, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if created {
		t.Error("the migrated file was overwritten with defaults")
	}
	if cfg.MQTTHost != "broker.example" || cfg.MQTTPassword != "s3cret" {
		t.Errorf("broker settings were lost: %+v", cfg)
	}
	// Defaults that carried the old name move to the new one; anything the
	// user chose deliberately would have been left alone.
	if cfg.TopicPrefix != AppName {
		t.Errorf("TopicPrefix = %q, want %q", cfg.TopicPrefix, AppName)
	}
	if cfg.InfluxMeasurement != IDPrefix {
		t.Errorf("InfluxMeasurement = %q, want %q", cfg.InfluxMeasurement, IDPrefix)
	}
	if !cfg.LegacyCleanupPending {
		t.Error("the old entities were not flagged for cleanup")
	}
}

func TestMigrateLegacyLeavesAnExistingConfigurationAlone(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AppData", appData)

	path := filepath.Join(appData, AppName, "config.json")
	cfg := Defaults()
	cfg.MQTTHost = "current.example"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	legacyDir := filepath.Join(appData, LegacyAppName)
	os.MkdirAll(legacyDir, 0o755)
	os.WriteFile(filepath.Join(legacyDir, "config.json"),
		[]byte(`{"mqtt_host":"old.example"}`), 0o600)

	migrated, err := MigrateLegacy(path)
	if err != nil {
		t.Fatalf("MigrateLegacy: %v", err)
	}
	if migrated {
		t.Error("MigrateLegacy overwrote the current configuration")
	}

	reloaded, _, _ := Load(path)
	if reloaded.MQTTHost != "current.example" {
		t.Errorf("MQTTHost = %q, want current.example", reloaded.MQTTHost)
	}
}

func TestMigrateLegacyIsANoOpWithoutAnOldConfiguration(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AppData", appData)

	migrated, err := MigrateLegacy(filepath.Join(appData, AppName, "config.json"))
	if err != nil {
		t.Fatalf("MigrateLegacy: %v", err)
	}
	if migrated {
		t.Error("MigrateLegacy claimed to migrate a configuration that does not exist")
	}
}

func TestLoadReportsBrokenJSONAndStillReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{ this is not json`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted broken JSON")
	}
	if cfg.PublishIntervalMs != Defaults().PublishIntervalMs {
		t.Errorf("PublishIntervalMs = %d, want defaults after a parse failure", cfg.PublishIntervalMs)
	}
}
