// Package hamqtt publishes snapshots to an MQTT broker in the shape Home
// Assistant's MQTT discovery expects.
//
// One retained discovery message per entity, one JSON state topic all entities
// read through value templates, and a retained availability topic that doubles
// as the last will so entities go unavailable when this process dies.
//
// Discovery is driven by what was actually collected: a graphics card that
// appears when Afterburner is started gets its entities announced on the next
// collection, without a restart and without entities for hardware that is not
// there.
package hamqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/export"
	"github.com/corgan/rig-exporter/internal/i18n"
)

const (
	publishTimeout    = 5 * time.Second
	disconnectGraceMs = 500
	keepAlive         = 30 * time.Second
	maxReconnect      = 60 * time.Second
)

// Publisher owns one MQTT connection. It is safe for concurrent use and
// implements export.Target.
type Publisher struct {
	cfg config.Config
	log *slog.Logger

	published export.Counter

	mu        sync.RWMutex
	client    mqtt.Client
	connected bool
	lastError string
	// announced remembers which entity keys have a retained discovery message,
	// so each one is announced once per connection rather than every second.
	announced map[string]entityRef
}

// entityRef is what is needed to retire an entity again.
type entityRef struct {
	component string
	key       string
}

// New builds a publisher for cfg. Nothing happens until Start is called.
func New(cfg config.Config, log *slog.Logger) *Publisher {
	return &Publisher{cfg: cfg, log: log, announced: map[string]entityRef{}}
}

// Start connects to the broker. It returns as soon as the attempt is under
// way: paho retries in the background, so a broker that is down at boot does
// not stop the tray from appearing.
func (p *Publisher) Start() error {
	opts := mqtt.NewClientOptions().
		AddBroker(p.cfg.BrokerURL()).
		SetClientID(p.cfg.ClientID).
		SetCleanSession(true).
		SetKeepAlive(keepAlive).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(maxReconnect).
		SetConnectRetry(true).
		SetConnectRetryInterval(10*time.Second).
		SetOrderMatters(false).
		SetWill(p.cfg.AvailabilityTopic(), availableOffline, 1, true).
		SetOnConnectHandler(p.onConnect).
		SetConnectionLostHandler(p.onConnectionLost)

	if p.cfg.MQTTUsername != "" {
		opts.SetUsername(p.cfg.MQTTUsername)
		opts.SetPassword(p.cfg.MQTTPassword)
	}
	if p.cfg.MQTTTLS {
		opts.SetTLSConfig(&tls.Config{
			InsecureSkipVerify: p.cfg.MQTTTLSInsecure, //nolint:gosec // opt-in for self-signed brokers
			MinVersion:         tls.VersionTLS12,
		})
	}

	client := mqtt.NewClient(opts)

	p.mu.Lock()
	p.client = client
	p.mu.Unlock()

	p.log.Info("connecting to broker", "broker", p.cfg.BrokerURL(), "client_id", p.cfg.ClientID)
	client.Connect()
	return nil
}

func (p *Publisher) onConnect(client mqtt.Client) {
	p.mu.Lock()
	p.connected = true
	p.lastError = ""
	// Forget what was announced, so discovery is republished. A Home Assistant
	// that was reinstalled or had its retained messages purged then picks the
	// device up again without restarting rig-exporter.
	p.announced = map[string]entityRef{}
	p.mu.Unlock()

	p.log.Info("broker connected", "broker", p.cfg.BrokerURL())

	// Availability first: Home Assistant marks entities unavailable until it
	// sees this, and discovery arriving into an unavailable device is fine.
	if err := publish(client, p.cfg.AvailabilityTopic(), []byte(availableOnline), 1, true); err != nil {
		p.recordError(fmt.Errorf("publish availability: %w", err))
	}
	if p.cfg.LegacyCleanupPending {
		p.clearLegacyDiscovery(client)
	}
}

func (p *Publisher) onConnectionLost(_ mqtt.Client, err error) {
	p.mu.Lock()
	p.connected = false
	p.lastError = err.Error()
	p.mu.Unlock()
	p.log.Warn("broker connection lost", "error", err)
}

// Export announces any entity that is new since the last collection and then
// publishes the state document.
func (p *Publisher) Export(snap collector.Snapshot) error {
	p.mu.RLock()
	client, connected := p.client, p.connected
	p.mu.RUnlock()

	if client == nil || !connected {
		return nil
	}

	if err := p.announceNew(client, snap); err != nil {
		p.recordError(err)
		return err
	}

	payload, err := json.Marshal(snap.JSON())
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := publish(client, p.cfg.StateTopic(), payload, 0, false); err != nil {
		p.recordError(err)
		return fmt.Errorf("publish state: %w", err)
	}

	p.published.Inc()
	return nil
}

// announceNew publishes discovery for entities that have appeared since the
// last collection. Entities that disappear are left alone: hardware coming and
// going — an external drive, a second monitor — should not make Home Assistant
// forget its history and any dashboard referring to it.
func (p *Publisher) announceNew(client mqtt.Client, snap collector.Snapshot) error {
	for _, reading := range snap.Entities() {
		key := reading.Key()

		p.mu.RLock()
		_, known := p.announced[key]
		p.mu.RUnlock()
		if known {
			continue
		}

		// Identifiers have changed shape twice, and every earlier form may still
		// hold a retained discovery message on the broker. Retained messages
		// outlive this program and survive deleting the entity by hand in Home
		// Assistant — the entity simply comes back when Home Assistant next
		// restarts. Each old name is therefore retired explicitly.
		//
		// Publishing into a topic that never existed does nothing, so this needs
		// no migration flag and no memory of having run.
		for _, legacy := range reading.LegacyKeys() {
			legacyTopic := p.cfg.DiscoveryTopic(reading.Def.Component(), legacy)
			if err := publish(client, legacyTopic, nil, 1, true); err != nil {
				p.log.Warn("could not retire a previous entity name",
					"topic", legacyTopic, "error", err)
			}
		}

		topic, payload, err := discoveryMessage(p.cfg, reading)
		if err != nil {
			return err
		}
		if err := publish(client, topic, payload, 1, true); err != nil {
			return fmt.Errorf("publish discovery %s: %w", key, err)
		}

		p.mu.Lock()
		p.announced[key] = entityRef{component: reading.Def.Component(), key: key}
		p.mu.Unlock()

		p.log.Debug("entity announced", "topic", topic)
	}
	return nil
}

// ClearDiscovery retires every entity this publisher announced, by publishing
// empty retained payloads. Used when the node id or topic prefix changes, so
// the old entities do not linger as unavailable leftovers.
func (p *Publisher) ClearDiscovery() {
	p.mu.RLock()
	client, connected := p.client, p.connected
	refs := make([]entityRef, 0, len(p.announced))
	for _, ref := range p.announced {
		refs = append(refs, ref)
	}
	p.mu.RUnlock()

	if client == nil || !connected {
		return
	}
	for _, ref := range refs {
		topic := p.cfg.DiscoveryTopic(ref.component, ref.key)
		if err := publish(client, topic, nil, 1, true); err != nil {
			p.log.Warn("clear discovery failed", "topic", topic, "error", err)
		}
	}
	p.log.Info("discovery cleared", "node_id", p.cfg.NodeID, "entities", len(refs))
}

// legacyKeys are the entities the previous application name published. They
// are retired once, after a configuration migration, because their retained
// discovery messages would otherwise keep a dead device in Home Assistant
// forever.
var legacyKeys = []struct{ component, key string }{
	{"sensor", "fps"}, {"sensor", "frametime"}, {"sensor", "game"},
	{"sensor", "resolution"}, {"sensor", "refresh_rate"},
	{"sensor", "cpu"}, {"sensor", "ram"}, {"binary_sensor", "rtss"},
}

func (p *Publisher) clearLegacyDiscovery(client mqtt.Client) {
	for _, entity := range legacyKeys {
		topic := p.cfg.LegacyDiscoveryTopic(entity.component, entity.key)
		if err := publish(client, topic, nil, 1, true); err != nil {
			p.log.Warn("legacy cleanup failed", "topic", topic, "error", err)
			return
		}
	}
	p.log.Info("legacy entities retired", "count", len(legacyKeys))
}

// Stop announces offline and closes the connection.
func (p *Publisher) Stop() {
	p.mu.Lock()
	client := p.client
	p.client = nil
	p.connected = false
	p.mu.Unlock()

	if client == nil {
		return
	}
	if client.IsConnected() {
		if err := publish(client, p.cfg.AvailabilityTopic(), []byte(availableOffline), 1, true); err != nil {
			p.log.Warn("publish offline failed", "error", err)
		}
	}
	client.Disconnect(disconnectGraceMs)
	p.log.Info("broker disconnected")
}

// Status reports the current connection state.
func (p *Publisher) Status() export.Status {
	p.mu.RLock()
	connected, lastError, entities := p.connected, p.lastError, len(p.announced)
	p.mu.RUnlock()

	lang := p.cfg.Lang()
	status := export.Status{
		Name:      "mqtt",
		Label:     i18n.T(lang, "export.mqtt"),
		Healthy:   connected,
		Delivered: p.published.Count(),
	}
	switch {
	case connected:
		status.Detail = fmt.Sprintf("%s · %s · %d %s",
			i18n.T(lang, "export.connected"), p.cfg.BrokerURL(), entities, i18n.T(lang, "export.entities"))
	case lastError != "":
		status.Detail = i18n.T(lang, "export.disconnected") + " · " + lastError
	default:
		status.Detail = i18n.T(lang, "export.connecting") + " · " + p.cfg.BrokerURL()
	}
	return status
}

func (p *Publisher) recordError(err error) {
	p.mu.Lock()
	p.lastError = err.Error()
	p.mu.Unlock()
	p.log.Error("mqtt", "error", err)
}

// publish waits for the broker to acknowledge, so callers see failures
// instead of silently dropping messages into paho's queue.
func publish(client mqtt.Client, topic string, payload []byte, qos byte, retain bool) error {
	token := client.Publish(topic, qos, retain, payload)
	if !token.WaitTimeout(publishTimeout) {
		return fmt.Errorf("timeout publishing to %s", topic)
	}
	return token.Error()
}
