package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// An existing config.json carries the single target in the field that used to
// hold it. Losing it on upgrade would silently move somebody's latency probe
// back to their router without telling them.
func TestTheOldSingleTargetIsCarriedOver(t *testing.T) {
	c := Defaults()
	c.PingTarget = "1.1.1.1"
	c.Normalize()

	if len(c.PingTargets) != 1 || c.PingTargets[0] != "1.1.1.1" {
		t.Errorf("PingTargets = %v, want the old target moved across", c.PingTargets)
	}
	// And retired, so it is not written back and cannot disagree with the new
	// field on the next load.
	if c.PingTarget != "" {
		t.Errorf("PingTarget = %q, want it blanked once it has been moved", c.PingTarget)
	}
}

// A file written by a newer build has both fields, and the old one is a
// leftover. The new field wins rather than being overwritten by history.
func TestTheNewFieldWinsWhenBothArePresent(t *testing.T) {
	c := Defaults()
	c.PingTarget = "1.1.1.1"
	c.PingTargets = []string{"8.8.8.8", "9.9.9.9"}
	c.Normalize()

	if len(c.PingTargets) != 2 || c.PingTargets[0] != "8.8.8.8" {
		t.Errorf("PingTargets = %v, want the new field untouched", c.PingTargets)
	}
}

// The retired field must not reappear in the file, or the next version to read
// it will find a value nobody set.
func TestTheRetiredFieldIsNotWrittenBack(t *testing.T) {
	c := Defaults()
	c.PingTargets = []string{"1.1.1.1"}

	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"ping_target"`) {
		t.Error("the retired ping_target field was written back into the configuration")
	}
	if !strings.Contains(string(out), `"ping_targets"`) {
		t.Error("ping_targets is missing from the configuration")
	}
}

func TestTheTargetListIsTidied(t *testing.T) {
	c := Defaults()
	c.PingTargets = []string{
		"  1.1.1.1  ", // trimmed
		"",            // dropped
		"   ",         // dropped
		"1.1.1.1",     // duplicate
		"8.8.8.8",
		"8.8.8.8", // duplicate
	}
	c.Normalize()

	want := []string{"1.1.1.1", "8.8.8.8"}
	if len(c.PingTargets) != len(want) {
		t.Fatalf("PingTargets = %v, want %v", c.PingTargets, want)
	}
	for i := range want {
		if c.PingTargets[i] != want[i] {
			t.Errorf("PingTargets[%d] = %q, want %q", i, c.PingTargets[i], want[i])
		}
	}
}

// Two entries that differ only in case are the same host, and would be two
// readings with the same instance — one overwriting the other in every export.
func TestTargetsThatDifferOnlyInCaseAreOneTarget(t *testing.T) {
	c := Defaults()
	c.PingTargets = []string{"One.One.One.One", "one.one.one.one"}
	c.Normalize()

	if len(c.PingTargets) != 1 {
		t.Errorf("PingTargets = %v, want one host", c.PingTargets)
	}
	// The first spelling is kept, because that is the one somebody typed.
	if c.PingTargets[0] != "One.One.One.One" {
		t.Errorf("PingTargets[0] = %q, want the spelling as entered", c.PingTargets[0])
	}
}

// The interface stops at the limit, but a hand-edited config.json does not.
func TestTheTargetListIsCapped(t *testing.T) {
	c := Defaults()
	for i := range MaxPingTargets + 5 {
		c.PingTargets = append(c.PingTargets, string(rune('a'+i))+".example")
	}
	c.Normalize()

	if len(c.PingTargets) != MaxPingTargets {
		t.Errorf("kept %d targets, want the cap of %d", len(c.PingTargets), MaxPingTargets)
	}
}

// The default is one probe against the gateway, which is what it has always
// been and what an unconfigured machine has to keep doing.
func TestNoTargetsMeansTheGateway(t *testing.T) {
	c := Defaults()
	c.Normalize()

	if len(c.PingTargets) != 0 {
		t.Errorf("PingTargets = %v, want empty by default", c.PingTargets)
	}
}
