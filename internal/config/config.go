// Package config holds the on-disk settings for rig-exporter.
//
// The file lives next to the log in %APPDATA%\rig-exporter and is written by both
// the first run (defaults) and the settings web UI.
package config

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/corgan/rig-exporter/internal/i18n"
)

const (
	// AppName is used for the executable, the config directory, the default
	// topic prefix and the autostart registry value.
	AppName = "rig-exporter"
	// IDPrefix goes into identifiers that must not contain a hyphen or that
	// read better short: unique ids, the device identifier and the InfluxDB
	// measurement.
	IDPrefix = "rig"
	// Version is reported to Home Assistant as the device software version.
	Version = "1.1.0"

	// LegacyAppName is the previous name. Its configuration is migrated on
	// first start and its retained discovery topics are cleaned up.
	LegacyAppName = "fps2mqtt"

	// RTSSDownloadURL is shown whenever RivaTuner Statistics Server is missing.
	RTSSDownloadURL = "https://www.guru3d.com/download/rtss-rivatuner-statistics-server-download/"
	// AfterburnerURL is shown when no graphics telemetry source is available.
	AfterburnerURL = "https://www.msi.com/Landing/afterburner/graphics-cards"
)

// Config is the complete, user-editable configuration.
//
// The export targets are independent switches rather than one mode, because
// running MQTT for Home Assistant and Prometheus for Grafana at the same time
// is a normal thing to want.
type Config struct {
	MQTTEnabled     bool   `json:"mqtt_enabled"`
	MQTTHost        string `json:"mqtt_host"`
	MQTTPort        int    `json:"mqtt_port"`
	MQTTUsername    string `json:"mqtt_username"`
	MQTTPassword    string `json:"mqtt_password"`
	MQTTTLS         bool   `json:"mqtt_tls"`
	MQTTTLSInsecure bool   `json:"mqtt_tls_insecure"`
	ClientID        string `json:"client_id"`

	DeviceName      string `json:"device_name"`
	NodeID          string `json:"node_id"`
	TopicPrefix     string `json:"topic_prefix"`
	DiscoveryPrefix string `json:"discovery_prefix"`

	// Data server: one HTTP listener that Home Assistant, Prometheus or
	// Telegraf can pull from. Each format is switched on separately.
	DataServerEnabled bool   `json:"data_server_enabled"`
	DataBindAddress   string `json:"data_bind_address"`
	DataPort          int    `json:"data_port"`
	// DataToken, when set, is required as a bearer token or ?token= parameter
	// on every data endpoint.
	DataToken         string `json:"data_token"`
	JSONEnabled       bool   `json:"json_enabled"`
	PrometheusEnabled bool   `json:"prometheus_enabled"`
	InfluxPullEnabled bool   `json:"influx_pull_enabled"`

	// InfluxDB push, written to the v2 write API.
	InfluxPushEnabled bool   `json:"influx_push_enabled"`
	InfluxURL         string `json:"influx_url"`
	InfluxOrg         string `json:"influx_org"`
	InfluxBucket      string `json:"influx_bucket"`
	InfluxToken       string `json:"influx_token"`
	InfluxMeasurement string `json:"influx_measurement"`

	// Sensor groups. Each is collected only when switched on, and drops out
	// silently when the machine cannot supply it.
	GPUEnabled       bool `json:"gpu_enabled"`
	CPUDetailEnabled bool `json:"cpu_detail_enabled"`
	CPUPerCore       bool `json:"cpu_per_core"`
	RAMDetailEnabled bool `json:"ram_detail_enabled"`
	// PawnIOEnabled opts in to the kernel-backed sensor source.
	//
	// Off by default and deliberately so: PawnIO's device is reachable only by
	// administrators, so switching this on means running rig-exporter elevated.
	// That is a decision about the machine, not a setting, and nobody should
	// arrive at it by accident.
	PawnIOEnabled bool     `json:"pawnio_enabled"`
	DiskEnabled   bool     `json:"disk_enabled"`
	DiskInclude   []string `json:"disk_include"`
	NetEnabled    bool     `json:"net_enabled"`
	// NetAllAdapters reports every connected interface instead of only the
	// one carrying the default route.
	NetAllAdapters bool `json:"net_all_adapters"`

	// Latency probe, part of the network group.
	PingEnabled    bool   `json:"ping_enabled"`
	PingTarget     string `json:"ping_target"`
	PingCount      int    `json:"ping_count"`
	PingIntervalMs int    `json:"ping_interval_ms"`

	// LegacyCleanupPending is set once when a configuration is migrated from
	// the previous application name, and cleared after the old retained
	// discovery topics have been emptied.
	LegacyCleanupPending bool `json:"legacy_cleanup_pending"`

	// PollIntervalMs is how often the hardware is read. It sets how smoothly
	// the tray and the settings page move.
	PollIntervalMs int `json:"poll_interval_ms"`
	// PublishIntervalMs is how often a reading is handed to the export
	// targets. It is never shorter than the poll interval. The JSON name is
	// the historical one, so an older configuration keeps working.
	PublishIntervalMs int `json:"interval_ms"`
	IdleTimeoutMs     int `json:"idle_timeout_ms"`

	Language  string `json:"language"`
	WebPort   int    `json:"web_port"`
	Autostart bool   `json:"autostart"`
	Debug     bool   `json:"debug"`
}

// Bounds applied by Normalize. Exported so the web UI can label its inputs.
const (
	MinIntervalMs = 250
	MaxIntervalMs = 300_000
	MinIdleMs     = 500
	MaxIdleMs     = 60_000
)

// Defaults returns a configuration that works against a local broker as soon
// as credentials are filled in. Host name drives every identifier so two PCs
// never collide in Home Assistant.
func Defaults() Config {
	node := Slug(hostname())
	return Config{
		MQTTEnabled:     true,
		MQTTHost:        "homeassistant.local",
		MQTTPort:        1883,
		ClientID:        AppName + "-" + node,
		DeviceName:      hostname(),
		NodeID:          node,
		TopicPrefix:     AppName,
		DiscoveryPrefix: "homeassistant",

		// The data server is off by default: it listens on all interfaces, so
		// turning it on should be a deliberate choice.
		DataServerEnabled: false,
		DataBindAddress:   "0.0.0.0",
		DataPort:          9838,
		JSONEnabled:       true,
		PrometheusEnabled: true,
		InfluxPullEnabled: false,

		InfluxMeasurement: IDPrefix,

		// Everything the machine can supply is on by default; a group that
		// has no source simply produces nothing.
		GPUEnabled:       true,
		CPUDetailEnabled: true,
		CPUPerCore:       false, // one entity per thread is a lot for a 16-core CPU
		RAMDetailEnabled: true,
		PawnIOEnabled:    false, // needs elevation; the user has to choose it
		DiskEnabled:      true,
		NetEnabled:       true,
		PingEnabled:      true,
		PingTarget:       "", // empty means the default gateway
		PingCount:        3,
		PingIntervalMs:   15000,

		// Read four times as often as we publish: the tray and the settings
		// page stay lively without putting four times the traffic on the
		// broker.
		PollIntervalMs:    500,
		PublishIntervalMs: 2000,
		IdleTimeoutMs:     3000,

		// Follows Windows, and falls back to the default for a language the
		// catalogue does not have yet.
		Language: string(i18n.Parse(osLanguage())),
		WebPort:  8787,
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "pc"
	}
	return h
}

// Slug reduces a string to the character set that is safe in MQTT topics and
// Home Assistant entity ids.
func Slug(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-':
			if !lastUnderscore && b.Len() > 0 {
				b.WriteRune('_')
				lastUnderscore = true
			}
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "pc"
	}
	return out
}

// Dir is the directory holding config.json and the log file.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(base, AppName), nil
}

// Path is the full path of config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LogPath is the full path of the rotating log file.
func LogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AppName+".log"), nil
}

// MigrateLegacy copies a configuration written by the previous application
// name into path, if path does not exist yet and the old one does.
//
// It reports whether anything was migrated, which the caller turns into the
// one-off cleanup of the retained Home Assistant discovery topics the old name
// left behind.
func MigrateLegacy(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil // the current configuration wins
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return false, nil
	}
	legacy := filepath.Join(base, LegacyAppName, "config.json")

	raw, err := os.ReadFile(legacy)
	if err != nil {
		return false, nil // nothing to migrate is the normal case
	}

	cfg := Defaults()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return false, fmt.Errorf("parse %s: %w", legacy, err)
	}

	// The old default measurement and topic prefix carried the old name; leave
	// anything the user chose deliberately alone.
	if cfg.TopicPrefix == LegacyAppName {
		cfg.TopicPrefix = AppName
	}
	if cfg.InfluxMeasurement == LegacyAppName {
		cfg.InfluxMeasurement = IDPrefix
	}
	if cfg.ClientID == LegacyAppName+"-"+cfg.NodeID {
		cfg.ClientID = AppName + "-" + cfg.NodeID
	}
	cfg.LegacyCleanupPending = true

	if err := Save(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

// Load reads path, filling anything absent from the file with defaults. A
// missing file is not an error: defaults are written back so the user has
// something to edit. The returned bool reports whether the file was created.
func Load(path string) (Config, bool, error) {
	cfg := Defaults()

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := Save(path, cfg); err != nil {
			return cfg, false, err
		}
		return cfg, true, nil
	}
	if err != nil {
		return cfg, false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Defaults(), false, fmt.Errorf("parse %s: %w", path, err)
	}

	cfg.Normalize()
	return cfg, false, nil
}

// Save writes the config atomically with owner-only permissions, since it
// contains the broker password.
func Save(path string, cfg Config) error {
	cfg.Normalize()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Normalize clamps every field into a usable range and fills in derived
// defaults. It never fails, so a hand-edited file can always be loaded.
func (c *Config) Normalize() {
	c.MQTTHost = strings.TrimSpace(c.MQTTHost)
	if c.MQTTHost == "" {
		c.MQTTHost = Defaults().MQTTHost
	}
	if c.MQTTPort <= 0 || c.MQTTPort > 65535 {
		if c.MQTTTLS {
			c.MQTTPort = 8883
		} else {
			c.MQTTPort = 1883
		}
	}

	c.DeviceName = strings.TrimSpace(c.DeviceName)
	if c.DeviceName == "" {
		c.DeviceName = hostname()
	}
	c.NodeID = Slug(c.NodeID)
	if c.NodeID == "pc" && Slug(c.DeviceName) != "" {
		c.NodeID = Slug(c.DeviceName)
	}

	c.TopicPrefix = strings.Trim(strings.TrimSpace(c.TopicPrefix), "/")
	if c.TopicPrefix == "" {
		c.TopicPrefix = AppName
	}
	c.DiscoveryPrefix = strings.Trim(strings.TrimSpace(c.DiscoveryPrefix), "/")
	if c.DiscoveryPrefix == "" {
		c.DiscoveryPrefix = "homeassistant"
	}

	c.ClientID = strings.TrimSpace(c.ClientID)
	if c.ClientID == "" {
		c.ClientID = AppName + "-" + c.NodeID
	}

	c.PollIntervalMs = clampInt(c.PollIntervalMs, MinIntervalMs, MaxIntervalMs, Defaults().PollIntervalMs)
	c.PublishIntervalMs = clampInt(c.PublishIntervalMs, MinIntervalMs, MaxIntervalMs, Defaults().PublishIntervalMs)
	// Publishing more often than reading would only repeat the same numbers,
	// and a publish interval that is not a whole number of reads would drift.
	if c.PublishIntervalMs < c.PollIntervalMs {
		c.PublishIntervalMs = c.PollIntervalMs
	}
	if remainder := c.PublishIntervalMs % c.PollIntervalMs; remainder != 0 {
		c.PublishIntervalMs += c.PollIntervalMs - remainder
	}

	c.IdleTimeoutMs = clampInt(c.IdleTimeoutMs, MinIdleMs, MaxIdleMs, Defaults().IdleTimeoutMs)
	c.Language = string(i18n.Parse(c.Language))
	if c.WebPort <= 0 || c.WebPort > 65535 {
		c.WebPort = Defaults().WebPort
	}

	c.normalizeExports()
	c.normalizeSensors()
}

func (c *Config) normalizeSensors() {
	c.PingTarget = strings.TrimSpace(c.PingTarget)
	c.PingCount = clampInt(c.PingCount, 1, 10, Defaults().PingCount)
	c.PingIntervalMs = clampInt(c.PingIntervalMs, 2000, 600_000, Defaults().PingIntervalMs)

	// Drive letters are stored uppercase without a colon, so "c:" and "C:\"
	// both match what the disk source reports.
	cleaned := c.DiskInclude[:0]
	for _, drive := range c.DiskInclude {
		drive = strings.ToUpper(strings.Trim(strings.TrimSpace(drive), `:\/`))
		if drive != "" {
			cleaned = append(cleaned, drive)
		}
	}
	c.DiskInclude = cleaned
}

// Lang is the configured language.
func (c Config) Lang() i18n.Lang { return i18n.Parse(c.Language) }

// WantsDisk reports whether a drive letter should be collected. An empty
// include list means every fixed drive.
func (c Config) WantsDisk(letter string) bool {
	if len(c.DiskInclude) == 0 {
		return true
	}
	letter = strings.ToUpper(strings.Trim(letter, `:\/`))
	for _, want := range c.DiskInclude {
		if want == letter {
			return true
		}
	}
	return false
}

func (c *Config) normalizeExports() {
	c.DataBindAddress = strings.TrimSpace(c.DataBindAddress)
	if c.DataBindAddress == "" {
		c.DataBindAddress = Defaults().DataBindAddress
	}
	if c.DataPort <= 0 || c.DataPort > 65535 {
		c.DataPort = Defaults().DataPort
	}
	c.DataToken = strings.TrimSpace(c.DataToken)

	// A data server with every format switched off would listen for nothing,
	// so treat that as switched off.
	if !c.JSONEnabled && !c.PrometheusEnabled && !c.InfluxPullEnabled {
		c.DataServerEnabled = false
	}

	c.InfluxURL = strings.TrimRight(strings.TrimSpace(c.InfluxURL), "/")
	c.InfluxOrg = strings.TrimSpace(c.InfluxOrg)
	c.InfluxBucket = strings.TrimSpace(c.InfluxBucket)
	c.InfluxToken = strings.TrimSpace(c.InfluxToken)
	c.InfluxMeasurement = strings.TrimSpace(c.InfluxMeasurement)
	if c.InfluxMeasurement == "" {
		c.InfluxMeasurement = Defaults().InfluxMeasurement
	}
	// Pushing without a target would only produce errors every interval.
	if c.InfluxURL == "" || c.InfluxBucket == "" {
		c.InfluxPushEnabled = false
	}
}

// DataAddress is the listen address of the data server.
func (c Config) DataAddress() string {
	return fmt.Sprintf("%s:%d", c.DataBindAddress, c.DataPort)
}

// InfluxWriteURL is the InfluxDB v2 write endpoint for the configured bucket.
// InfluxDB 1.8 exposes the same path, with the token set to "user:password".
func (c Config) InfluxWriteURL() string {
	query := neturl.Values{}
	if c.InfluxOrg != "" {
		query.Set("org", c.InfluxOrg)
	}
	query.Set("bucket", c.InfluxBucket)
	query.Set("precision", "ns")
	return c.InfluxURL + "/api/v2/write?" + query.Encode()
}

// AnyExportEnabled reports whether the snapshot goes anywhere at all.
func (c Config) AnyExportEnabled() bool {
	return c.MQTTEnabled || c.DataServerEnabled || c.InfluxPushEnabled
}

func clampInt(v, lo, hi, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// BrokerURL is the connection string handed to the MQTT client.
func (c Config) BrokerURL() string {
	scheme := "tcp"
	if c.MQTTTLS {
		scheme = "ssl"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, c.MQTTHost, c.MQTTPort)
}

// StateTopic carries the JSON payload every sensor reads from.
func (c Config) StateTopic() string {
	return fmt.Sprintf("%s/%s/state", c.TopicPrefix, c.NodeID)
}

// AvailabilityTopic is retained and also used as the MQTT last will.
func (c Config) AvailabilityTopic() string {
	return fmt.Sprintf("%s/%s/availability", c.TopicPrefix, c.NodeID)
}

// DiscoveryTopic is where Home Assistant picks up one entity's configuration.
func (c Config) DiscoveryTopic(component, key string) string {
	return fmt.Sprintf("%s/%s/%s_%s/%s/config", c.DiscoveryPrefix, component, IDPrefix, c.NodeID, key)
}

// LegacyDiscoveryTopic is the same topic under the previous application name,
// used once to retire entities left behind by an older version.
func (c Config) LegacyDiscoveryTopic(component, key string) string {
	return fmt.Sprintf("%s/%s/%s_%s/%s/config", c.DiscoveryPrefix, component, LegacyAppName, c.NodeID, key)
}

// ObjectID is the entity id Home Assistant should suggest, e.g. "fps_corganpc2".
func (c Config) ObjectID(key string) string {
	return key + "_" + c.NodeID
}

// UniqueID identifies an entity across renames and restarts.
func (c Config) UniqueID(key string) string {
	return fmt.Sprintf("%s_%s_%s", IDPrefix, c.NodeID, key)
}

// DeviceIdentifier groups every entity under one Home Assistant device.
func (c Config) DeviceIdentifier() string {
	return fmt.Sprintf("%s_%s", IDPrefix, c.NodeID)
}

// WebAddress is the listen address of the settings UI, bound to loopback only.
func (c Config) WebAddress() string {
	return fmt.Sprintf("127.0.0.1:%d", c.WebPort)
}

// WebURL is the address to open in a browser.
func (c Config) WebURL() string {
	return "http://" + c.WebAddress()
}
