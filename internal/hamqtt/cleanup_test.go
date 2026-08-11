package hamqtt

import (
	"errors"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// Clearing discovery has to survive an absent broker, exactly as retiring does.
//
// This is the same situation TestARetirementSurvivesTheBrokerBeingAway already
// covers for Retire, and ClearDiscovery simply returned. It is used when the
// node id or the topic prefix changes — so a rename while the broker happened
// to be restarting left every old retained message on it, and Home Assistant
// kept a second, permanently unavailable device that survives being deleted by
// hand: the retained config brings it back at the next restart.
func TestClearingDiscoverySurvivesTheBrokerBeingAway(t *testing.T) {
	p := testPublisher(t)

	refs := EntityRefs([]metrics.Reading{
		metrics.Gauge(metrics.CPUClock, "", 4200),
		metrics.Gauge(metrics.GPUCoreClock, "0", 1515),
	})
	for _, ref := range refs {
		p.announced[ref.key] = ref
	}

	p.ClearDiscovery() // no client: has to remember, not forget

	if len(p.pendingRetire) != len(refs) {
		t.Fatalf("clearing discovery without a broker dropped %d of %d retirements",
			len(refs)-len(p.pendingRetire), len(refs))
	}
}

// What was announced describes what lies on the broker, not what this
// connection did.
//
// A retained discovery message outlives the connection and the process, so
// forgetting the list on reconnect throws away the only record of what is out
// there. RetireUnselected reads that list, and its own documentation promises
// to catch "a drive that spun down, a card that stopped answering" — after a
// reconnect it could no longer see either, and those entities stayed in Home
// Assistant with nothing left to clear them.
func TestAnnouncementsSurviveAReconnect(t *testing.T) {
	p := testPublisher(t)

	ref := EntityRefs([]metrics.Reading{metrics.Gauge(metrics.CPUClock, "", 4200)})[0]
	p.announced[ref.key] = ref

	client := &fakeMQTTClient{connected: true, publishErrors: map[string][]error{}}
	p.client = client
	p.onConnect(client)

	if _, ok := p.announced[ref.key]; !ok {
		t.Error("the reconnect forgot an entity that is still retained on the broker")
	}
}

// And a drive that was unplugged while the broker was away is still retired
// when its group is unticked afterwards. This is the fault as a user meets it:
// reconnect, then the entity goes quiet, then the tick comes off.
func TestUnselectingAfterAReconnectStillRetires(t *testing.T) {
	p := testPublisher(t)

	ref := EntityRefs([]metrics.Reading{metrics.Gauge(metrics.DiskBusy, "F:", 12)})[0]
	ref.defID = metrics.DiskBusy.ID
	p.announced[ref.key] = ref

	client := &fakeMQTTClient{connected: true, publishErrors: map[string][]error{}}
	p.client = client
	p.onConnect(client)

	// The disk group is unticked. Nothing is reporting F: any more.
	p.RetireUnselected(func(defID string) bool { return defID != metrics.DiskBusy.ID })

	var retired bool
	for _, message := range client.messages {
		if message.topic == p.cfg.DiscoveryTopic(ref.component, ref.key) && len(message.payload) == 0 {
			retired = true
		}
	}
	if !retired {
		t.Error("the unplugged drive was never retired: the reconnect had erased the reference")
	}
}

// The one-off cleanup after a rename of the application has to finish, and it
// has to say whether it did.
//
// It stopped at the first failing topic and returned nothing, so the caller
// could not tell success from half a pass — and cleared the flag that remembers
// to try again either way. Eight retained messages of the previous application
// name then stayed on the broker forever: a dead device with sensor.fps,
// sensor.cpu and binary_sensor.rtss in it.
func TestLegacyCleanupReportsAndFinishesDespiteOneFailure(t *testing.T) {
	p := testPublisher(t)

	failing := p.cfg.LegacyDiscoveryTopic(legacyKeys[0].component, legacyKeys[0].key)
	client := &fakeMQTTClient{connected: true, publishErrors: map[string][]error{
		failing: {errors.New("broker refused")},
	}}

	err := p.clearLegacyDiscovery(client)

	if err == nil {
		t.Error("a failed legacy cleanup reported success")
	}
	if client.publishes != len(legacyKeys) {
		t.Errorf("legacy cleanup stopped after %d of %d topics",
			client.publishes, len(legacyKeys))
	}
}

// A rename while the broker is away is finished by whoever comes next.
//
// Queueing the retirements is not enough on its own: the publisher that knows
// the old identity is stopped and discarded in the same breath as the rename.
// So the old identity is written to the configuration, and the publisher built
// after it empties those topics alongside its own announcements — the same
// mechanism that already retires earlier shapes of a key, one level up.
func TestTheNewIdentityRetiresTheOldOne(t *testing.T) {
	p := testPublisher(t)
	p.cfg.NodeID = "gamingpc"
	p.cfg.PreviousNodeID = "pc3"

	reading := metrics.Gauge(metrics.CPUClock, "", 4200)
	client := &fakeMQTTClient{connected: true, publishErrors: map[string][]error{}}
	p.client = client

	var snap collector.Snapshot
	snap.Add(reading)
	if err := p.announceNew(client, snap); err != nil {
		t.Fatalf("announceNew: %v", err)
	}

	old := p.cfg
	old.NodeID = "pc3"
	wanted := old.DiscoveryTopic(reading.Def.Component(), reading.Key())

	var retired bool
	for _, message := range client.messages {
		if message.topic == wanted && len(message.payload) == 0 && message.retain {
			retired = true
		}
	}
	if !retired {
		t.Errorf("nothing emptied %s, so Home Assistant keeps a second dead device", wanted)
	}
}

// And with no rename on record it stays quiet, rather than publishing into a
// hundred topics that were never used.
func TestWithoutARenameNothingExtraIsPublished(t *testing.T) {
	p := testPublisher(t)

	if topics := p.previousTopicsFor(metrics.Gauge(metrics.CPUClock, "", 4200)); topics != nil {
		t.Errorf("an installation that was never renamed would publish to %v", topics)
	}
}

// And a clean run says so, or the flag would never come off.
func TestLegacyCleanupReportsSuccess(t *testing.T) {
	p := testPublisher(t)

	client := &fakeMQTTClient{connected: true, publishErrors: map[string][]error{}}
	if err := p.clearLegacyDiscovery(client); err != nil {
		t.Errorf("a clean run reported %v", err)
	}
	if !p.LegacyCleanupDone() {
		t.Error("a clean run did not record that it happened")
	}
}
