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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/export"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/updater"
)

const (
	publishTimeout      = 5 * time.Second
	initialConnectRetry = 10 * time.Second
	disconnectGraceMs   = 500
	keepAlive           = 30 * time.Second
	maxReconnect        = 60 * time.Second
)

type connectionClient interface {
	Connect() mqtt.Token
}

// Publisher owns one MQTT connection. It is safe for concurrent use and
// implements export.Target.
type Publisher struct {
	cfg config.Config
	log *slog.Logger

	// webURL reports where the settings interface is actually listening, asked
	// afresh at every announcement rather than copied at construction: the web
	// server may not have picked its port yet when the publisher is built, and
	// after a fallback that port is not the configured one.
	webURL func() string
	// updates is the updater Module behind its small Controller interface. MQTT
	// is only an adapter at this seam: it exposes state and forwards the exact
	// install command, without learning how releases are selected or applied.
	updates updater.Controller

	published export.Counter

	mu           sync.RWMutex
	updateSync   sync.Mutex
	client       mqtt.Client
	connected    bool
	lastError    string
	updateError  string
	stop         chan struct{}
	stopOnce     sync.Once
	connectRetry time.Duration
	// announced remembers which entity keys have a retained discovery message,
	// so each one is announced once per connection rather than every second.
	announced map[string]EntityRef
	// announcedURL is the interface address those messages carry. The publisher
	// usually connects before the web server has picked its port, so the first
	// announcement goes out with the configured address and the real one only
	// becomes known a moment later.
	announcedURL string
	// updateAnnounced is separate from announced because software is always
	// present and not driven by a collected reading.
	updateAnnounced         bool
	updateSubscribed        bool
	commonAvailable         bool
	updateAvailabilityKnown bool
	updateAvailable         bool
	updateState             updater.State
	updateGeneration        uint64
	updatePublished         uint64
	stopUpdates             func()
	// republish forces the next pass to announce every entity again, even ones
	// already in announced. Separate from that map on purpose: whether the
	// broker needs telling again is a fact about this connection, and what is
	// retained out there is not.
	republish bool
	// legacyCleared records that the one-off retirement of the previous
	// application name's entities completed. Read by the caller that owns the
	// flag in the configuration, so it is only cleared once it really happened.
	legacyCleared bool
	// pendingRetire holds entities to retire that could not be retired yet
	// because the broker was unreachable at the time. Somebody who narrows the
	// sensor set while the broker is down still means it.
	pendingRetire []EntityRef
}

// EntityRef names one entity's discovery topic — everything needed to retire
// it again. Built by EntityRefs; the fields stay unexported so the mapping from
// a reading to a topic has exactly one home.
type EntityRef struct {
	component string
	key       string
	// defID names the measurement behind the entity, so a retirement can be
	// decided from the selection rather than from a reading that may no longer
	// be taken.
	defID string
}

// New builds a publisher for cfg. Nothing happens until Start is called.
//
// webURL may be nil, in which case the device page links to the configured
// address. Everything that has one should pass it: see configURL.
func New(cfg config.Config, log *slog.Logger, webURL func() string, updates updater.Controller) *Publisher {
	return &Publisher{
		cfg: cfg, log: log, webURL: webURL, updates: updates,
		announced: map[string]EntityRef{}, stop: make(chan struct{}),
		connectRetry: initialConnectRetry,
	}
}

// currentWebURL is the interface's real address, empty when nobody reports one.
func (p *Publisher) currentWebURL() string {
	if p.webURL == nil {
		return ""
	}
	return p.webURL()
}

// forgetAnnouncementsIfURLChanged makes the next pass announce everything again
// when the interface address has moved.
//
// The address is part of every discovery message, and those are retained: one
// published with the wrong port sits on the broker until something overwrites
// it. In practice this fires once per run, right after the web server reports
// the port it actually got.
func (p *Publisher) forgetAnnouncementsIfURLChanged(webURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if webURL == p.announcedURL {
		return
	}
	if len(p.announced) > 0 {
		p.log.Info("interface address changed, re-announcing the device link",
			"from", p.announcedURL, "to", webURL, "entities", len(p.announced))
	}
	// Announce again, and keep the list of what is out there — the same
	// distinction onConnect makes. The entities have not gone anywhere; only
	// the address inside their payload is stale.
	p.republish = true
	p.updateAnnounced = false
	p.announcedURL = webURL
}

// Start connects to the broker. It returns as soon as the attempt is under
// way, so a broker that is down at boot does not stop the tray from appearing.
func (p *Publisher) Start() error {
	if p.updates != nil {
		p.mu.Lock()
		needSubscription := p.stopUpdates == nil
		p.mu.Unlock()
		if needSubscription {
			stop := p.updates.Subscribe(p.onUpdateStateChanged)
			p.mu.Lock()
			p.stopUpdates = stop
			p.mu.Unlock()
		}
	}

	opts := mqtt.NewClientOptions().
		AddBroker(p.cfg.BrokerURL()).
		SetClientID(p.cfg.ClientID).
		SetCleanSession(true).
		SetKeepAlive(keepAlive).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(maxReconnect).
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
	go p.connect(client)
	return nil
}

// connect retries the initial connection while keeping the most recent error
// visible. Paho's ConnectRetry deliberately leaves its token pending between
// attempts, so its useful socket or authentication error cannot reach Status.
// AutoReconnect still owns failures after the first successful connection.
func (p *Publisher) connect(client connectionClient) {
	for {
		select {
		case <-p.stop:
			return
		default:
		}

		token := client.Connect()
		select {
		case <-p.stop:
			return
		case <-token.Done():
		}

		if err := token.Error(); err == nil {
			return
		} else {
			p.recordError(fmt.Errorf("connect to broker: %w", err))
		}

		timer := time.NewTimer(p.connectRetry)
		select {
		case <-p.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *Publisher) onConnect(client mqtt.Client) {
	p.mu.Lock()
	select {
	case <-p.stop:
		p.mu.Unlock()
		client.Disconnect(0)
		return
	default:
	}
	if p.client != client {
		p.mu.Unlock()
		client.Disconnect(0)
		return
	}
	p.connected = true
	p.lastError = ""
	p.updateError = ""
	// Republish discovery, but do not forget what is out there.
	//
	// This used to empty p.announced, which had the right effect for the wrong
	// reason: republishing is wanted — a Home Assistant that was reinstalled or
	// had its retained messages purged picks the device up again — but that
	// list is not a record of what this connection did. It is the record of
	// which retained messages lie on the broker, and those outlive the
	// connection and the process. Emptying it left RetireUnselected blind: a
	// drive unplugged after a reconnect had no reference left to retire, and
	// unticking its group cleared nothing.
	p.republish = true
	p.updateAnnounced = false
	p.updateSubscribed = false
	p.commonAvailable = false
	p.updateAvailabilityKnown = false
	p.updateAvailable = false
	p.updatePublished = 0
	p.mu.Unlock()

	p.log.Info("broker connected", "broker", p.cfg.BrokerURL())

	// Without an update adapter the process-wide availability remains the only
	// one. With an adapter, syncUpdateChannel deliberately publishes the update
	// guard offline before this common topic goes online.
	if p.updates == nil {
		if err := publish(client, p.cfg.AvailabilityTopic(), []byte(availableOnline), 1, true); err != nil {
			p.recordError(fmt.Errorf("publish availability: %w", err))
		}
	}
	// Before anything is announced: a retirement decided while the broker was
	// away must not undo an announcement made a moment later.
	p.flushPendingRetire(client)
	if p.cfg.LegacyCleanupPending {
		// The error is logged inside and reported through LegacyCleanupDone,
		// which is what decides whether the flag may come off. A failed pass
		// leaves it set so the next connection tries again.
		_ = p.clearLegacyDiscovery(client)
	}
	if p.updates != nil {
		if err := p.syncUpdateChannel(client); err != nil {
			p.recordUpdateError(err)
		} else {
			p.clearUpdateError()
		}
	}
}

func (p *Publisher) onConnectionLost(_ mqtt.Client, err error) {
	p.mu.Lock()
	p.connected = false
	p.updateSubscribed = false
	p.updateAnnounced = false
	p.commonAvailable = false
	p.updateAvailabilityKnown = false
	p.updateAvailable = false
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
	if p.updates != nil {
		if err := p.syncUpdateChannel(client); err != nil {
			// Telemetry remains useful, but the update entity must stay hidden
			// until its command subscription and retained state are confirmed.
			p.recordUpdateError(err)
		} else {
			p.clearUpdateError()
		}
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

	// A transient publish error must disappear again after the broker accepts a
	// complete telemetry export. Preserve a connection-loss error that may have
	// arrived concurrently after Publish returned. Update-channel errors live
	// separately and can only be cleared by a complete update-channel sync.
	p.mu.Lock()
	if p.connected {
		p.lastError = ""
	}
	p.mu.Unlock()
	p.published.Inc()
	return nil
}

// announceNew publishes discovery for entities that have appeared since the
// last collection. Entities that disappear are left alone: hardware coming and
// going — an external drive, a second monitor — should not make Home Assistant
// forget its history and any dashboard referring to it.
func (p *Publisher) announceNew(client mqtt.Client, snap collector.Snapshot) error {
	webURL := p.currentWebURL()
	p.forgetAnnouncementsIfURLChanged(webURL)

	p.mu.RLock()
	republish := p.republish
	p.mu.RUnlock()

	for _, reading := range snap.Entities() {
		if !announceable(reading) {
			continue
		}

		key := reading.Key()

		p.mu.RLock()
		_, known := p.announced[key]
		p.mu.RUnlock()
		if known && !republish {
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

		// And the same entity under the identity this machine was renamed away
		// from, for as long as one is on record. Same mechanism, one level up:
		// there the key changed shape, here the node id or the prefix did.
		//
		// This is what makes a rename survive an absent broker. The publisher
		// that knew the old identity was stopped the moment the name changed,
		// so the old topics can only be emptied by whoever comes after it —
		// and that one reads the old identity off the configuration.
		for _, topic := range p.previousTopicsFor(reading) {
			if err := publish(client, topic, nil, 1, true); err != nil {
				p.log.Warn("could not retire an entity of the previous identity",
					"topic", topic, "error", err)
			}
		}

		topic, payload, err := discoveryMessage(p.cfg, webURL, reading)
		if err != nil {
			return err
		}
		if err := publish(client, topic, payload, 1, true); err != nil {
			return fmt.Errorf("publish discovery %s: %w", key, err)
		}

		p.mu.Lock()
		p.announced[key] = EntityRef{component: reading.Def.Component(), key: key, defID: reading.Def.ID}
		p.mu.Unlock()

		p.log.Debug("entity announced", "topic", topic)
	}

	// Only after a complete pass. A run that gave up halfway would otherwise
	// leave the rest of the entities unannounced until something else changed.
	if republish {
		p.mu.Lock()
		p.republish = false
		p.mu.Unlock()
	}
	return nil
}

func (p *Publisher) announceUpdate(client mqtt.Client, webURL string) error {
	if p.updates == nil {
		return nil
	}

	p.mu.RLock()
	announced, subscribed := p.updateAnnounced, p.updateSubscribed
	p.mu.RUnlock()
	if !subscribed {
		return fmt.Errorf("software update command subscription is not ready")
	}
	if announced {
		return nil
	}

	topic, payload, err := updateDiscoveryMessage(p.cfg, webURL)
	if err != nil {
		return err
	}
	if err := publish(client, topic, payload, 1, true); err != nil {
		return fmt.Errorf("publish software update discovery: %w", err)
	}

	p.mu.Lock()
	p.updateAnnounced = true
	p.mu.Unlock()
	p.log.Debug("software update announced", "topic", topic)
	return nil
}

// syncUpdateChannel is the single serialized path for subscription,
// discovery, and retained state. A generation loop prevents an older state
// read during reconnect from overwriting a newer updater callback.
func (p *Publisher) syncUpdateChannel(client mqtt.Client) error {
	if p.updates == nil {
		return nil
	}

	p.updateSync.Lock()
	defer p.updateSync.Unlock()

	p.mu.RLock()
	current, connected, subscribed := p.client == client, p.connected, p.updateSubscribed
	p.mu.RUnlock()
	if !current || !connected {
		return nil
	}
	if err := p.ensureCommonAvailabilityForUpdates(client); err != nil {
		return err
	}

	if !subscribed {
		if err := p.publishUpdateAvailability(client, false); err != nil {
			return err
		}
		if err := subscribe(client, p.cfg.UpdateCommandTopic(), 1, p.onUpdateCommand); err != nil {
			return fmt.Errorf("subscribe to software update command: %w", err)
		}
		p.mu.Lock()
		if p.client != client || !p.connected {
			p.mu.Unlock()
			return fmt.Errorf("broker disconnected while subscribing to software updates")
		}
		p.updateSubscribed = true
		p.mu.Unlock()
	}

	webURL := p.currentWebURL()
	p.forgetAnnouncementsIfURLChanged(webURL)

	p.mu.Lock()
	if p.updateGeneration == 0 {
		p.updateState = cloneUpdateState(p.updates.State())
		p.updateGeneration = 1
	}
	p.mu.Unlock()

	for {
		p.mu.RLock()
		ready := p.client == client && p.connected && p.updateSubscribed
		generation, published := p.updateGeneration, p.updatePublished
		announced := p.updateAnnounced
		state := cloneUpdateState(p.updateState)
		p.mu.RUnlock()
		if !ready {
			return nil
		}
		if published < generation {
			if err := p.publishUpdateAvailability(client, false); err != nil {
				return err
			}
			if err := p.publishUpdateState(client, state); err != nil {
				return err
			}
			p.mu.Lock()
			if generation > p.updatePublished {
				p.updatePublished = generation
			}
			p.mu.Unlock()
			continue
		}
		if !announced {
			if err := p.publishUpdateAvailability(client, false); err != nil {
				return err
			}
			if err := p.announceUpdate(client, webURL); err != nil {
				return err
			}
			continue
		}
		return p.publishUpdateAvailability(client, true)
	}
}

// ensureCommonAvailabilityForUpdates establishes the two-topic availability
// contract in a safe order. A retained update-offline guard exists before the
// process-wide last-will topic is allowed online, so an older retained update
// state can never become actionable during reconnect setup.
func (p *Publisher) ensureCommonAvailabilityForUpdates(client mqtt.Client) error {
	p.mu.RLock()
	current := p.client == client && p.connected
	commonAvailable := p.commonAvailable
	p.mu.RUnlock()
	if !current {
		return errors.New("broker disconnected before publishing common availability")
	}
	if commonAvailable {
		return nil
	}
	if err := p.publishUpdateAvailability(client, false); err != nil {
		return err
	}
	if err := publish(client, p.cfg.AvailabilityTopic(), []byte(availableOnline), 1, true); err != nil {
		return fmt.Errorf("publish common availability: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != client || !p.connected {
		return errors.New("broker disconnected while publishing common availability")
	}
	p.commonAvailable = true
	return nil
}

func (p *Publisher) publishUpdateAvailability(client mqtt.Client, available bool) error {
	p.mu.RLock()
	current := p.client == client && p.connected
	known := p.updateAvailabilityKnown && p.updateAvailable == available
	p.mu.RUnlock()
	if !current {
		return errors.New("broker disconnected before publishing software update availability")
	}
	if known {
		return nil
	}
	payload := availableOffline
	if available {
		payload = availableOnline
	}
	if err := publish(client, p.cfg.UpdateAvailabilityTopic(), []byte(payload), 1, true); err != nil {
		return fmt.Errorf("publish software update availability %s: %w", payload, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != client || !p.connected {
		return errors.New("broker disconnected while publishing software update availability")
	}
	p.updateAvailabilityKnown = true
	p.updateAvailable = available
	return nil
}

type updateStatePayload struct {
	InstalledVersion string   `json:"installed_version,omitempty"`
	LatestVersion    string   `json:"latest_version,omitempty"`
	Title            string   `json:"title,omitempty"`
	ReleaseSummary   string   `json:"release_summary,omitempty"`
	ReleaseURL       string   `json:"release_url,omitempty"`
	InProgress       bool     `json:"in_progress"`
	UpdatePercentage *float64 `json:"update_percentage"`
	// EntityPicture is the application mark, served by this program's own web
	// interface. Omitted where that interface cannot be reached from wherever
	// Home Assistant is being looked at; the card then falls back to the icon
	// in the discovery payload, which is a worse picture but never a broken
	// one.
	EntityPicture string `json:"entity_picture,omitempty"`
}

func (p *Publisher) publishUpdateState(client mqtt.Client, state updater.State) error {
	payload, err := json.Marshal(updateStatePayload{
		InstalledVersion: state.InstalledVersion,
		LatestVersion:    state.LatestVersion,
		Title:            state.Title,
		ReleaseSummary:   state.ReleaseSummary,
		ReleaseURL:       state.ReleaseURL,
		InProgress:       state.InProgress,
		UpdatePercentage: state.UpdatePercentage,
		EntityPicture:    iconPictureURL(p.cfg, p.currentWebURL()),
	})
	if err != nil {
		return fmt.Errorf("encode software update state: %w", err)
	}
	if err := publish(client, p.cfg.UpdateStateTopic(), payload, 1, true); err != nil {
		return fmt.Errorf("publish software update state: %w", err)
	}
	return nil
}

func (p *Publisher) onUpdateStateChanged(state updater.State) {
	if state.LastError != "" {
		p.log.Error("software update", "error", state.LastError)
	}

	p.mu.Lock()
	p.updateState = cloneUpdateState(state)
	p.updateGeneration++
	client, connected := p.client, p.connected
	p.mu.Unlock()
	if client == nil || !connected {
		return
	}
	if err := p.syncUpdateChannel(client); err != nil {
		p.recordUpdateError(err)
	} else {
		p.clearUpdateError()
	}
}

func cloneUpdateState(state updater.State) updater.State {
	if state.UpdatePercentage != nil {
		percentage := *state.UpdatePercentage
		state.UpdatePercentage = &percentage
	}
	return state
}

func (p *Publisher) onUpdateCommand(_ mqtt.Client, message mqtt.Message) {
	if p.updates == nil || message.Retained() || message.Topic() != p.cfg.UpdateCommandTopic() || string(message.Payload()) != installPayload {
		return
	}
	if err := p.updates.RequestInstall(); err != nil {
		p.log.Warn("software update request rejected", "error", err)
	}
}

// announceable reports whether a reading may be announced right now.
//
// It exists because exports run on their own goroutine: a snapshot taken before
// the sensor set was narrowed can reach announceNew after the entities it names
// have just been retired, and would put every one of them back — retained, and
// indistinguishable from a genuine announcement. Asking the same package state
// the collector filters on makes a stale snapshot harmless.
//
// Deliberately not read from p.cfg: that copy is frozen when the publisher is
// built and is not refreshed when a configuration change does not rebuild it.
func announceable(r metrics.Reading) bool {
	return metrics.Selected(r.Def.ID)
}

// Retire removes named entities from Home Assistant by emptying their retained
// discovery messages.
//
// This is for a deliberate choice — the user narrowing which measurements are
// reported — and never for a measurement that merely failed to appear. That
// distinction is the whole reason announceNew leaves absent entities alone: a
// drive that is unplugged for an afternoon must not cost anybody their history.
//
// Retiring works whether or not this process announced the entity itself.
// announced is cleared on every reconnect, but the retained message on the
// broker outlives both the reconnect and the process, so what has to be emptied
// is the topic, not a bookkeeping entry. Publishing into a topic that holds
// nothing does nothing.
func (p *Publisher) Retire(refs []EntityRef) {
	if len(refs) == 0 {
		return
	}

	p.mu.Lock()
	client, connected := p.client, p.connected
	for _, ref := range refs {
		delete(p.announced, ref.key)
	}
	if client == nil || !connected {
		p.pendingRetire = append(p.pendingRetire, refs...)
		p.mu.Unlock()
		p.log.Info("entities queued for retirement until the broker is back", "count", len(refs))
		return
	}
	p.mu.Unlock()

	p.retire(client, refs)
}

// RetireUnselected withdraws every entity this publisher has announced whose
// measurement is no longer selected.
//
// The announcement list rather than the last reading, because those are two
// different questions. A reading says what the machine produced a second ago; a
// retained discovery message says what Home Assistant is still being told
// exists. A drive that spun down, a card that stopped answering, a value that
// happened to be missing from one pass — all of them leave an entity behind
// that the reading cannot see and that nothing else would ever clear.
func (p *Publisher) RetireUnselected(selected func(defID string) bool) {
	p.mu.RLock()
	var refs []EntityRef
	for _, ref := range p.announced {
		if ref.defID != "" && !selected(ref.defID) {
			refs = append(refs, ref)
		}
	}
	p.mu.RUnlock()

	p.Retire(refs)
}

// EntityRefs turns readings into what Retire needs. It lives here because the
// mapping from a reading to its discovery topic is this package's business.
func EntityRefs(readings []metrics.Reading) []EntityRef {
	refs := make([]EntityRef, 0, len(readings))
	for _, r := range readings {
		refs = append(refs, EntityRef{component: r.Def.Component(), key: r.Key()})
	}
	return refs
}

func (p *Publisher) retire(client mqtt.Client, refs []EntityRef) {
	failed := 0
	for _, ref := range refs {
		topic := p.cfg.DiscoveryTopic(ref.component, ref.key)
		if err := publish(client, topic, nil, 1, true); err != nil {
			p.log.Warn("could not retire an entity", "topic", topic, "error", err)
			failed++
		}
	}
	p.log.Info("entities retired", "count", len(refs)-failed, "failed", failed)
}

// flushPendingRetire runs the retirements that were queued while the broker was
// away. Called from onConnect, before any discovery is republished, so an
// entity cannot be retired straight after being announced again.
func (p *Publisher) flushPendingRetire(client mqtt.Client) {
	p.mu.Lock()
	refs := p.pendingRetire
	p.pendingRetire = nil
	p.mu.Unlock()

	if len(refs) > 0 {
		p.retire(client, refs)
	}
}

// ClearDiscovery retires every entity this publisher announced, by publishing
// empty retained payloads. Used when the node id or topic prefix changes, so
// the old entities do not linger as unavailable leftovers.
func (p *Publisher) ClearDiscovery() {
	p.updateSync.Lock()
	defer p.updateSync.Unlock()
	p.mu.Lock()
	client, connected := p.client, p.connected
	refs := make([]EntityRef, 0, len(p.announced))
	for _, ref := range p.announced {
		refs = append(refs, ref)
	}
	clearUpdate := p.updates != nil
	p.updateAnnounced = false
	if client == nil || !connected {
		// Queued, the way Retire does it, instead of returning in silence.
		//
		// This runs when the node id or the topic prefix changes. A rename
		// while the broker happened to be restarting used to leave every old
		// retained message where it was, and Home Assistant then kept a second,
		// permanently unavailable device — one that survives being deleted by
		// hand, because the retained config brings it back at the next restart.
		// Only emptying the topics with an MQTT client helped.
		p.pendingRetire = append(p.pendingRetire, refs...)
		if clearUpdate {
			p.pendingRetire = append(p.pendingRetire,
				EntityRef{component: "update", key: updateKey})
		}
		p.announced = map[string]EntityRef{}
		p.mu.Unlock()
		p.log.Warn("discovery cannot be cleared while the broker is away, queued instead",
			"node_id", p.cfg.NodeID, "entities", len(refs))
		return
	}
	p.announced = map[string]EntityRef{}
	p.mu.Unlock()
	for _, ref := range refs {
		topic := p.cfg.DiscoveryTopic(ref.component, ref.key)
		if err := publish(client, topic, nil, 1, true); err != nil {
			p.log.Warn("clear discovery failed", "topic", topic, "error", err)
		}
	}
	entityCount := len(refs)
	if clearUpdate {
		topic := p.cfg.DiscoveryTopic("update", updateKey)
		if err := publish(client, topic, nil, 1, true); err != nil {
			p.log.Warn("clear software update discovery failed", "topic", topic, "error", err)
		}
		entityCount++
	}
	p.log.Info("discovery cleared", "node_id", p.cfg.NodeID, "entities", entityCount)
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

// clearLegacyDiscovery retires them and says whether it got through.
//
// It used to stop at the first failing topic and return nothing, so the caller
// could not tell a clean pass from half of one — and cleared the flag that
// remembers to try again either way. One refused publish then left up to seven
// retained messages of the previous name on the broker for good: a dead device
// carrying sensor.fps, sensor.cpu and binary_sensor.rtss, and nothing left that
// would ever come back for it.
//
// Carrying on past a failure is the point. These eight topics are independent
// of each other; giving up on the rest because one was refused only widens the
// damage.
func (p *Publisher) clearLegacyDiscovery(client mqtt.Client) error {
	var failed []string
	for _, entity := range legacyKeys {
		topic := p.cfg.LegacyDiscoveryTopic(entity.component, entity.key)
		if err := publish(client, topic, nil, 1, true); err != nil {
			p.log.Warn("legacy cleanup failed", "topic", topic, "error", err)
			failed = append(failed, topic)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("legacy cleanup left %d of %d topics behind: %s",
			len(failed), len(legacyKeys), strings.Join(failed, ", "))
	}

	p.mu.Lock()
	p.legacyCleared = true
	p.mu.Unlock()

	p.log.Info("legacy entities retired", "count", len(legacyKeys))
	return nil
}

// previousTopicsFor is where one reading's discovery message would lie under
// the identity this machine was renamed away from.
//
// Both the current and every earlier shape of the key, because a rename and a
// key migration can be pending at the same time — somebody who updates and
// renames in one sitting.
//
// It covers the entities that still exist. One that was announced under the old
// name and is gone by the time the broker returns cannot be reached this way;
// what is on record is an identity, not a list of entities. That is the honest
// limit of the mechanism, and it is the far smaller half of the problem.
func (p *Publisher) previousTopicsFor(reading metrics.Reading) []string {
	if p.cfg.PreviousNodeID == "" {
		return nil
	}

	previous := p.cfg
	previous.NodeID = p.cfg.PreviousNodeID
	if p.cfg.PreviousTopicPrefix != "" {
		previous.TopicPrefix = p.cfg.PreviousTopicPrefix
	}
	if p.cfg.PreviousDiscoveryPrefix != "" {
		previous.DiscoveryPrefix = p.cfg.PreviousDiscoveryPrefix
	}

	component := reading.Def.Component()
	topics := []string{previous.DiscoveryTopic(component, reading.Key())}
	for _, legacy := range reading.LegacyKeys() {
		topics = append(topics, previous.DiscoveryTopic(component, legacy))
	}
	return topics
}

// PreviousIdentityCleared reports whether the entities of the identity this
// machine was renamed away from have been retired, so the note about it may
// come off the configuration.
func (p *Publisher) PreviousIdentityCleared() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg.PreviousNodeID != "" && !p.republish && len(p.announced) > 0
}

// LegacyCleanupDone reports whether the one-off retirement of the previous
// application name's entities has completed. The flag that remembers to do it
// lives in the configuration, and this is what says it may finally come off.
func (p *Publisher) LegacyCleanupDone() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.legacyCleared
}

// Stop announces offline and closes the connection.
func (p *Publisher) Stop() {
	p.stopOnce.Do(func() { close(p.stop) })
	p.updateSync.Lock()
	defer p.updateSync.Unlock()

	p.mu.Lock()
	client := p.client
	stopUpdates := p.stopUpdates
	updateSubscribed := p.updateSubscribed
	p.stopUpdates = nil
	p.client = nil
	p.connected = false
	p.updateSubscribed = false
	p.updateAnnounced = false
	p.commonAvailable = false
	p.updateAvailabilityKnown = false
	p.updateAvailable = false
	p.mu.Unlock()

	if stopUpdates != nil {
		stopUpdates()
	}
	if client == nil {
		return
	}
	if client.IsConnected() {
		if p.updates != nil && updateSubscribed {
			if err := unsubscribe(client, p.cfg.UpdateCommandTopic()); err != nil {
				p.log.Warn("unsubscribe from software update command failed", "error", err)
			}
		}
		if p.updates != nil {
			if err := publish(client, p.cfg.UpdateAvailabilityTopic(), []byte(availableOffline), 1, true); err != nil {
				p.log.Warn("publish software update offline failed", "error", err)
			}
		}
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
	connected, lastError, updateError := p.connected, p.lastError, p.updateError
	p.mu.RUnlock()
	if updateError != "" {
		lastError = updateError
	}

	lang := p.cfg.Lang()
	failed := lastError != ""
	status := export.Status{
		Name:      "mqtt",
		Label:     i18n.T(lang, "export.mqtt"),
		Healthy:   connected && !failed,
		Failed:    failed,
		Delivered: p.published.Count(),
	}
	switch {
	case failed && !connected:
		status.Detail = i18n.T(lang, "export.disconnected") + " · " + lastError
	case failed:
		status.Detail = lastError
	case connected:
		// The entity count used to hang here. It has a chip of its own now:
		// how many entities exist is a fact about the machine, not about this
		// one connection, and it was invisible whenever MQTT was switched off.
		status.Detail = i18n.T(lang, "export.connected") + " · " + p.cfg.BrokerURL()
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

func (p *Publisher) recordUpdateError(err error) {
	p.mu.Lock()
	p.updateError = err.Error()
	p.mu.Unlock()
	p.log.Error("mqtt software update", "error", err)
}

func (p *Publisher) clearUpdateError() {
	p.mu.Lock()
	p.updateError = ""
	p.mu.Unlock()
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

func subscribe(client mqtt.Client, topic string, qos byte, handler mqtt.MessageHandler) error {
	token := client.Subscribe(topic, qos, handler)
	if !token.WaitTimeout(publishTimeout) {
		return fmt.Errorf("timeout subscribing to %s", topic)
	}
	return token.Error()
}

func unsubscribe(client mqtt.Client, topic string) error {
	token := client.Unsubscribe(topic)
	if !token.WaitTimeout(publishTimeout) {
		return fmt.Errorf("timeout unsubscribing from %s", topic)
	}
	return token.Error()
}
