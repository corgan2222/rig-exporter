package hamqtt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

func testConfig() config.Config {
	cfg := config.Defaults()
	// Pinned, because Defaults takes the language from Windows and several
	// assertions below are about translated names. Left to the machine, this
	// package passes on a German installation and fails on an English one —
	// which is exactly what happened on the build server.
	cfg.Language = string(i18n.DE)
	cfg.NodeID = "corganpc2"
	cfg.DeviceName = "CorganPC2"
	cfg.TopicPrefix = config.AppName
	cfg.DiscoveryPrefix = "homeassistant"
	return cfg
}

func decode(t *testing.T, reading metrics.Reading) (string, discoveryPayload) {
	t.Helper()

	topic, raw, err := discoveryMessage(testConfig(), "", reading)
	if err != nil {
		t.Fatalf("discoveryMessage(%s): %v", reading.Key(), err)
	}
	var payload discoveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return topic, payload
}

// The device page's "Visit" link has to open the port the server actually got.
//
// A configured 8787 that was busy leaves the interface on an ephemeral port,
// and the configuration still says 8787. Discovery messages are retained, so
// one published with the wrong port stays wrong until something overwrites it.
func TestTheDeviceLinkFollowsTheRealAddress(t *testing.T) {
	cfg := testConfig()
	cfg.WebPort = 8787
	reading := metrics.Gauge(metrics.FPS, "", 143.2)

	_, raw, err := discoveryMessage(cfg, "http://127.0.0.1:48352", reading)
	if err != nil {
		t.Fatalf("discoveryMessage: %v", err)
	}
	var payload discoveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := "http://127.0.0.1:48352"; payload.Device.ConfigURL != want {
		t.Errorf("ConfigURL = %q, want the address the server reported, %q",
			payload.Device.ConfigURL, want)
	}

	// Before the server has reported anything there is nothing better than the
	// configured address, and no link at all would be worse.
	if got := configURL(cfg, ""); got != cfg.WebURL() {
		t.Errorf("without a reported address ConfigURL = %q, want the configured %q",
			got, cfg.WebURL())
	}
}

// A moved address has to reach the broker, and the only way it can is by
// announcing everything again.
//
// The publisher usually connects before the web server has bound its port, so
// the first round of discovery goes out with the configured address. Without
// this, that first round would be the only one, and a retained message with the
// wrong port would sit there for good.
func TestAMovedAddressMakesTheNextPassAnnounceAgain(t *testing.T) {
	p := testPublisher(t)
	p.announced = map[string]EntityRef{"fps": {component: "sensor", key: "fps"}}
	p.announcedURL = "http://127.0.0.1:8787"

	// The same address changes nothing: re-announcing every second would flood
	// the broker with messages that say what it already knows.
	p.forgetAnnouncementsIfURLChanged("http://127.0.0.1:8787")
	if p.republish {
		t.Error("an unchanged address triggered a re-announcement anyway")
	}

	p.forgetAnnouncementsIfURLChanged("http://127.0.0.1:48352")
	if !p.republish {
		t.Error("a changed address did not make the next pass announce again")
	}
	// And the list of what lies retained on the broker is kept. It used to be
	// emptied here, which announced again by forgetting — and forgetting is
	// what left RetireUnselected with nothing to retire.
	if len(p.announced) != 1 {
		t.Error("the record of what is retained on the broker was thrown away")
	}
	if p.announcedURL != "http://127.0.0.1:48352" {
		t.Errorf("announcedURL = %q, want the new address", p.announcedURL)
	}
}

// A ranked list is one entity with a table beside it, not five entities whose
// meaning changes whenever two programs swap places.
func TestARankedListAnnouncesItsTableAsAttributes(t *testing.T) {
	cfg := testConfig()
	reading := metrics.Table(metrics.TopCPU, "", []metrics.Row{
		{Label: "firefox.exe", Value: 41.2},
		{Label: "cs2.exe", Value: 12},
	})

	topic, payload := decode(t, reading)
	if want := "homeassistant/sensor/rig_corganpc2/top_cpu/config"; topic != want {
		t.Errorf("topic = %q, want %q", topic, want)
	}

	if want := "{{ value_json.top_cpu.top }}"; payload.ValueTemplate != want {
		t.Errorf("value_template = %q, want the leader %q", payload.ValueTemplate, want)
	}
	if payload.JSONAttributesTopic != cfg.StateTopic() {
		t.Errorf("attributes topic = %q, want the state topic — one message, not two",
			payload.JSONAttributesTopic)
	}
	if want := "{{ value_json.top_cpu | tojson }}"; payload.JSONAttributesTemplate != want {
		t.Errorf("attributes template = %q, want %q", payload.JSONAttributesTemplate, want)
	}

	// The unit describes the rows. On the state, which is a program name, it
	// would render as "firefox.exe %".
	if payload.UnitOfMeasurement != "" {
		t.Errorf("unit = %q, want none on a state that is a name", payload.UnitOfMeasurement)
	}
	// Display precision applies to numbers; Home Assistant rejects the whole
	// discovery when it arrives on a text state.
	if payload.SuggestedPrecision != nil {
		t.Errorf("suggested_display_precision = %v, want none", *payload.SuggestedPrecision)
	}
}

// The game entity is established: dashboards, automations and history are
// built on its id, its state and the template that reads it. Learning what the
// launchers call the game may add attributes to it and must change nothing
// else.
func TestTheGameEntityGainsAttributesAndNothingElse(t *testing.T) {
	cfg := testConfig()
	topic, payload := decode(t, metrics.Text(metrics.Game, "", "Cyberpunk2077.exe"))

	if want := "homeassistant/sensor/rig_corganpc2/game/config"; topic != want {
		t.Errorf("topic = %q, want %q", topic, want)
	}
	if want := "{{ value_json.game }}"; payload.ValueTemplate != want {
		t.Errorf("value_template = %q, want the plain state %q", payload.ValueTemplate, want)
	}
	if want := cfg.UniqueID("game"); payload.UniqueID != want {
		t.Errorf("unique_id = %q, want %q", payload.UniqueID, want)
	}

	if payload.JSONAttributesTopic != cfg.StateTopic() {
		t.Errorf("attributes topic = %q, want the state topic — one message, not two",
			payload.JSONAttributesTopic)
	}
	// The default is what keeps this quiet in the ordinary case: the key is
	// absent whenever nothing was identified, and it clears the attributes when
	// a game closes rather than leaving the last title on the entity forever.
	if want := "{{ value_json.game_details | default({}) | tojson }}"; payload.JSONAttributesTemplate != want {
		t.Errorf("attributes template = %q, want %q", payload.JSONAttributesTemplate, want)
	}
}

// The details themselves are not an entity. An entity whose state is an object
// is nothing anybody can put on a dashboard, and it would be a second copy of
// what the game entity already carries.
func TestTheGameDetailsAreNeverAnEntityOfTheirOwn(t *testing.T) {
	if !metrics.GameDetails.NoEntity {
		t.Error("the game details would be discovered as an entity of their own")
	}

	var set metrics.Set
	set.Add(metrics.Details(metrics.GameDetails, "",
		metrics.Detail{Name: metrics.DetailPlatform, Value: "steam"}))

	for _, reading := range set.Entities() {
		if reading.Def.ID == metrics.GameDetails.ID {
			t.Error("the game details reached the entity list")
		}
	}
}

// Everything else must keep its unit — the table case is an exception, not a
// new default.
func TestOrdinaryReadingsKeepTheirUnit(t *testing.T) {
	_, payload := decode(t, metrics.Gauge(metrics.CPULoad, "", 24.5))
	if payload.UnitOfMeasurement != "%" {
		t.Errorf("unit = %q, want %%", payload.UnitOfMeasurement)
	}
}

// The entity naming the whole thing is built around: sensor.re_corganpc2_fps.
func TestDiscoveryNamesEntitiesAfterTheHost(t *testing.T) {
	cfg := testConfig()
	topic, payload := decode(t, metrics.Gauge(metrics.FPS, "", 143.2))

	if want := "homeassistant/sensor/rig_corganpc2/fps/config"; topic != want {
		t.Errorf("topic = %q, want %q", topic, want)
	}
	// This is the field that decides the entity id in Home Assistant 2026;
	// object_id was removed from the MQTT component and is only still sent for
	// older versions. It carries the domain, so it is a whole entity id.
	if payload.DefaultEntityID != "sensor.re_corganpc2_fps" {
		t.Errorf("default_entity_id = %q, want sensor.re_corganpc2_fps", payload.DefaultEntityID)
	}
	if payload.ObjectID != "re_corganpc2_fps" {
		t.Errorf("object_id = %q, want re_corganpc2_fps", payload.ObjectID)
	}
	if payload.UniqueID != "re_corganpc2_fps" {
		t.Errorf("unique_id = %q", payload.UniqueID)
	}
	if payload.StateTopic != cfg.StateTopic() || payload.AvailabilityTopic != cfg.AvailabilityTopic() {
		t.Errorf("topics = %q / %q", payload.StateTopic, payload.AvailabilityTopic)
	}
	if payload.ValueTemplate != "{{ value_json.fps }}" {
		t.Errorf("value_template = %q", payload.ValueTemplate)
	}
	if len(payload.Device.Identifiers) != 1 || payload.Device.Identifiers[0] != "rig_corganpc2" {
		t.Errorf("device identifiers = %v", payload.Device.Identifiers)
	}
}

// Software updates belong to the same device as its telemetry, but use Home
// Assistant's native update domain so the normal update card can install one.
func TestSoftwareUpdateDiscoveryOffersAShortLivedInstallCommand(t *testing.T) {
	cfg := testConfig()
	topic, raw, err := updateDiscoveryMessage(cfg, "http://127.0.0.1:48352")
	if err != nil {
		t.Fatalf("updateDiscoveryMessage: %v", err)
	}
	if want := "homeassistant/update/rig_corganpc2/software/config"; topic != want {
		t.Errorf("topic = %q, want %q", topic, want)
	}

	var payload updateDiscoveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.DefaultEntityID != "update.re_corganpc2_software" {
		t.Errorf("default_entity_id = %q", payload.DefaultEntityID)
	}
	if payload.ObjectID != cfg.ObjectID("software") || payload.UniqueID != cfg.UniqueID("software") {
		t.Errorf("identifiers = %q / %q", payload.ObjectID, payload.UniqueID)
	}
	if payload.StateTopic != cfg.UpdateStateTopic() || payload.CommandTopic != cfg.UpdateCommandTopic() {
		t.Errorf("topics = %q / %q", payload.StateTopic, payload.CommandTopic)
	}
	if payload.AvailabilityMode != "all" {
		t.Errorf("availability_mode = %q, want all", payload.AvailabilityMode)
	}
	wantAvailability := []availabilityTopic{
		{Topic: cfg.AvailabilityTopic(), PayloadAvailable: availableOnline, PayloadNotAvailable: availableOffline},
		{Topic: cfg.UpdateAvailabilityTopic(), PayloadAvailable: availableOnline, PayloadNotAvailable: availableOffline},
	}
	if !reflect.DeepEqual(payload.Availability, wantAvailability) {
		t.Errorf("availability = %#v, want %#v", payload.Availability, wantAvailability)
	}
	if strings.Contains(string(raw), "availability_topic") {
		t.Errorf("update discovery mixes availability list with availability_topic: %s", raw)
	}
	if payload.PayloadInstall != "install" || payload.QoS != 1 || payload.Retain {
		t.Errorf("command = payload %q, qos %d, retain %v", payload.PayloadInstall, payload.QoS, payload.Retain)
	}
	if payload.MessageExpiryInterval.Seconds != 30 {
		t.Errorf("command expiry = %d seconds, want 30", payload.MessageExpiryInterval.Seconds)
	}
	if payload.EntityCategory != "config" {
		t.Errorf("entity_category = %q, want config", payload.EntityCategory)
	}
	if strings.Contains(string(raw), "device_class") {
		t.Errorf("application software was advertised as device firmware: %s", raw)
	}
	if payload.Device.ConfigURL != "http://127.0.0.1:48352" {
		t.Errorf("configuration_url = %q", payload.Device.ConfigURL)
	}
}

// The language may change what a person reads and nothing else.
//
// The name is free to translate because default_entity_id pins the entity id at
// creation: a dashboard, an automation condition and a voice assistant all
// reference that id, and none of them notices a renamed label. An identifier
// that moved with the language would break every one of them.
func TestOnlyTheNameFollowsTheInterfaceLanguage(t *testing.T) {
	reading := metrics.Gauge(metrics.GPUTemperature, "0", 61)

	german := testConfig()
	german.Language = "de"
	english := testConfig()
	english.Language = "en"

	de := discoveryFor(german, "", reading)
	en := discoveryFor(english, "", reading)

	if de.Name != "GPU 0 Temperatur" || en.Name != "GPU 0 Temperature" {
		t.Errorf("names = %q / %q, want them translated", de.Name, en.Name)
	}
	if de.DefaultEntityID != en.DefaultEntityID {
		t.Errorf("default_entity_id changed with the language: %q vs %q",
			de.DefaultEntityID, en.DefaultEntityID)
	}
	if de.UniqueID != en.UniqueID || de.ObjectID != en.ObjectID {
		t.Error("an identifier changed with the language")
	}
	if de.ValueTemplate != en.ValueTemplate {
		t.Error("the value template changed with the language")
	}
}

// A binary sensor lives in its own domain, and the entity id carries it — a
// sensor. prefix on a binary_sensor would name an entity that never exists.
func TestTheDefaultEntityIdCarriesTheRightDomain(t *testing.T) {
	_, payload := decode(t, metrics.Bool(metrics.RTSSUp, "", true))

	if payload.DefaultEntityID != "binary_sensor.re_corganpc2_rtss" {
		t.Errorf("default_entity_id = %q, want the binary_sensor domain", payload.DefaultEntityID)
	}
}

// An instanced reading must reach the value template and the entity id under
// the same key, or the entity shows up permanently unknown.
func TestDiscoveryCarriesTheInstanceIntoTheKey(t *testing.T) {
	topic, payload := decode(t, metrics.Gauge(metrics.DiskUsedPercent, "C:", 61.2))

	if !strings.HasSuffix(topic, "/diskc_used_percent/config") {
		t.Errorf("topic = %q", topic)
	}
	if payload.DefaultEntityID != "sensor.re_corganpc2_diskc_used_percent" {
		t.Errorf("default_entity_id = %q", payload.DefaultEntityID)
	}
	if payload.ObjectID != "re_corganpc2_diskc_used_percent" {
		t.Errorf("object_id = %q", payload.ObjectID)
	}
	if payload.ValueTemplate != "{{ value_json.diskc_used_percent }}" {
		t.Errorf("value_template = %q", payload.ValueTemplate)
	}
	// The hardware leads, so a device page sorts one drive's readings together.
	// This configuration is German, and the name follows it — only identifiers
	// stay put.
	if payload.Name != "Laufwerk C: Belegung" {
		t.Errorf("name = %q, want the hardware first", payload.Name)
	}
}

// Two instances of one definition must not land on the same entity.
func TestDiscoveryKeepsInstancesApart(t *testing.T) {
	firstTopic, first := decode(t, metrics.Gauge(metrics.GPUTemperature, "0", 61))
	secondTopic, second := decode(t, metrics.Gauge(metrics.GPUTemperature, "1", 54))

	if firstTopic == secondTopic {
		t.Errorf("both cards share the topic %s", firstTopic)
	}
	if first.UniqueID == second.UniqueID {
		t.Errorf("both cards share the unique id %s", first.UniqueID)
	}
}

func TestNumericEntitiesCarryTheirUnitAndPrecision(t *testing.T) {
	_, payload := decode(t, metrics.Gauge(metrics.Frametime, "", 6.98))

	if payload.UnitOfMeasurement != "ms" || payload.StateClass != "measurement" {
		t.Errorf("unit = %q state_class = %q", payload.UnitOfMeasurement, payload.StateClass)
	}
	if payload.SuggestedPrecision == nil || *payload.SuggestedPrecision != 2 {
		t.Errorf("suggested_display_precision = %v, want 2", payload.SuggestedPrecision)
	}
}

// With decimals switched off the discovery has to say so too. Promising two
// decimals that never arrive would render every frametime as x.00.
func TestTheAdvertisedPrecisionFollowsWhatIsActuallySent(t *testing.T) {
	metrics.SetDecimals(false)
	t.Cleanup(func() { metrics.SetDecimals(true) })

	reading := metrics.Gauge(metrics.Frametime, "", 6.98)
	if reading.Number != 7 {
		t.Errorf("the reading itself kept its decimals: %v", reading.Number)
	}

	_, payload := decode(t, reading)
	if payload.SuggestedPrecision == nil || *payload.SuggestedPrecision != 0 {
		t.Errorf("suggested_display_precision = %v, want 0", payload.SuggestedPrecision)
	}
}

// Home Assistant rejects a text entity that advertises a unit, a state class
// or a display precision.
func TestTextEntitiesOmitNumericAttributes(t *testing.T) {
	for _, reading := range []metrics.Reading{
		metrics.Text(metrics.Game, "", "Cyberpunk2077.exe"),
		metrics.Text(metrics.Resolution, "", "2560x1440"),
		metrics.Text(metrics.DiskMedia, "C:", "NVMe"),
	} {
		_, raw, err := discoveryMessage(testConfig(), "", reading)
		if err != nil {
			t.Fatalf("discoveryMessage: %v", err)
		}
		body := string(raw)
		for _, forbidden := range []string{"unit_of_measurement", "state_class", "suggested_display_precision"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s advertises %s", reading.Key(), forbidden)
			}
		}
	}
}

func TestBooleanEntitiesBecomeBinarySensors(t *testing.T) {
	topic, payload := decode(t, metrics.Bool(metrics.RTSSUp, "", true))

	if !strings.HasPrefix(topic, "homeassistant/binary_sensor/") {
		t.Errorf("topic = %q, want a binary_sensor", topic)
	}
	if payload.PayloadOn != metrics.PayloadOn || payload.PayloadOff != metrics.PayloadOff {
		t.Errorf("payloads = %q / %q", payload.PayloadOn, payload.PayloadOff)
	}
	if payload.SuggestedPrecision != nil {
		t.Error("a binary sensor advertises a display precision")
	}
}

// Everything the collector offers as an entity has to survive being turned
// into a discovery message, with a topic all of its own.
func TestEveryDefinitionProducesADistinctTopic(t *testing.T) {
	cfg := testConfig()
	seen := map[string]string{}

	for _, def := range metrics.All {
		if def.NoEntity {
			continue
		}
		instance := ""
		if def.InstanceLabel != "" {
			instance = "0"
		}

		topic, _, err := discoveryMessage(cfg, "", metrics.Reading{Def: def, Instance: instance})
		if err != nil {
			t.Fatalf("discoveryMessage(%s): %v", def.ID, err)
		}
		if other, dup := seen[topic]; dup {
			t.Errorf("%s and %s share the discovery topic %s", def.ID, other, topic)
		}
		seen[topic] = def.ID
	}
}

// The one-off cleanup after a rename has to target the topics the previous
// version actually wrote.
func TestLegacyCleanupTargetsTheOldTopics(t *testing.T) {
	cfg := testConfig()

	topic := cfg.LegacyDiscoveryTopic("sensor", "fps")
	if want := "homeassistant/sensor/" + config.LegacyAppName + "_corganpc2/fps/config"; topic != want {
		t.Errorf("legacy topic = %q, want %q", topic, want)
	}
	if len(legacyKeys) == 0 {
		t.Error("no legacy entities are retired")
	}
}

// The picture is fetched by the browser Home Assistant is open in, not by Home
// Assistant itself. An address of this machine therefore only works from this
// machine: loopback resolves to whoever is looking, and the port is whatever
// the interface happened to get that day. Measured on the live broker: the
// retained state carried no picture at all, because the interface was on
// loopback, and the discovery message next to it still pointed at
// http://127.0.0.1:8788 after the interface had moved off 8787.
func TestThePictureIsTheSameFromEveryMachine(t *testing.T) {
	if !strings.HasPrefix(entityPictureURL, "https://") {
		t.Errorf("picture = %q, and Home Assistant is usually served over https", entityPictureURL)
	}
	for _, local := range []string{"127.0.0.1", "localhost", "0.0.0.0", "192.168.", ":8787", ":8788"} {
		if strings.Contains(entityPictureURL, local) {
			t.Errorf("picture %q carries %q, which only means something on one machine", entityPictureURL, local)
		}
	}
}

// The address is a constant, and a constant rots quietly. It names a file that
// is published with the handbook, so the file has to still be there.
func TestThePictureNamesAFileThatIsPublishedWithTheHandbook(t *testing.T) {
	name := entityPictureURL[strings.LastIndex(entityPictureURL, "/")+1:]
	if _, err := os.Stat(filepath.Join("..", "..", "docs", "images", name)); err != nil {
		t.Errorf("the picture names %s, which is not in docs/images: %v", name, err)
	}
}

// A browser that cannot reach the handbook still needs something to draw, and
// Home Assistant resolves an mdi name inside its own frontend with no request
// going anywhere.
func TestTheUpdateEntityCarriesAnIconForWhenThereIsNoPicture(t *testing.T) {
	payload := updateDiscoveryFor(config.Defaults(), "http://127.0.0.1:8787")
	if payload.Icon != "mdi:speedometer" {
		t.Errorf("icon = %q, want mdi:speedometer", payload.Icon)
	}
}

// The row on the device page says which program is offering the update. It read
// "corgan_pc3 Software", which is true of every update entity ever published.
// The identifiers are not part of the rename — they would orphan the entity.
func TestTheUpdateEntityIsNamedAfterTheProgram(t *testing.T) {
	cfg := config.Defaults()
	payload := updateDiscoveryFor(cfg, "http://127.0.0.1:8787")

	if payload.Name != config.AppName {
		t.Errorf("name = %q, want %q", payload.Name, config.AppName)
	}
	if payload.UniqueID != cfg.UniqueID(updateKey) || payload.ObjectID != cfg.ObjectID(updateKey) {
		t.Errorf("the rename moved an identifier: %+v", payload)
	}
}
