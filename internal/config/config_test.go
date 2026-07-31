package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corgan/rig-exporter/internal/i18n"
)

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
func TestPublishIntervalIsAWholeNumberOfReads(t *testing.T) {
	cases := []struct{ poll, publish, wantPublish int }{
		{500, 2000, 2000}, // already a multiple
		{500, 300, 500},   // shorter than a read: raised to one read
		{500, 1700, 2000}, // not a multiple: rounded up
		{1000, 1000, 1000},
	}

	for _, tc := range cases {
		cfg := Defaults()
		cfg.PollIntervalMs = tc.poll
		cfg.PublishIntervalMs = tc.publish
		cfg.Normalize()

		if cfg.PublishIntervalMs != tc.wantPublish {
			t.Errorf("poll %d, publish %d → %d, want %d",
				tc.poll, tc.publish, cfg.PublishIntervalMs, tc.wantPublish)
		}
		if cfg.PublishIntervalMs%cfg.PollIntervalMs != 0 {
			t.Errorf("publish %d is not a multiple of poll %d", cfg.PublishIntervalMs, cfg.PollIntervalMs)
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
	// This is the naming the entities are requested with: sensor.fps_corganpc2.
	if got, want := cfg.ObjectID("fps"), "fps_corganpc2"; got != want {
		t.Errorf("ObjectID = %q, want %q", got, want)
	}
	if got, want := cfg.UniqueID("fps"), "rig_corganpc2_fps"; got != want {
		t.Errorf("UniqueID = %q, want %q", got, want)
	}
	// Instanced entities keep the same shape: sensor.disk_used_percent_c_corganpc2.
	if got, want := cfg.ObjectID("disk_used_percent_c"), "disk_used_percent_c_corganpc2"; got != want {
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
