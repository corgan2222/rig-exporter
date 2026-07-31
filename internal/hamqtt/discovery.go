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
	Name                string     `json:"name"`
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
		// The entity id is language independent; only the name a person reads
		// follows the setting, so switching language renames nothing.
		Name:                r.DisplayName(cfg.Lang()),
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
			Model:        "Gaming PC telemetry",
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
