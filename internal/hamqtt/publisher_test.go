package hamqtt

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/updater"
)

type finishedToken struct {
	err error
}

func (t finishedToken) Wait() bool                     { return true }
func (t finishedToken) WaitTimeout(time.Duration) bool { return true }
func (t finishedToken) Error() error                   { return t.err }
func (t finishedToken) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

type fakeMQTTClient struct {
	connectErrors   []error
	subscribeErrors []error
	publishErrors   map[string][]error
	connectCalls    int
	connected       bool
	publishes       int
	disconnects     int
	subscriptions   []mqttSubscription
	unsubscribed    []string
	messages        []mqttPublish
	events          []string
}

type mqttSubscription struct {
	topic   string
	qos     byte
	handler mqtt.MessageHandler
}

type mqttPublish struct {
	topic   string
	qos     byte
	retain  bool
	payload []byte
}

func (c *fakeMQTTClient) IsConnected() bool      { return c.connected }
func (c *fakeMQTTClient) IsConnectionOpen() bool { return c.connected }
func (c *fakeMQTTClient) Connect() mqtt.Token {
	c.connectCalls++
	var err error
	if len(c.connectErrors) > 0 {
		err = c.connectErrors[0]
		c.connectErrors = c.connectErrors[1:]
	}
	return finishedToken{err: err}
}
func (c *fakeMQTTClient) Disconnect(uint) {
	c.connected = false
	c.disconnects++
}
func (c *fakeMQTTClient) Publish(topic string, qos byte, retain bool, payload interface{}) mqtt.Token {
	c.publishes++
	var body []byte
	switch value := payload.(type) {
	case []byte:
		body = append([]byte(nil), value...)
	case string:
		body = []byte(value)
	case nil:
		body = nil
	}
	c.messages = append(c.messages, mqttPublish{topic: topic, qos: qos, retain: retain, payload: body})
	c.events = append(c.events, "publish:"+topic)
	var err error
	if failures := c.publishErrors[topic]; len(failures) > 0 {
		err = failures[0]
		c.publishErrors[topic] = failures[1:]
	}
	return finishedToken{err: err}
}
func (c *fakeMQTTClient) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) mqtt.Token {
	c.subscriptions = append(c.subscriptions, mqttSubscription{topic: topic, qos: qos, handler: handler})
	c.events = append(c.events, "subscribe:"+topic)
	var err error
	if len(c.subscribeErrors) > 0 {
		err = c.subscribeErrors[0]
		c.subscribeErrors = c.subscribeErrors[1:]
	}
	return finishedToken{err: err}
}
func (*fakeMQTTClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	return finishedToken{}
}
func (c *fakeMQTTClient) Unsubscribe(topics ...string) mqtt.Token {
	c.unsubscribed = append(c.unsubscribed, topics...)
	return finishedToken{}
}
func (*fakeMQTTClient) AddRoute(string, mqtt.MessageHandler) {}
func (*fakeMQTTClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.NewOptionsReader(mqtt.NewClientOptions())
}

type blockingUpdateClient struct {
	*fakeMQTTClient
	stateTopic string
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (c *blockingUpdateClient) Publish(topic string, qos byte, retain bool, payload interface{}) mqtt.Token {
	if topic == c.stateTopic {
		c.once.Do(func() {
			close(c.started)
			<-c.release
		})
	}
	return c.fakeMQTTClient.Publish(topic, qos, retain, payload)
}

type fakeUpdateController struct {
	mu           sync.Mutex
	state        updater.State
	listener     func(updater.State)
	installs     int
	installErr   error
	unsubscribed int
}

func (u *fakeUpdateController) State() updater.State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

func (u *fakeUpdateController) Subscribe(listener func(updater.State)) func() {
	u.mu.Lock()
	u.listener = listener
	state := u.state
	u.mu.Unlock()
	listener(state)
	return func() {
		u.mu.Lock()
		u.listener = nil
		u.unsubscribed++
		u.mu.Unlock()
	}
}

func (u *fakeUpdateController) RequestInstall() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.installs++
	return u.installErr
}

func (u *fakeUpdateController) change(state updater.State) {
	u.mu.Lock()
	u.state = state
	listener := u.listener
	u.mu.Unlock()
	if listener != nil {
		listener(state)
	}
}

type fakeMQTTMessage struct {
	retained bool
	topic    string
	payload  []byte
}

func eventIndex(events []string, wanted string) int {
	for index, event := range events {
		if event == wanted {
			return index
		}
	}
	return len(events)
}

func eventLastIndex(events []string, wanted string) int {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index] == wanted {
			return index
		}
	}
	return len(events)
}

func (m fakeMQTTMessage) Duplicate() bool   { return false }
func (m fakeMQTTMessage) Qos() byte         { return 1 }
func (m fakeMQTTMessage) Retained() bool    { return m.retained }
func (m fakeMQTTMessage) Topic() string     { return m.topic }
func (m fakeMQTTMessage) MessageID() uint16 { return 1 }
func (m fakeMQTTMessage) Payload() []byte   { return m.payload }
func (m fakeMQTTMessage) Ack()              {}

func TestConnectMakesTheNativeUpdateEntityReady(t *testing.T) {
	progress := 42.5
	controller := &fakeUpdateController{state: updater.State{
		InstalledVersion: "1.6.3",
		LatestVersion:    "1.6.4",
		Title:            "rig-exporter 1.6.4",
		ReleaseSummary:   "GPU detection and MQTT updates.",
		ReleaseURL:       "https://github.com/corgan2222/rig-exporter/releases/tag/v1.6.4",
		LastError:        "must stay local",
		InProgress:       true,
		UpdatePercentage: &progress,
	}}
	cfg := config.Defaults()
	client := &fakeMQTTClient{connected: true}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client

	publisher.onConnect(client)

	if len(client.subscriptions) != 1 {
		t.Fatalf("subscriptions = %d, want the update command", len(client.subscriptions))
	}
	subscription := client.subscriptions[0]
	if subscription.topic != cfg.UpdateCommandTopic() || subscription.qos != 1 {
		t.Errorf("subscription = %q qos %d", subscription.topic, subscription.qos)
	}
	offline := eventIndex(client.events, "publish:"+cfg.UpdateAvailabilityTopic())
	commonOnline := eventIndex(client.events, "publish:"+cfg.AvailabilityTopic())
	subscribed := eventIndex(client.events, "subscribe:"+cfg.UpdateCommandTopic())
	statePublished := eventIndex(client.events, "publish:"+cfg.UpdateStateTopic())
	discovered := eventIndex(client.events, "publish:"+cfg.DiscoveryTopic("update", updateKey))
	updateOnline := eventLastIndex(client.events, "publish:"+cfg.UpdateAvailabilityTopic())
	if !(offline < commonOnline && commonOnline < subscribed && subscribed < statePublished &&
		statePublished < discovered && discovered < updateOnline) {
		t.Errorf("broker operations = %v, want update-offline, common-online, subscription, state, discovery, update-online", client.events)
	}

	var discovery, state *mqttPublish
	for index := range client.messages {
		message := &client.messages[index]
		switch message.topic {
		case cfg.DiscoveryTopic("update", updateKey):
			discovery = message
		case cfg.UpdateStateTopic():
			state = message
		}
	}
	if discovery == nil || discovery.qos != 1 || !discovery.retain {
		t.Fatalf("retained update discovery was not published: %#v", discovery)
	}
	if state == nil || state.qos != 1 || !state.retain {
		t.Fatalf("retained update state was not published: %#v", state)
	}
	var availability []mqttPublish
	for _, message := range client.messages {
		if message.topic == cfg.UpdateAvailabilityTopic() {
			availability = append(availability, message)
		}
	}
	if len(availability) != 2 || string(availability[0].payload) != availableOffline ||
		string(availability[1].payload) != availableOnline || !availability[0].retain || !availability[1].retain {
		t.Fatalf("update availability = %#v, want retained offline then online", availability)
	}

	var payload map[string]any
	if err := json.Unmarshal(state.payload, &payload); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if payload["installed_version"] != "1.6.3" || payload["latest_version"] != "1.6.4" {
		t.Errorf("versions = %v -> %v", payload["installed_version"], payload["latest_version"])
	}
	if payload["release_summary"] != controller.state.ReleaseSummary || payload["release_url"] != controller.state.ReleaseURL {
		t.Errorf("release notes missing from state: %s", state.payload)
	}
	if payload["in_progress"] != true || payload["update_percentage"] != progress {
		t.Errorf("progress missing from state: %s", state.payload)
	}
	if _, leaked := payload["last_error"]; leaked {
		t.Errorf("non-standard last_error leaked into the native update state: %s", state.payload)
	}
}

func TestEveryReconnectRestoresTheSoftwareUpdateEntity(t *testing.T) {
	controller := &fakeUpdateController{state: updater.State{InstalledVersion: "1.6.3", LatestVersion: "1.6.4"}}
	cfg := config.Defaults()
	client := &fakeMQTTClient{connected: true}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client

	publisher.onConnect(client)
	firstConnectOperations := len(client.events)
	publisher.onConnect(client)

	if len(client.subscriptions) != 2 {
		t.Fatalf("subscriptions after two connects = %d, want 2", len(client.subscriptions))
	}
	reconnectEvents := client.events[firstConnectOperations:]
	offline := eventIndex(reconnectEvents, "publish:"+cfg.UpdateAvailabilityTopic())
	commonOnline := eventIndex(reconnectEvents, "publish:"+cfg.AvailabilityTopic())
	subscribed := eventIndex(reconnectEvents, "subscribe:"+cfg.UpdateCommandTopic())
	statePublished := eventIndex(reconnectEvents, "publish:"+cfg.UpdateStateTopic())
	discovered := eventIndex(reconnectEvents, "publish:"+cfg.DiscoveryTopic("update", updateKey))
	updateOnline := eventLastIndex(reconnectEvents, "publish:"+cfg.UpdateAvailabilityTopic())
	if !(offline < commonOnline && commonOnline < subscribed && subscribed < statePublished &&
		statePublished < discovered && discovered < updateOnline) {
		t.Errorf("reconnect operations = %v, want the complete safe update bootstrap order", reconnectEvents)
	}
	var discoveries, states int
	for _, message := range client.messages {
		switch message.topic {
		case cfg.DiscoveryTopic("update", updateKey):
			discoveries++
		case cfg.UpdateStateTopic():
			states++
		}
	}
	if discoveries != 2 || states != 2 {
		t.Errorf("two connects published %d discoveries and %d states, want 2 each", discoveries, states)
	}
}

func TestARejectedUpdateSubscriptionKeepsTheUpdateUnavailableAndRetries(t *testing.T) {
	controller := &fakeUpdateController{state: updater.State{
		InstalledVersion: "1.6.3",
		LatestVersion:    "1.6.4",
	}}
	cfg := config.Defaults()
	client := &fakeMQTTClient{
		connected:       true,
		subscribeErrors: []error{errors.New("subscription denied"), nil},
	}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client

	publisher.onConnect(client)
	var availability []mqttPublish
	for _, message := range client.messages {
		if message.topic == cfg.DiscoveryTopic("update", updateKey) || message.topic == cfg.UpdateStateTopic() {
			t.Fatalf("update channel was published without a command subscription: %q", message.topic)
		}
		if message.topic == cfg.UpdateAvailabilityTopic() {
			availability = append(availability, message)
		}
	}
	if len(availability) != 1 || string(availability[0].payload) != availableOffline || !availability[0].retain {
		t.Fatalf("availability after rejected subscription = %#v, want retained offline", availability)
	}
	if status := publisher.Status(); !status.Failed || !strings.Contains(status.Detail, "subscription denied") {
		t.Errorf("status after rejected subscription = %#v", status)
	}

	retryStart := len(client.events)
	if err := publisher.Export(collector.Snapshot{}); err != nil {
		t.Fatalf("Export retry: %v", err)
	}
	if len(client.subscriptions) != 2 {
		t.Fatalf("subscription attempts = %d, want 2", len(client.subscriptions))
	}
	var discovery, state bool
	for _, message := range client.messages {
		discovery = discovery || message.topic == cfg.DiscoveryTopic("update", updateKey)
		state = state || message.topic == cfg.UpdateStateTopic()
	}
	if !discovery || !state {
		t.Errorf("successful retry published discovery=%v state=%v", discovery, state)
	}
	retryEvents := client.events[retryStart:]
	subscribed := eventIndex(retryEvents, "subscribe:"+cfg.UpdateCommandTopic())
	statePublished := eventIndex(retryEvents, "publish:"+cfg.UpdateStateTopic())
	discovered := eventIndex(retryEvents, "publish:"+cfg.DiscoveryTopic("update", updateKey))
	updateOnline := eventIndex(retryEvents, "publish:"+cfg.UpdateAvailabilityTopic())
	if !(subscribed < statePublished && statePublished < discovered && discovered < updateOnline) {
		t.Errorf("retry operations = %v, want subscription, state, discovery, update-online", retryEvents)
	}
	var lastAvailability *mqttPublish
	for index := len(client.messages) - 1; index >= 0; index-- {
		if client.messages[index].topic == cfg.UpdateAvailabilityTopic() {
			lastAvailability = &client.messages[index]
			break
		}
	}
	if lastAvailability == nil || string(lastAvailability.payload) != availableOnline || !lastAvailability.retain {
		t.Errorf("last update availability = %#v, want retained online", lastAvailability)
	}
	if status := publisher.Status(); !status.Healthy || status.Failed {
		t.Errorf("status after successful retry = %#v", status)
	}
}

func TestAFailedUpdateStatePublishIsRetried(t *testing.T) {
	controller := &fakeUpdateController{state: updater.State{
		InstalledVersion: "1.6.3",
		LatestVersion:    "1.6.4",
	}}
	cfg := config.Defaults()
	client := &fakeMQTTClient{
		connected: true,
		publishErrors: map[string][]error{
			cfg.UpdateStateTopic(): {errors.New("state publish denied"), nil},
		},
	}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client
	publisher.onConnect(client)

	for _, message := range client.messages {
		if message.topic == cfg.DiscoveryTopic("update", updateKey) {
			t.Fatal("update discovery was published after its retained state failed")
		}
	}
	var availability []mqttPublish
	for _, message := range client.messages {
		if message.topic == cfg.UpdateAvailabilityTopic() {
			availability = append(availability, message)
		}
	}
	if len(availability) != 1 || string(availability[0].payload) != availableOffline {
		t.Fatalf("availability after failed state = %#v, want offline", availability)
	}
	if status := publisher.Status(); !status.Failed || !strings.Contains(status.Detail, "state publish denied") {
		t.Errorf("status after failed state = %#v", status)
	}
	retryStart := len(client.events)
	if err := publisher.Export(collector.Snapshot{}); err != nil {
		t.Fatalf("Export retry: %v", err)
	}
	var attempts int
	for _, message := range client.messages {
		if message.topic == cfg.UpdateStateTopic() {
			attempts++
		}
	}
	if attempts != 2 {
		t.Errorf("update state attempts = %d, want 2", attempts)
	}
	retryEvents := client.events[retryStart:]
	statePublished := eventIndex(retryEvents, "publish:"+cfg.UpdateStateTopic())
	discovered := eventIndex(retryEvents, "publish:"+cfg.DiscoveryTopic("update", updateKey))
	updateOnline := eventIndex(retryEvents, "publish:"+cfg.UpdateAvailabilityTopic())
	if !(statePublished < discovered && discovered < updateOnline) {
		t.Errorf("state retry operations = %v, want state, discovery, update-online", retryEvents)
	}
	if status := publisher.Status(); !status.Healthy || status.Failed {
		t.Errorf("status after state retry = %#v", status)
	}
}

func TestConcurrentUpdateChangesCannotBeOverwrittenByAnOlderState(t *testing.T) {
	cfg := config.Defaults()
	controller := &fakeUpdateController{}
	client := &blockingUpdateClient{
		fakeMQTTClient: &fakeMQTTClient{connected: true},
		stateTopic:     cfg.UpdateStateTopic(),
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client
	publisher.connected = true
	publisher.updateSubscribed = true
	publisher.updateAnnounced = true
	publisher.updateState = updater.State{InstalledVersion: "1.6.3", LatestVersion: "1.6.4"}
	publisher.updateGeneration = 1

	firstDone := make(chan error, 1)
	go func() { firstDone <- publisher.syncUpdateChannel(client) }()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("first retained update state did not start")
	}

	changeDone := make(chan struct{})
	go func() {
		publisher.onUpdateStateChanged(updater.State{
			InstalledVersion: "1.6.3",
			LatestVersion:    "1.6.5",
		})
		close(changeDone)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		publisher.mu.RLock()
		generation := publisher.updateGeneration
		publisher.mu.RUnlock()
		if generation == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new updater generation was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	close(client.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	select {
	case <-changeDone:
	case <-time.After(time.Second):
		t.Fatal("new update state remained blocked")
	}

	var states []updateStatePayload
	for _, message := range client.messages {
		if message.topic != cfg.UpdateStateTopic() {
			continue
		}
		var state updateStatePayload
		if err := json.Unmarshal(message.payload, &state); err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	if len(states) != 2 {
		t.Fatalf("retained state publishes = %d, want old then new", len(states))
	}
	if states[len(states)-1].LatestVersion != "1.6.5" {
		t.Errorf("last retained version = %q, want newest 1.6.5", states[len(states)-1].LatestVersion)
	}
}

// The mark has to reach Home Assistant on the machine this actually runs on:
// interface on loopback, which is the factory setting, and on a port it fell
// back to because something else held the configured one. That was the live
// case, and the retained state on the broker carried no picture at all — so
// Home Assistant drew its own update icon.
func TestTheRetainedUpdateStateCarriesTheMarkOnALoopbackInterface(t *testing.T) {
	cfg := config.Defaults()
	if cfg.WebBindAll {
		t.Fatal("the interface is on the network from the factory, so this proves nothing")
	}
	client := &fakeMQTTClient{connected: true}
	publisher := New(cfg, applog.Discard(), func() string { return "http://127.0.0.1:8788" }, nil)
	publisher.client = client
	publisher.connected = true

	if err := publisher.publishUpdateState(client, updater.State{
		InstalledVersion: "1.9.4", LatestVersion: "1.10.0",
	}); err != nil {
		t.Fatalf("publishUpdateState: %v", err)
	}

	var state updateStatePayload
	for _, message := range client.messages {
		if message.topic != cfg.UpdateStateTopic() {
			continue
		}
		if err := json.Unmarshal(message.payload, &state); err != nil {
			t.Fatal(err)
		}
	}
	if state.EntityPicture == "" {
		t.Fatal("no picture in the retained state, so the card falls back to an icon")
	}
	for _, local := range []string{"127.0.0.1", "localhost", "8788"} {
		if strings.Contains(state.EntityPicture, local) {
			t.Errorf("picture %q only works from this machine", state.EntityPicture)
		}
	}
}

func TestOnlyAFreshExactInstallCommandStartsAnUpdate(t *testing.T) {
	controller := &fakeUpdateController{state: updater.State{InstalledVersion: "1.6.3", LatestVersion: "1.6.4"}}
	cfg := config.Defaults()
	client := &fakeMQTTClient{connected: true}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client
	publisher.onConnect(client)

	if len(client.subscriptions) != 1 || client.subscriptions[0].handler == nil {
		t.Fatal("the update command has no handler")
	}
	handle := client.subscriptions[0].handler
	for _, message := range []fakeMQTTMessage{
		{topic: cfg.UpdateCommandTopic(), payload: []byte("install"), retained: true},
		{topic: cfg.UpdateCommandTopic(), payload: []byte("install\n")},
		{topic: cfg.UpdateCommandTopic(), payload: []byte("INSTALL")},
		{topic: cfg.UpdateCommandTopic() + "/other", payload: []byte("install")},
	} {
		handle(client, message)
	}
	if controller.installs != 0 {
		t.Errorf("ignored commands started %d installs", controller.installs)
	}

	handle(client, fakeMQTTMessage{topic: cfg.UpdateCommandTopic(), payload: []byte("install")})
	if controller.installs != 1 {
		t.Errorf("exact install command started %d installs, want 1", controller.installs)
	}
}

func TestUpdateStateChangesArePublishedWithoutWaitingForTelemetry(t *testing.T) {
	cfg := config.Defaults()
	cfg.MQTTHost = "127.0.0.1"
	cfg.MQTTPort = 1
	controller := &fakeUpdateController{state: updater.State{InstalledVersion: "1.6.3", LatestVersion: "1.6.3"}}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.connectRetry = time.Hour
	if err := publisher.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := &fakeMQTTClient{connected: true}
	publisher.mu.Lock()
	publisher.client = client
	publisher.connected = true
	publisher.mu.Unlock()

	controller.change(updater.State{
		InstalledVersion: "1.6.3",
		LatestVersion:    "1.6.4",
		ReleaseSummary:   "Available now.",
	})

	offline := eventIndex(client.events, "publish:"+cfg.UpdateAvailabilityTopic())
	commonOnline := eventIndex(client.events, "publish:"+cfg.AvailabilityTopic())
	subscribed := eventIndex(client.events, "subscribe:"+cfg.UpdateCommandTopic())
	statePublished := eventIndex(client.events, "publish:"+cfg.UpdateStateTopic())
	discovered := eventIndex(client.events, "publish:"+cfg.DiscoveryTopic("update", updateKey))
	updateOnline := eventLastIndex(client.events, "publish:"+cfg.UpdateAvailabilityTopic())
	if !(offline < commonOnline && commonOnline < subscribed && subscribed < statePublished &&
		statePublished < discovered && discovered < updateOnline) {
		t.Fatalf("state-change operations = %v, want the safe update bootstrap order", client.events)
	}
	var stateMessage *mqttPublish
	for index := range client.messages {
		if client.messages[index].topic == cfg.UpdateStateTopic() {
			stateMessage = &client.messages[index]
		}
	}
	if stateMessage == nil || stateMessage.qos != 1 || !stateMessage.retain {
		t.Fatalf("retained update state was not published: %#v", stateMessage)
	}
	if !json.Valid(stateMessage.payload) {
		t.Errorf("state payload is not JSON: %q", stateMessage.payload)
	}

	stopStart := len(client.messages)
	publisher.Stop()
	if controller.unsubscribed != 1 {
		t.Errorf("updater listener was released %d times, want 1", controller.unsubscribed)
	}
	if len(client.unsubscribed) != 1 || client.unsubscribed[0] != cfg.UpdateCommandTopic() {
		t.Errorf("MQTT unsubscribe = %v", client.unsubscribed)
	}
	stopMessages := client.messages[stopStart:]
	if len(stopMessages) != 2 || stopMessages[0].topic != cfg.UpdateAvailabilityTopic() ||
		string(stopMessages[0].payload) != availableOffline || !stopMessages[0].retain ||
		stopMessages[1].topic != cfg.AvailabilityTopic() || string(stopMessages[1].payload) != availableOffline ||
		!stopMessages[1].retain {
		t.Errorf("stop publishes = %#v, want retained update-offline then common-offline", stopMessages)
	}
}

func TestTheSoftwareUpdateDeviceLinkIsRepublishedOnlyAfterItMoves(t *testing.T) {
	cfg := config.Defaults()
	webURL := "http://127.0.0.1:8787"
	controller := &fakeUpdateController{state: updater.State{InstalledVersion: "1.6.3", LatestVersion: "1.6.4"}}
	publisher := New(cfg, applog.Discard(), func() string { return webURL }, controller)
	client := &fakeMQTTClient{connected: true}
	publisher.client = client
	publisher.onConnect(client)
	client.messages = nil

	if err := publisher.Export(collector.Snapshot{}); err != nil {
		t.Fatalf("Export at unchanged URL: %v", err)
	}
	for _, message := range client.messages {
		if message.topic == cfg.DiscoveryTopic("update", updateKey) {
			t.Error("an unchanged interface address republished update discovery")
		}
	}

	webURL = "http://127.0.0.1:48352"
	client.messages = nil
	if err := publisher.Export(collector.Snapshot{}); err != nil {
		t.Fatalf("Export after URL change: %v", err)
	}
	var updateMessages []mqttPublish
	for _, message := range client.messages {
		if message.topic == cfg.DiscoveryTopic("update", updateKey) {
			updateMessages = append(updateMessages, message)
		}
	}
	if len(updateMessages) != 1 {
		t.Fatalf("moved address produced %d update discovery messages, want 1", len(updateMessages))
	}
	var discovery updateDiscoveryPayload
	if err := json.Unmarshal(updateMessages[0].payload, &discovery); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	if discovery.Device.ConfigURL != webURL {
		t.Errorf("configuration_url = %q, want %q", discovery.Device.ConfigURL, webURL)
	}
}

func TestClearDiscoveryAlsoRetiresTheSoftwareUpdateEntity(t *testing.T) {
	cfg := config.Defaults()
	controller := &fakeUpdateController{state: updater.State{InstalledVersion: "1.6.3", LatestVersion: "1.6.4"}}
	client := &fakeMQTTClient{connected: true}
	publisher := New(cfg, applog.Discard(), nil, controller)
	publisher.client = client
	publisher.connected = true
	publisher.updateAnnounced = true

	publisher.ClearDiscovery()

	if len(client.messages) != 1 {
		t.Fatalf("clear published %d messages, want one update retirement", len(client.messages))
	}
	message := client.messages[0]
	if message.topic != cfg.DiscoveryTopic("update", updateKey) || message.qos != 1 || !message.retain || len(message.payload) != 0 {
		t.Errorf("retirement = topic %q qos %d retain %v payload %q", message.topic, message.qos, message.retain, message.payload)
	}
	if publisher.updateAnnounced {
		t.Error("retired update entity is still remembered as announced")
	}
}

// The initial connection is retried in the background. A failed attempt still
// has to reach Status; otherwise the interface says "connecting" forever while
// the useful socket error never reaches the application.
func TestInitialConnectionFailureReachesStatus(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}

	cfg := config.Defaults()
	cfg.MQTTHost = "127.0.0.1"
	cfg.MQTTPort = port
	publisher := New(cfg, applog.Discard(), nil, nil)
	connecting := publisher.Status().Detail

	if err := publisher.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(publisher.Stop)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && publisher.Status().Detail == connecting {
		time.Sleep(10 * time.Millisecond)
	}
	status := publisher.Status()
	if got := status.Detail; got == connecting {
		t.Fatalf("status stayed at %q after the broker refused the connection", got)
	}
	if !status.Failed || status.Healthy {
		t.Errorf("failed connection status = healthy:%v failed:%v", status.Healthy, status.Failed)
	}
}

func TestInitialConnectionIsRetried(t *testing.T) {
	publisher := New(config.Defaults(), applog.Discard(), nil, nil)
	publisher.connectRetry = time.Millisecond
	client := &fakeMQTTClient{connectErrors: []error{errors.New("first attempt"), nil}}

	publisher.connect(client)

	if client.connectCalls != 2 {
		t.Errorf("Connect calls = %d, want 2", client.connectCalls)
	}
	if !publisher.Status().Failed {
		t.Error("the first failed attempt was not recorded")
	}
}

func TestLateOrStaleConnectionIsIgnored(t *testing.T) {
	t.Run("after stop", func(t *testing.T) {
		publisher := New(config.Defaults(), applog.Discard(), nil, nil)
		late := &fakeMQTTClient{}
		publisher.client = late
		publisher.Stop()

		publisher.onConnect(late)

		if publisher.connected {
			t.Error("a connection callback made a stopped publisher active again")
		}
		if late.publishes != 0 {
			t.Error("a stopped publisher announced itself online")
		}
	})

	t.Run("from replaced client", func(t *testing.T) {
		publisher := New(config.Defaults(), applog.Discard(), nil, nil)
		current := &fakeMQTTClient{}
		late := &fakeMQTTClient{}
		publisher.client = current
		t.Cleanup(publisher.Stop)

		publisher.onConnect(late)

		if publisher.connected {
			t.Error("a replaced client made the publisher active")
		}
		if late.publishes != 0 {
			t.Error("a replaced client announced itself online")
		}
	})
}

func TestSuccessfulExportClearsAPreviousFailure(t *testing.T) {
	publisher := New(config.Defaults(), applog.Discard(), nil, nil)
	client := &fakeMQTTClient{connected: true}
	publisher.client = client
	publisher.connected = true
	publisher.recordError(errors.New("temporary publish failure"))

	if err := publisher.Export(collector.Snapshot{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	status := publisher.Status()
	if status.Failed || !status.Healthy {
		t.Errorf("status after successful export = healthy:%v failed:%v", status.Healthy, status.Failed)
	}
}
