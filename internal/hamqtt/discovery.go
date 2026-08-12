package hamqtt

import (
	"encoding/json"
	"fmt"

	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

const (
	availableOnline  = "online"
	availableOffline = "offline"
	updateKey        = "software"
	installPayload   = "install"
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

type messageExpiryInterval struct {
	Seconds int `json:"seconds"`
}

type availabilityTopic struct {
	Topic               string `json:"topic"`
	PayloadAvailable    string `json:"payload_available"`
	PayloadNotAvailable string `json:"payload_not_available"`
}

// updateDiscoveryPayload is deliberately separate from discoveryPayload:
// Home Assistant's update domain has a command interface and no measurement
// attributes such as units, state classes or device classes.
type updateDiscoveryPayload struct {
	Name                  string                `json:"name"`
	DefaultEntityID       string                `json:"default_entity_id"`
	ObjectID              string                `json:"object_id"`
	UniqueID              string                `json:"unique_id"`
	StateTopic            string                `json:"state_topic"`
	CommandTopic          string                `json:"command_topic"`
	PayloadInstall        string                `json:"payload_install"`
	Availability          []availabilityTopic   `json:"availability"`
	AvailabilityMode      string                `json:"availability_mode"`
	QoS                   byte                  `json:"qos"`
	Retain                bool                  `json:"retain"`
	MessageExpiryInterval messageExpiryInterval `json:"message_expiry_interval"`
	EntityCategory        string                `json:"entity_category"`
	// Icon is what Home Assistant draws when the picture does not arrive — a
	// browser without internet, or a mark that has moved on the handbook. A
	// speedometer, because that is what the mark is.
	Icon   string     `json:"icon,omitempty"`
	Device deviceInfo `json:"device"`
	Origin originInfo `json:"origin"`
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
	DefaultEntityID     string `json:"default_entity_id"`
	ObjectID            string `json:"object_id"`
	UniqueID            string `json:"unique_id"`
	StateTopic          string `json:"state_topic"`
	ValueTemplate       string `json:"value_template"`
	AvailabilityTopic   string `json:"availability_topic"`
	PayloadAvailable    string `json:"payload_available"`
	PayloadNotAvailable string `json:"payload_not_available"`
	UnitOfMeasurement   string `json:"unit_of_measurement,omitempty"`
	DeviceClass         string `json:"device_class,omitempty"`
	StateClass          string `json:"state_class,omitempty"`
	EntityCategory      string `json:"entity_category,omitempty"`
	Icon                string `json:"icon,omitempty"`
	PayloadOn           string `json:"payload_on,omitempty"`
	PayloadOff          string `json:"payload_off,omitempty"`
	SuggestedPrecision  *int   `json:"suggested_display_precision,omitempty"`
	// JSONAttributes carry a table alongside the state. Home Assistant reads
	// them from the same document the state comes from, so a ranked list costs
	// no extra topic and no extra message.
	JSONAttributesTopic    string     `json:"json_attributes_topic,omitempty"`
	JSONAttributesTemplate string     `json:"json_attributes_template,omitempty"`
	Device                 deviceInfo `json:"device"`
	Origin                 originInfo `json:"origin"`
}

// configURL is what the "Visit" link on the Home Assistant device page opens.
//
// webURL is where the interface is really listening, which is not always where
// the configuration says it should be: a busy port makes the server fall back
// to an ephemeral one, and a link to the port nobody is listening on is worse
// than no link. Empty means nothing has reported an address yet, and then the
// configured one is the best guess there is.
func configURL(cfg config.Config, webURL string) string {
	if webURL != "" {
		return webURL
	}
	return cfg.WebURL()
}

func deviceFor(cfg config.Config, webURL string) deviceInfo {
	return deviceInfo{
		Identifiers:  []string{cfg.DeviceIdentifier()},
		Name:         cfg.DeviceName,
		Manufacturer: config.AppName,
		Model:        "PC telemetry",
		SWVersion:    config.Version,
		ConfigURL:    configURL(cfg, webURL),
	}
}

func originFor() originInfo {
	return originInfo{
		Name:       config.AppName,
		SWVersion:  config.Version,
		SupportURL: config.ProjectURL,
	}
}

func discoveryFor(cfg config.Config, webURL string, r metrics.Reading) discoveryPayload {
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
		Device:              deviceFor(cfg, webURL),
		Origin:              originFor(),
	}

	switch r.Def.Kind {
	case metrics.KindBool:
		payload.PayloadOn = metrics.PayloadOn
		payload.PayloadOff = metrics.PayloadOff
	case metrics.KindTable:
		// The state is the leading program's name; the list rides along as
		// attributes, which is where a five-row table can live without becoming
		// five entities whose meaning changes whenever two programs swap places.
		//
		// Same topic, so the whole thing is still one message. tojson because
		// Home Assistant expects a JSON object here and would otherwise receive
		// Python's repr of one, quotes and all.
		payload.ValueTemplate = fmt.Sprintf("{{ value_json.%s.top }}", key)
		payload.JSONAttributesTopic = cfg.StateTopic()
		payload.JSONAttributesTemplate = fmt.Sprintf("{{ value_json.%s | tojson }}", key)
		// No unit: it describes the rows, and Home Assistant would attach it to
		// the state, which is a program name. "firefox.exe %" helps nobody.
		payload.UnitOfMeasurement = ""
	case metrics.KindGauge:
		// Display precision only applies to numbers; setting it on a text
		// entity makes Home Assistant reject the discovery. It is what we
		// actually send, not what the definition would like: promising a
		// decimal that never arrives just renders every value as x.0.
		precision := r.Def.EffectivePrecision()
		payload.SuggestedPrecision = &precision
	}

	// A measurement that names a companion carries that companion's object as
	// its attributes. Same topic and same message as the table above, and the
	// state, the id and the value template are untouched — this adds
	// attributes to an entity that already exists rather than changing it.
	//
	// The default is what makes it survive the ordinary case: the companion key
	// is absent whenever nothing was identified, and a template that renders
	// Undefined would fail on every single message. An empty object is also the
	// right answer rather than merely a safe one — it clears the attributes,
	// so a game that has closed does not leave the last title and app id behind
	// for Home Assistant to keep showing.
	if from := r.Def.AttributesFrom; from != "" && payload.JSONAttributesTopic == "" {
		payload.JSONAttributesTopic = cfg.StateTopic()
		payload.JSONAttributesTemplate = fmt.Sprintf("{{ value_json.%s | default({}) | tojson }}", from)
	}
	return payload
}

func updateDiscoveryFor(cfg config.Config, webURL string) updateDiscoveryPayload {
	return updateDiscoveryPayload{
		// The application name, not "Software". A device page can carry update
		// entities from several sources, and "corgan_pc3 Software" says nothing
		// about which program the row is offering to update. The identifiers
		// below stay as they are, so no entity is orphaned by the rename.
		Name:            config.AppName,
		DefaultEntityID: "update." + cfg.ObjectID(updateKey),
		ObjectID:        cfg.ObjectID(updateKey),
		UniqueID:        cfg.UniqueID(updateKey),
		StateTopic:      cfg.UpdateStateTopic(),
		CommandTopic:    cfg.UpdateCommandTopic(),
		PayloadInstall:  installPayload,
		Availability: []availabilityTopic{
			{Topic: cfg.AvailabilityTopic(), PayloadAvailable: availableOnline, PayloadNotAvailable: availableOffline},
			{Topic: cfg.UpdateAvailabilityTopic(), PayloadAvailable: availableOnline, PayloadNotAvailable: availableOffline},
		},
		AvailabilityMode:      "all",
		QoS:                   1,
		Retain:                false,
		MessageExpiryInterval: messageExpiryInterval{Seconds: 30},
		EntityCategory:        "config",
		Icon:                  updateIcon,
		Device:                deviceFor(cfg, webURL),
		Origin:                originFor(),
	}
}

// updateIcon is the Material Design name Home Assistant resolves itself, with
// no request leaving its own frontend. It is what the card draws for anyone
// whose browser cannot reach entityPictureURL.
const updateIcon = "mdi:speedometer"

// entityPictureURL is where the browser fetches the application mark.
//
// The browser, not Home Assistant: entity_picture is an address the frontend
// puts into the page, so it is resolved wherever somebody is looking. This used
// to be this program's own interface, and that address only ever worked from
// this machine — bound to loopback it resolves to whoever is looking, and the
// port is whatever the interface got that day. Both happened here at once: the
// interface fell back to 8788 because something else held 8787, and it listens
// on loopback, which is the factory setting. The picture was then left out
// altogether and Home Assistant drew its own update icon.
//
// So the mark is taken from where it is published anyway — the handbook, built
// from docs/images and served by GitHub Pages. One address, the same from every
// machine, and no port to go stale. A browser without internet gets no picture
// and falls back to updateIcon, exactly as before.
const entityPictureURL = "https://corgan2222.github.io/rig-exporter/images/rig-exporter-entity-256.png"

func updateDiscoveryMessage(cfg config.Config, webURL string) (string, []byte, error) {
	payload, err := json.Marshal(updateDiscoveryFor(cfg, webURL))
	if err != nil {
		return "", nil, fmt.Errorf("encode software update discovery: %w", err)
	}
	return cfg.DiscoveryTopic("update", updateKey), payload, nil
}

// discoveryMessage returns the topic and payload announcing one reading.
func discoveryMessage(cfg config.Config, webURL string, r metrics.Reading) (string, []byte, error) {
	payload, err := json.Marshal(discoveryFor(cfg, webURL, r))
	if err != nil {
		return "", nil, fmt.Errorf("encode discovery for %s: %w", r.Key(), err)
	}
	return cfg.DiscoveryTopic(r.Def.Component(), r.Key()), payload, nil
}
