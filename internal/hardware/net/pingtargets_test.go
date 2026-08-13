//go:build windows

package net

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// answered builds a pinger that has already measured, without a network.
func answered(target string, rtt float64) *Pinger {
	p := &Pinger{target: target}
	p.result = PingResult{
		Target: target, Sent: 3, Received: 3, AverageMs: rtt, LossPercent: 0,
	}
	p.have = true
	return p
}

func keys(set *metrics.Set, id string) []string {
	var out []string
	for _, r := range set.Readings {
		if r.Def.ID == id {
			out = append(out, r.Key())
		}
	}
	return out
}

// One probe keeps the plain ping_rtt it has always had. This is the case every
// existing installation is in, and instancing it would rename the entity on all
// of them for a feature nobody switched on — a renamed entity is an orphaned
// dashboard and a broken automation.
func TestASingleTargetIsNotInstanced(t *testing.T) {
	set := &metrics.Set{}
	New([]*Pinger{answered("1.1.1.1", 12.5)}, false).addPing(set)

	if got := keys(set, metrics.PingRTT.ID); len(got) != 1 || got[0] != "ping_rtt" {
		t.Errorf("keys = %v, want exactly [ping_rtt]", got)
	}
}

// The gateway is the same case, and the one an unconfigured machine is in.
func TestTheDefaultGatewayIsNotInstancedEither(t *testing.T) {
	gateway := answered("", 3.2)
	gateway.result.Target = "192.168.2.1" // what the round resolved to

	set := &metrics.Set{}
	New([]*Pinger{gateway}, false).addPing(set)

	if got := keys(set, metrics.PingRTT.ID); len(got) != 1 || got[0] != "ping_rtt" {
		t.Errorf("keys = %v, want exactly [ping_rtt]", got)
	}
}

// From the second target on, every one of them carries its own instance —
// including the first, because two readings that both answer to ping_rtt are one
// reading overwriting the other in every export.
func TestSeveralTargetsEachGetTheirOwnIdentity(t *testing.T) {
	set := &metrics.Set{}
	New([]*Pinger{
		answered("1.1.1.1", 12.5),
		answered("8.8.8.8", 18.0),
	}, false).addPing(set)

	got := keys(set, metrics.PingRTT.ID)
	if len(got) != 2 {
		t.Fatalf("keys = %v, want one per target", got)
	}
	for i, want := range []string{"ping_rtt_1_1_1_1", "ping_rtt_8_8_8_8"} {
		if got[i] != want {
			t.Errorf("keys[%d] = %q, want %q", i, got[i], want)
		}
	}

	// Loss and the target name follow the same identity, or half the readings
	// would collide while the other half did not.
	if got := keys(set, metrics.PingLoss.ID); len(got) != 2 {
		t.Errorf("loss keys = %v, want one per target", got)
	}
	if got := keys(set, metrics.PingTarget.ID); len(got) != 2 {
		t.Errorf("target keys = %v, want one per target", got)
	}
}

// A target is filed under the host as configured, not under the host a round
// resolved to. They differ for a name that moves — and an instance that moves is
// an entity that moves with it, which is the thing instances exist to prevent.
func TestATargetIsFiledUnderWhatWasConfigured(t *testing.T) {
	named := answered("one.one.one.one", 12.5)
	named.result.Target = "1.1.1.1"

	set := &metrics.Set{}
	New([]*Pinger{named, answered("8.8.8.8", 18.0)}, false).addPing(set)

	if got := keys(set, metrics.PingRTT.ID); got[0] != "ping_rtt_one_one_one_one" {
		t.Errorf("keys[0] = %q, want the configured name", got[0])
	}
}

// A round that never got off the ground says nothing about the network, and
// must not be reported as zero loss — for one target among several just as much
// as for a lone one.
func TestATargetThatNeverSentReportsNoLoss(t *testing.T) {
	silent := answered("10.0.0.9", 0)
	silent.result = PingResult{Target: "10.0.0.9"}

	set := &metrics.Set{}
	New([]*Pinger{answered("1.1.1.1", 12.5), silent}, false).addPing(set)

	if got := keys(set, metrics.PingLoss.ID); len(got) != 1 {
		t.Errorf("loss keys = %v, want only the target that actually measured", got)
	}
	// Its name is still reported: the probe exists, it just has nothing to say.
	if got := keys(set, metrics.PingTarget.ID); len(got) != 2 {
		t.Errorf("target keys = %v, want both probes named", got)
	}
}
