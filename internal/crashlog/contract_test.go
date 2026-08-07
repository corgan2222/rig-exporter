package crashlog

import (
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The issue form lives in this repository, so the contract between it and this
// package can be checked rather than agreed.
//
// This is the test both sides were worried about and neither could write alone:
// a renamed id makes the field arrive empty, a renamed file makes the button
// land on a 404, and GitHub reports neither. Now the build does.
const formPath = "../../.github/ISSUE_TEMPLATE/"

// formFields reads the ids and the required flags out of the form.
//
// Scanned rather than parsed: the file is a flat list of blocks, the two facts
// wanted are on their own lines, and adding a YAML dependency to a program that
// has none would cost more than it saves.
func formFields(t *testing.T, name string) (ids map[string]bool, required map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(formPath, name))
	if err != nil {
		t.Fatalf("the form this package fills in is not there: %v", err)
	}

	ids, required = map[string]bool{}, map[string]bool{}
	current := ""
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "id:"):
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "id:"))
			ids[current] = true
		case trimmed == "required: true" && current != "":
			required[current] = true
		}
	}
	return ids, required
}

// Every field the button fills has to exist in the form under that exact name.
func TestEveryFilledFieldExistsInTheForm(t *testing.T) {
	ids, _ := formFields(t, issueTemplate)

	raw := IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{
		OS: "Windows 11", CPU: "CPU", GPU: "GPU", Locale: "de-DE", Sources: "NVML",
	})
	parsed, err := neturl.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	// template and title are GitHub's own parameters, not fields of the form.
	own := map[string]bool{"template": true, "title": true}
	for name := range parsed.Query() {
		if own[name] {
			continue
		}
		if !ids[name] {
			t.Errorf("the button fills %q, which the form does not have — it would arrive empty", name)
		}
	}
}

// Every field the form demands has to be one the button fills, except the one
// question only a person can answer. A required field left empty blocks the
// submit, and the reporter would have to work out which.
func TestEveryRequiredFieldIsFilledExceptTheHumanOne(t *testing.T) {
	_, required := formFields(t, issueTemplate)

	parsed, err := neturl.Parse(IssueURL("https://github.com/corgan2222/rig-exporter",
		reportWithLog(), Machine{OS: "Windows 11", CPU: "CPU", Locale: "de-DE"}))
	if err != nil {
		t.Fatal(err)
	}
	filled := parsed.Query()

	for name := range required {
		if name == "steps" {
			continue // the question only a person can answer
		}
		if filled.Get(name) == "" {
			t.Errorf("the form requires %q and the button leaves it empty, so the submit is blocked", name)
		}
	}
	if !required["steps"] {
		t.Error("steps is no longer required; the form now answers its own question")
	}
}

// The form declares its own labels. Setting them in the URL as well is at best
// redundant and at worst silently discarded.
func TestTheFormDeclaresTheLabels(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(formPath, issueTemplate))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "labels:") {
		t.Error("the form sets no labels, so the issue arrives unlabelled")
	}

	parsed, _ := neturl.Parse(IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{}))
	if parsed.Query().Has("labels") {
		t.Error("the button sets labels as well")
	}
}

// Where the form renders the field itself, ours must not add a second fence.
func TestFieldsTheFormRendersCarryNoFenceFromHere(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(formPath, issueTemplate))
	if err != nil {
		t.Fatal(err)
	}

	// Which ids sit in a block that also says render.
	rendered := map[string]bool{}
	current := ""
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "id:") {
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "id:"))
		}
		if strings.HasPrefix(trimmed, "render:") && current != "" {
			rendered[current] = true
		}
	}
	if !rendered["panic"] || !rendered["log"] {
		t.Fatalf("the two long fields no longer render themselves: %v", rendered)
	}

	parsed, _ := neturl.Parse(IssueURL("https://github.com/corgan2222/rig-exporter", reportWithLog(), Machine{}))
	for id := range rendered {
		if strings.Contains(parsed.Query().Get(id), "```") {
			t.Errorf("%s is rendered by the form and carries a fence from here too", id)
		}
	}
}
