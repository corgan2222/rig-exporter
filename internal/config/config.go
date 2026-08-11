// Package config holds the on-disk settings for rig-exporter.
//
// The file lives next to the log in %APPDATA%\rig-exporter and is written by both
// the first run (defaults) and the settings web UI.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/corgan2222/rig-exporter/internal/i18n"
)

const (
	// AppName is used for the executable, the config directory, the default
	// topic prefix and the autostart registry value.
	AppName = "rig-exporter"
	// IDPrefix goes into identifiers that must not contain a hyphen or that
	// read better short: the device identifier and the InfluxDB measurement.
	IDPrefix = "rig"
	// EntityPrefix opens every Home Assistant entity id, so that a glance at an
	// entity list says which program owns it. Short on purpose: it is repeated
	// on every one of a hundred entities.
	EntityPrefix = "re"
	// Version is reported to Home Assistant as the device software version.
	Version = "1.9.4"

	// LegacyAppName is the previous name. Its configuration is migrated on
	// first start and its retained discovery topics are cleaned up.
	LegacyAppName = "fps2mqtt"

	// RTSSDownloadURL is shown whenever RivaTuner Statistics Server is missing.
	RTSSDownloadURL = "https://www.guru3d.com/download/rtss-rivatuner-statistics-server-download/"
	// AfterburnerURL is shown when no graphics telemetry source is available.
	AfterburnerURL = "https://www.msi.com/Landing/afterburner/graphics-cards"
	// AMDDriverURL is offered only when an AMD card is present and its driver
	// is not answering. The full Adrenalin package carries ADLX; the display
	// driver on its own does not, and that is the difference between a Radeon
	// reporting its temperature and staying silent.
	AMDDriverURL = "https://www.amd.com/en/support/download/drivers.html"
	// PawnIOURL is where the kernel driver behind CPU power comes from. Named
	// on the source line rather than hidden in a tooltip: it is the one source
	// this program cannot install for you.
	PawnIOURL = "https://pawnio.eu/"
	// CoolingURL points at the protocols the cooling source is decoded from.
	// Credit where it is due, and the only list of what might work.
	CoolingURL = "https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/tree/master/LibreHardwareMonitorLib/Hardware/Controller"

	// ProjectURL and AuthorURL are where the interface points when somebody
	// clicks the name in the header or the credit in the footer.
	ProjectURL = "https://github.com/corgan2222/rig-exporter"
	AuthorURL  = "https://github.com/corgan2222"
	// AuthorName is the person who wrote this, spelled the way he spells it.
	AuthorName = "Stefan Knaak"
)

// Build identifies the exact build behind a release, and is set at link time by
// build.ps1 from the commit count and the short hash.
//
// A version alone cannot answer "is this the binary that has the fix", because
// a release number does not move between commits. It stays empty for a plain
// `go build`, which is honest: that binary was not produced by the build script
// and there is nothing to identify it by.
var Build = ""

// VersionString is what a person is shown and what is reported to Home
// Assistant: the release, and the build behind it when there is one.
func VersionString() string {
	if Build == "" {
		return Version
	}
	return Version + "+" + Build
}

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

	// SensorSet decides how much of each group is reported: SensorSetStandard
	// keeps what somebody watching their machine looks at, SensorSetExtended
	// adds the inventory and the fine detail. It cuts across the group
	// switches below rather than replacing them — the groups say which
	// hardware is read at all, this says how much of it is worth an entity.
	SensorSet string `json:"sensor_set"`

	// Measurements is the same decision made one measurement at a time.
	Measurements Measurements `json:"measurements"`

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
	PawnIOEnabled bool `json:"pawnio_enabled"`
	// SpecialHardwareEnabled opts in to the USB cooling controllers: all-in-one
	// water coolers, pumps and fan hubs.
	//
	// Off by default, and marked as untested in the interface, because the
	// protocols are reverse-engineered and only one device family has ever been
	// held against real hardware. Reading is all it does — nothing here writes
	// to a pump — so the worst case is a measurement that does not appear, not
	// a cooler that stops working.
	SpecialHardwareEnabled bool     `json:"special_hardware_enabled"`
	DiskEnabled            bool     `json:"disk_enabled"`
	DiskInclude            []string `json:"disk_include"`
	NetEnabled             bool     `json:"net_enabled"`
	// NetAllAdapters reports every connected interface instead of only the
	// one carrying the default route.
	NetAllAdapters bool `json:"net_all_adapters"`
	// UpdateCheckEnabled lets the program ask GitHub every six hours whether a
	// newer release exists.
	//
	// On by default, and switchable because it is the one thing this program
	// does that talks to somewhere other than the machine it measures and the
	// broker it was pointed at. Off means no request leaves the machine — and
	// no update is offered, in Home Assistant or on the dashboard. Nothing is
	// ever installed without somebody asking for it either way.
	//
	// Load unmarshals into Defaults, so a configuration written before this
	// existed keeps the value on rather than falling silently to off.
	UpdateCheckEnabled bool `json:"update_check_enabled"`

	// CrashReportOffered decides whether the interface offers to turn a crash
	// record into a GitHub issue.
	//
	// On by default, because the offer is only a prepared page: the button
	// opens GitHub with the report filled in, and the user reads every word and
	// presses submit themselves. Nothing is ever sent from here, and no token
	// exists that could be. Switchable because the report names the machine,
	// its hardware and its Windows build, and not everybody wants that button
	// within reach.
	//
	// The record itself is written either way. Whether a crash is noticed is
	// not a setting.
	CrashReportOffered bool `json:"crash_report_offered"`

	// BatteryEnabled reports the battery pack: charge, mains, wear.
	//
	// On by default like the other hardware groups, and costing nothing on a
	// machine that has no battery — the source asks Windows once and reports
	// nothing when the answer is that there is no pack.
	BatteryEnabled bool `json:"battery_enabled"`

	// Latency probe, part of the network group.
	PingEnabled    bool   `json:"ping_enabled"`
	PingTarget     string `json:"ping_target"`
	PingCount      int    `json:"ping_count"`
	PingIntervalMs int    `json:"ping_interval_ms"`

	// SelfUsageEnabled reports what this program costs the machine it measures.
	//
	// A group of its own rather than a side effect of debug logging: "how much
	// does watching cost me" is a question somebody asks without wanting their
	// log filled with every reading. Off by default, because two values that are
	// almost always flat are two entities nobody asked for.
	SelfUsageEnabled bool `json:"self_usage_enabled"`

	// TopProcessesEnabled ranks the programs using the most CPU and memory.
	//
	// The most expensive thing this program can be asked to do, and off by
	// default for that reason: one pass reads every process on the machine, and
	// the result is two entities whose attributes change on every sample.
	TopProcessesEnabled    bool `json:"top_processes_enabled"`
	TopProcessesCount      int  `json:"top_processes_count"`
	TopProcessesIntervalMs int  `json:"top_processes_interval_ms"`

	// LegacyCleanupPending is set once when a configuration is migrated from
	// the previous application name, and cleared after the old retained
	// discovery topics have been emptied.
	LegacyCleanupPending bool `json:"legacy_cleanup_pending"`

	// PollIntervalMs is how often the hardware is read. It sets how smoothly
	// the tray and the settings page move.
	PollIntervalMs int `json:"poll_interval_ms"`
	// PublishIntervalMs is how often a reading is handed to the export targets
	// while a game is rendering. It is never shorter than the poll interval.
	// The JSON name is the historical one, so an older configuration keeps
	// working.
	PublishIntervalMs int `json:"interval_ms"`
	// IdlePublishIntervalMs is the same for a machine that is rendering
	// nothing. A second-by-second series of an idle PC has no reader, and
	// every export is a row in somebody's database, so the idle pace is
	// deliberately much slower.
	IdlePublishIntervalMs int `json:"idle_interval_ms"`
	// Decimals keeps the fractional part of numeric readings. Switching it off
	// publishes whole numbers throughout, which cuts how often a value changes
	// at all — and a value that does not change costs no row in the Home
	// Assistant database.
	Decimals      bool `json:"decimals"`
	IdleTimeoutMs int  `json:"idle_timeout_ms"`

	// RecorderNoticeRead remembers that the hint about the Home Assistant
	// database has been read.
	//
	// It lives here rather than in the browser, where it started. The web
	// server falls back to a random port when the configured one is taken, and
	// a different port is a different origin — so local storage was thrown away
	// on most restarts and the hint came back every time. It is a fact about
	// this installation anyway, not about one browser.
	RecorderNoticeRead bool `json:"recorder_notice_read"`
	// NoGPU remembers that this installation does not need the game-only status
	// supplied by RTSS. It changes presentation only: collection and every
	// export contract stay untouched.
	NoGPU bool `json:"no_gpu"`

	Language string `json:"language"`
	WebPort  int    `json:"web_port"`
	// WebBindAll opens the settings interface to the local network instead of
	// loopback only.
	//
	// Off by default and deliberately its own decision: the page carries the
	// broker password and every other setting, and nothing on it asks who is
	// calling. Somebody switching this on is saying they trust their network.
	WebBindAll bool `json:"web_bind_all"`
	Autostart  bool `json:"autostart"`
	Debug      bool `json:"debug"`
}

// The two sensor sets. Which measurement is in which is decided in
// internal/metrics; these are only the names the configuration stores.
//
// Superseded by Measurements below and kept because a configuration written by
// an older version still carries it, and because writing it back keeps a
// downgrade sane. Normalize holds the two in step.
const (
	// SensorSetStandard reports what somebody watching their machine looks at.
	SensorSetStandard = "standard"
	// SensorSetExtended adds the inventory and the fine detail on top.
	SensorSetExtended = "extended"
)

// The rungs of the measurement ladder, from fewest to most. The names match
// metrics.Preset; the strings live here because this is what lands in the file.
const (
	PresetMinimal  = "minimal"
	PresetBasic    = "basic"
	PresetExtended = "extended"
)

// Measurements says which measurements are collected: a rung of the ladder,
// plus what the user added to it or took out of it by hand.
//
// The rung is stored rather than the resulting list of identifiers, and that is
// the whole point of the shape. A measurement added to the catalogue by a later
// version joins the rung it belongs to on its own; a stored list would have
// left it switched off, and nobody goes looking for a value they never heard
// of. The two lists then hold exactly what the user decided against the rung,
// which is also the only part worth reading in the file.
type Measurements struct {
	Preset  string   `json:"preset"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// normalizeMeasurements settles the rung and keeps the old field in step.
//
// Defaults deliberately leaves the rung empty. Load unmarshals into Defaults,
// so a prefilled rung would have looked exactly like a chosen one, and a file
// written before the ladder existed would have been read as "extended" no
// matter what its sensor_set said — silently reporting the whole catalogue to
// somebody who had asked for half of it.
//
// An empty rung means this file was written before the ladder existed, so the
// old two-way switch decides — that is the migration, and it happens once,
// silently, without anybody losing what they had chosen. From then on the rung
// leads and sensor_set follows it, so the file never says two things at once.
func (c *Config) normalizeMeasurements() {
	switch c.Measurements.Preset {
	case PresetMinimal, PresetBasic, PresetExtended:
	case "":
		c.Measurements.Preset = PresetForSensorSet(c.SensorSet)
	default:
		c.Measurements.Preset = PresetExtended
	}
	c.SensorSet = sensorSetForPreset(c.Measurements.Preset)

	c.Measurements.Added = cleanIDs(c.Measurements.Added)
	c.Measurements.Removed = cleanIDs(c.Measurements.Removed)
}

// cleanIDs trims, drops blanks and duplicates, and sorts. A hand-edited list
// should come back readable rather than exactly as it was typed.
func cleanIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// PresetForSensorSet migrates the old two-way switch onto the ladder. Also
// what the settings page uses while its control still offers the old two.
func PresetForSensorSet(sensorSet string) string {
	if sensorSet == SensorSetStandard {
		return PresetBasic
	}
	return PresetExtended
}

// sensorSetForPreset is the way back, for the field an older version reads.
// Minimal has no equivalent there; standard is the closer of the two.
func sensorSetForPreset(preset string) string {
	if preset == PresetExtended {
		return SensorSetExtended
	}
	return SensorSetStandard
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
		// has no source simply produces nothing. The extended set likewise:
		// somebody who wants less can say so, but a value that was never
		// offered is a value nobody knows to ask for.
		SensorSet:        SensorSetExtended,
		GPUEnabled:       true,
		CPUDetailEnabled: true,
		CPUPerCore:       false, // one entity per thread is a lot for a 16-core CPU
		RAMDetailEnabled: true,
		PawnIOEnabled:    false, // needs elevation; the user has to choose it
		// Reverse-engineered protocols against hardware nobody here owns most
		// of. Whoever switches it on should have decided to.
		SpecialHardwareEnabled: false,
		DiskEnabled:            true,
		NetEnabled:             true,
		BatteryEnabled:         true,
		// Offering costs nothing and sends nothing: the button only opens a
		// page that the user has to submit.
		CrashReportOffered: true,

		// On by default: a telemetry exporter that quietly runs an old build
		// with a fixed bug in it helps nobody. Nothing installs itself.
		UpdateCheckEnabled: true,
		PingEnabled:        true,
		PingTarget:         "", // empty means the default gateway
		PingCount:          3,
		PingIntervalMs:     15000,

		// Off, but with its settings already sensible for whoever switches it
		// on. Ten seconds is fine enough to see what a game or a build did and
		// slow enough that reading 660 processes costs nothing worth naming.
		TopProcessesEnabled:    false,
		TopProcessesCount:      5,
		TopProcessesIntervalMs: 10000,

		// Read four times as often as we publish while a game runs: the tray
		// and the settings page stay lively without putting four times the
		// traffic on the broker. With nothing rendering, five times slower
		// again — an idle machine has nothing to say every two seconds.
		PollIntervalMs:        500,
		PublishIntervalMs:     2000,
		IdlePublishIntervalMs: 10000,
		Decimals:              true,
		IdleTimeoutMs:         3000,

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

// ModuleDir is where fetched PawnIO modules are kept, next to the
// configuration. An unresolvable config directory yields an empty path, which
// the module store reports when it fails to read.
func ModuleDir() string {
	dir, err := Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "modules")
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
	if err := json.Unmarshal(stripBOM(raw), &cfg); err != nil {
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

// utf8BOM is what several Windows tools put at the front of a text file —
// PowerShell's `Set-Content -Encoding utf8` and older editors among them.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// stripBOM removes that mark. encoding/json refuses a document that starts with
// it, and the error it gives ("invalid character 'ï'") sends the reader looking
// for a typo that is not there. Somebody who edited their configuration by hand
// should not lose it to an invisible three bytes.
func stripBOM(raw []byte) []byte { return bytes.TrimPrefix(raw, utf8BOM) }

// Load reads path, filling anything absent from the file with defaults. A
// missing file is not an error: defaults are written back so the user has
// something to edit. The returned bool reports whether the file was created.
func Load(path string) (Config, bool, error) {
	cfg := Defaults()

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Normalized here and not left to Save, which takes its Config by
		// value and would resolve only its own copy: the file would carry a
		// preset and the caller would not. Both returns from this function
		// have to hand back the same thing, and the one below already does.
		cfg.Normalize()
		if err := Save(path, cfg); err != nil {
			return cfg, false, err
		}
		return cfg, true, nil
	}
	if err != nil {
		return cfg, false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(stripBOM(raw), &cfg); err != nil {
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
	c.PublishIntervalMs = snapToPoll(
		clampInt(c.PublishIntervalMs, MinIntervalMs, MaxIntervalMs, Defaults().PublishIntervalMs),
		c.PollIntervalMs)
	c.IdlePublishIntervalMs = snapToPoll(
		clampInt(c.IdlePublishIntervalMs, MinIntervalMs, MaxIntervalMs, Defaults().IdlePublishIntervalMs),
		c.PollIntervalMs)

	c.IdleTimeoutMs = clampInt(c.IdleTimeoutMs, MinIdleMs, MaxIdleMs, Defaults().IdleTimeoutMs)
	c.Language = string(i18n.Parse(c.Language))
	if c.WebPort <= 0 || c.WebPort > 65535 {
		c.WebPort = Defaults().WebPort
	}

	c.normalizeExports()
	c.normalizeSensors()
}

func (c *Config) normalizeSensors() {
	// Anything unrecognised means extended, including the empty string an
	// older configuration file leaves behind: a setting nobody chose should
	// report more rather than silently less.
	if c.SensorSet != SensorSetStandard {
		c.SensorSet = SensorSetExtended
	}
	c.normalizeMeasurements()

	c.PingTarget = strings.TrimSpace(c.PingTarget)
	c.PingCount = clampInt(c.PingCount, 1, 10, Defaults().PingCount)
	c.PingIntervalMs = clampInt(c.PingIntervalMs, 2000, 600_000, Defaults().PingIntervalMs)
	c.TopProcessesCount = clampInt(c.TopProcessesCount, 3, 10, Defaults().TopProcessesCount)
	// Never faster than two seconds: a pass reads every process on the machine,
	// and below that the sampling starts to cost more than what it measures.
	c.TopProcessesIntervalMs = clampInt(c.TopProcessesIntervalMs, 2000, 600_000, Defaults().TopProcessesIntervalMs)

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

// snapToPoll rounds a publish interval up to a whole number of reads.
// Publishing more often than reading would only repeat the same numbers, and an
// interval that is not a whole multiple of the read rate would drift against it.
func snapToPoll(interval, poll int) int {
	if poll <= 0 || interval < poll {
		return max(interval, poll)
	}
	if remainder := interval % poll; remainder != 0 {
		interval += poll - remainder
	}
	return interval
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

// UpdateStateTopic carries the retained state of the software update entity.
func (c Config) UpdateStateTopic() string {
	return fmt.Sprintf("%s/%s/update/state", c.TopicPrefix, c.NodeID)
}

// UpdateAvailabilityTopic is retained independently from the process-wide
// last will. Home Assistant requires both topics to be online before it makes
// the install command available.
func (c Config) UpdateAvailabilityTopic() string {
	return fmt.Sprintf("%s/%s/update/availability", c.TopicPrefix, c.NodeID)
}

// UpdateCommandTopic receives the short-lived install command from Home
// Assistant. The command never carries a version or URL; the updater decides
// which previously verified release is safe to install.
func (c Config) UpdateCommandTopic() string {
	return fmt.Sprintf("%s/%s/update/install", c.TopicPrefix, c.NodeID)
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

// ObjectID is the entity id Home Assistant should suggest, e.g.
// "re_corganpc2_gpu0_vendor".
//
// Read left to right it answers, in the order somebody scanning a list of a
// hundred entities wants them: which program put this here, which machine it
// came from, which piece of hardware, and finally what is measured. The
// hostname used to trail at the end, where it was no help at all in telling two
// PCs apart at a glance.
func (c Config) ObjectID(key string) string {
	return c.ObjectPrefix() + key
}

// ObjectPrefix is what every entity id of this machine begins with. It is also
// exactly the glob a Home Assistant recorder filter needs to name all of them
// at once, which is what the settings page builds its snippet from.
func (c Config) ObjectPrefix() string {
	return fmt.Sprintf("%s_%s_", EntityPrefix, c.NodeID)
}

// UniqueID identifies an entity across renames and restarts.
//
// The same string as the object id. Home Assistant keys an entity's identity on
// this and its displayed id on the other, and having them agree means what a
// person reads in the interface is exactly what the integration knows it as.
func (c Config) UniqueID(key string) string {
	return c.ObjectID(key)
}

// DeviceIdentifier groups every entity under one Home Assistant device.
func (c Config) DeviceIdentifier() string {
	return fmt.Sprintf("%s_%s", IDPrefix, c.NodeID)
}

// WebAddress is the listen address of the settings UI: loopback only, unless
// WebBindAll opens it to the network.
func (c Config) WebAddress() string {
	if c.WebBindAll {
		return fmt.Sprintf("0.0.0.0:%d", c.WebPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", c.WebPort)
}

// WebURL is the address to open in a browser.
//
// Always loopback, even when the server listens on every interface: 0.0.0.0 is
// something to bind to, not something to type. The address another machine
// would use is built in the web package, which is the only place that knows
// which port the server actually got.
func (c Config) WebURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.WebPort)
}
