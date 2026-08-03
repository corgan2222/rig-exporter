package hamqtt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/metrics"
)

func testConfig() config.Config {
	cfg := config.Defaults()
	cfg.NodeID = "corganpc2"
	cfg.DeviceName = "CorganPC2"
	cfg.TopicPrefix = config.AppName
	cfg.DiscoveryPrefix = "homeassistant"
	return cfg
}

func decode(t *testing.T, reading metrics.Reading) (string, discoveryPayload) {
	t.Helper()

	topic, raw, err := discoveryMessage(testConfig(), reading)
	if err != nil {
		t.Fatalf("discoveryMessage(%s): %v", reading.Key(), err)
	}
	var payload discoveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return topic, payload
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

// Home Assistant rejects a text entity that advertises a unit, a state class
// or a display precision.
func TestTextEntitiesOmitNumericAttributes(t *testing.T) {
	for _, reading := range []metrics.Reading{
		metrics.Text(metrics.Game, "", "Cyberpunk2077.exe"),
		metrics.Text(metrics.Resolution, "", "2560x1440"),
		metrics.Text(metrics.DiskMedia, "C:", "NVMe"),
	} {
		_, raw, err := discoveryMessage(testConfig(), reading)
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

		topic, _, err := discoveryMessage(cfg, metrics.Reading{Def: def, Instance: instance})
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
