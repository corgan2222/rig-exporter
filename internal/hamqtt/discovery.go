package hamqtt

import (
	"encoding/json"
	"fmt"

	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/metrics"
)

const (
	availableOnline  = "online"
	availableOffline = "offline"

	projectURL = "https://github.com/corgan/rig-exporter"
)

// deviceInfo groups every entity under one device in Home Assistant.
type deviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	SWVersion    string   `json:"sw_version"`
	ConfigURL    string   `json:"configuration_url,omitempty"`
}

// originInfo tells Home Assistant which integration created the entity.
type originInfo struct {
	Name       string `json:"name"`
	SWVersion  string `json:"sw_version"`
	SupportURL string `json:"support_url"`
}

// discoveryPayload is the JSON published to the discovery topic. Fields are
// omitted when empty so a text entity does not advertise a unit.
type discoveryPayload struct {
	Name string `json:"name"`
	// DefaultEntityID is what actually decides the entity id, and it carries
	// the domain: "sensor.re_corganpc2_diskc_busy".
	//
	// object_id used to do this and no longer exists — Home Assistant 2026
	// removed it from the MQTT component altogether, so a payload carrying only
	// object_id gets an entity id built from the device name and the entity
	// name instead. That is how "sensor.corganpc3_busy_c" came about while the
	// payload asked for "re_corganpc3_diskc_busy".
	//
	// Both are sent. Older Home Assistant reads object_id and ignores this;
	// newer reads this and ignores object_id. Neither ever sees a key it
	// understands differently.
	DefaultEntityID     string     `json:"default_entity_id"`
	ObjectID            string     `json:"object_id"`
	UniqueID            string     `json:"unique_id"`
	StateTopic          string     `json:"state_topic"`
	ValueTemplate       string     `json:"value_template"`
	AvailabilityTopic   string     `json:"availability_topic"`
	PayloadAvailable    string     `json:"payload_available"`
	PayloadNotAvailable string     `json:"payload_not_available"`
	UnitOfMeasurement   string     `json:"unit_of_measurement,omitempty"`
	DeviceClass         string     `json:"device_class,omitempty"`
	StateClass          string     `json:"state_class,omitempty"`
	EntityCategory      string     `json:"entity_category,omitempty"`
	Icon                string     `json:"icon,omitempty"`
	PayloadOn           string     `json:"payload_on,omitempty"`
	PayloadOff          string     `json:"payload_off,omitempty"`
	SuggestedPrecision  *int       `json:"suggested_display_precision,omitempty"`
	Device              deviceInfo `json:"device"`
	Origin              originInfo `json:"origin"`
}

// discoveryFor builds the announcement for one reading.
func discoveryFor(cfg config.Config, r metrics.Reading) discoveryPayload {
	key := r.Key()

	payload := discoveryPayload{
		// The name follows the language, the identifiers never do.
		//
		// That split is the whole point: a dashboard, an automation condition
		// and a voice assistant all reference the entity id, and default_entity_id
		// pins it at creation. The name is only what a person reads, so it is
		// free to be German on a German installation — changing it renames
		// nothing that anything else depends on.
		Name:                r.DisplayName(cfg.Lang()),
		DefaultEntityID:     r.Def.Component() + "." + cfg.ObjectID(key),
		ObjectID:            cfg.ObjectID(key),
		UniqueID:            cfg.UniqueID(key),
		StateTopic:          cfg.StateTopic(),
		ValueTemplate:       fmt.Sprintf("{{ value_json.%s }}", key),
		AvailabilityTopic:   cfg.AvailabilityTopic(),
		PayloadAvailable:    availableOnline,
		PayloadNotAvailable: availableOffline,
		UnitOfMeasurement:   r.Def.Unit,
		DeviceClass:         r.Def.DeviceClass,
		StateClass:          r.Def.StateClass,
		EntityCategory:      r.Def.EntityCategory,
		Icon:                r.Def.Icon,
		Device: deviceInfo{
			Identifiers:  []string{cfg.DeviceIdentifier()},
			Name:         cfg.DeviceName,
			Manufacturer: config.AppName,
			Model:        "PC telemetry",
			SWVersion:    config.Version,
			ConfigURL:    cfg.WebURL(),
		},
		Origin: originInfo{
			Name:       config.AppName,
			SWVersion:  config.Version,
			SupportURL: projectURL,
		},
	}

	switch r.Def.Kind {
	case metrics.KindBool:
		payload.PayloadOn = metrics.PayloadOn
		payload.PayloadOff = metrics.PayloadOff
	case metrics.KindGauge:
		// Display precision only applies to numbers; setting it on a text
		// entity makes Home Assistant reject the discovery.
		precision := r.Def.Precision
		payload.SuggestedPrecision = &precision
	}
	return payload
}

// discoveryMessage returns the topic and payload announcing one reading.
func discoveryMessage(cfg config.Config, r metrics.Reading) (string, []byte, error) {
	payload, err := json.Marshal(discoveryFor(cfg, r))
	if err != nil {
		return "", nil, fmt.Errorf("encode discovery for %s: %w", r.Key(), err)
	}
	return cfg.DiscoveryTopic(r.Def.Component(), r.Key()), payload, nil
}
