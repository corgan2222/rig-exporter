package hamqtt

import (
	"testing"

	"github.com/corgan/rig-exporter/internal/applog"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/metrics"
)

func testPublisher(t *testing.T) *Publisher {
	t.Helper()

	cfg := config.Defaults()
	cfg.NodeID = "corganpc2"
	return New(cfg, applog.Discard(), nil)
}

// This is the one that matters. Exports run on their own goroutine, so a
// snapshot collected before the switch can reach announceNew after the
// retirement — and without this guard it would re-announce every entity that
// was just retired, retained, and the switch would appear to do nothing.
func TestAStaleSnapshotCannotResurrectARetiredEntity(t *testing.T) {
	t.Cleanup(func() { metrics.SetStandardOnly(false) })

	stale := metrics.Gauge(metrics.CPUClock, "", 4200) // extended
	keep := metrics.Gauge(metrics.CPULoad, "", 37)     // standard

	metrics.SetStandardOnly(false)
	if !announceable(stale) || !announceable(keep) {
		t.Error("the extended set refused to announce something")
	}

	metrics.SetStandardOnly(true)
	if announceable(stale) {
		t.Error("a measurement outside the standard set would still be announced")
	}
	if !announceable(keep) {
		t.Error("the standard set refused one of its own")
	}
}

// A retirement decided while the broker is away is still meant, so it waits
// rather than evaporating.
func TestARetirementSurvivesTheBrokerBeingAway(t *testing.T) {
	p := testPublisher(t)

	refs := EntityRefs([]metrics.Reading{
		metrics.Gauge(metrics.CPUClock, "", 4200),
		metrics.Gauge(metrics.GPUCoreClock, "0", 1515),
	})

	// Pretend both had been announced, which is the normal case.
	for _, ref := range refs {
		p.announced[ref.key] = ref
	}

	p.Retire(refs) // no client: queues

	if len(p.pendingRetire) != 2 {
		t.Errorf("queued %d retirements, want 2", len(p.pendingRetire))
	}
	if len(p.announced) != 0 {
		t.Errorf("%d entities are still marked as announced", len(p.announced))
	}
}

// Forgetting the entity is what lets switching back announce it again.
func TestRetiringForgetsTheEntitySoItCanComeBack(t *testing.T) {
	p := testPublisher(t)

	reading := metrics.Gauge(metrics.CPUClock, "", 4200)
	key := reading.Key()
	p.announced[key] = EntityRef{component: reading.Def.Component(), key: key}

	p.Retire(EntityRefs([]metrics.Reading{reading}))

	if _, still := p.announced[key]; still {
		t.Error("the entity is still remembered as announced, so it would never be re-announced")
	}
}

// Retiring nothing must not queue anything, or a reconnect would republish
// empty payloads for no reason.
func TestRetiringNothingDoesNothing(t *testing.T) {
	p := testPublisher(t)

	p.Retire(nil)
	p.Retire([]EntityRef{})

	if len(p.pendingRetire) != 0 {
		t.Errorf("queued %d retirements for an empty request", len(p.pendingRetire))
	}
}

// The topic that gets emptied has to be the very one the entity was announced
// on, byte for byte — Home Assistant matches on the discovery hash.
func TestARetirementTargetsTheAnnouncedTopic(t *testing.T) {
	p := testPublisher(t)

	reading := metrics.Gauge(metrics.GPUCoreClock, "1", 2700)
	announced, _, err := discoveryMessage(p.cfg, "", reading)
	if err != nil {
		t.Fatalf("discoveryMessage: %v", err)
	}

	refs := EntityRefs([]metrics.Reading{reading})
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if got := p.cfg.DiscoveryTopic(refs[0].component, refs[0].key); got != announced {
		t.Errorf("retiring %q but the entity was announced on %q", got, announced)
	}
}
