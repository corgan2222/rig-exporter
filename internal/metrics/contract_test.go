package metrics

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The published contract: what a consumer sees, and nothing else.
//
// A Home Assistant entity, a Prometheus recording rule and an InfluxDB dashboard
// are all keyed off the identifier and interpret the number through the unit.
// Renaming a metric orphans every entity built on it; changing a unit silently
// rewrites the meaning of history already recorded under it — a chart that read
// GB yesterday and MB today never announces the change, it just lies.
//
// This is doubly load-bearing now that the same measurement can arrive from
// different places. A processor temperature must look identical whether it came
// from MSI Afterburner or from a kernel-backed source, on a machine with every
// helper installed or with none: same identifier, same unit, same precision.
// Which source produced a value is the program's business, never the consumer's.
//
// So the contract is written down rather than merely intended. Changing this
// file is allowed — breaking the contract on purpose is a legitimate act — but
// it has to be a deliberate edit that shows up in review, not a side effect of
// touching a definition.
var updateGolden = flag.Bool("update-catalogue", false,
	"rewrite the recorded metric contract after an intended change")

const goldenPath = "testdata/catalogue.txt"

func TestTheMetricContractHasNotDrifted(t *testing.T) {
	got := renderContract()

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("recorded %d metrics", len(All))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v\n\nrun: go test ./internal/metrics/ -update-catalogue", err)
	}
	if got == string(want) {
		return
	}

	t.Errorf(`the published metric contract changed.

%s

Every identifier and unit below is something a consumer keys off: a Home
Assistant entity id, a Prometheus series, an InfluxDB field. Renaming one
orphans the entities built on it, and changing a unit rewrites the meaning of
history already recorded under the old one.

If the change is intended, record it deliberately:
    go test ./internal/metrics/ -update-catalogue
and say in the commit message which consumers have to be repointed.`,
		diff(string(want), got))
}

// renderContract writes the parts of a definition a consumer can observe. The
// display name is deliberately absent: it is translated, so it is allowed to
// change, and pinning it here would fight the language switcher.
func renderContract() string {
	lines := make([]string, 0, len(All))
	for _, d := range All {
		lines = append(lines, fmt.Sprintf("%-24s %-10s %-6s prec=%d group=%-5s panel=%-5s cat=%-10s prom=%s",
			d.ID, unitOrNone(d.Unit), kindName(d.Kind), d.Precision,
			d.Group, d.PanelGroup(), categoryOrPrimary(d), promOrNone(d.Prom)))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// categoryOrPrimary is where Home Assistant files the entity.
//
// Pinned here because it is not cosmetic: moving a measurement between primary
// and diagnostic moves it out of the device's main list and out of every
// auto-generated dashboard. That is a change every user sees, and it should
// never happen as a side effect of editing a definition.
func categoryOrPrimary(d Definition) string {
	switch {
	case d.NoEntity:
		return "none"
	case d.EntityCategory == "":
		return "primary"
	default:
		return d.EntityCategory
	}
}

func unitOrNone(u string) string {
	if u == "" {
		return "-"
	}
	return u
}

func promOrNone(p string) string {
	if p == "" {
		return "-"
	}
	return p
}

func kindName(k Kind) string {
	switch k {
	case KindText:
		return "text"
	case KindBool:
		return "bool"
	case KindTable:
		return "table"
	default:
		return "gauge"
	}
}

// diff reports the lines that were removed and added, which is all anyone needs
// to see to judge whether a contract change was meant.
func diff(want, got string) string {
	inWant := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(want), "\n") {
		inWant[line] = true
	}
	inGot := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		inGot[line] = true
	}

	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(want), "\n") {
		if !inGot[line] {
			fmt.Fprintf(&b, "  removed: %s\n", strings.TrimSpace(line))
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if !inWant[line] {
			fmt.Fprintf(&b, "  added:   %s\n", strings.TrimSpace(line))
		}
	}
	return b.String()
}
