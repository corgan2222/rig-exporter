package webui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/config"
)

// The form carries the target list as a repeated field name, which is the one
// thing about this that a handler can get wrong invisibly: FormValue returns
// only the first value, so a page that sends four targets and a handler that
// reads one look identical from the outside — three probes simply never happen.
func TestEveryTargetTheFormSendsIsKept(t *testing.T) {
	server, ts := newServer(t, nil)

	post(t, ts.URL, "/save/sensors", url.Values{
		"net_enabled":      {"1"},
		"ping_enabled":     {"1"},
		"ping_target":      {"1.1.1.1", "8.8.8.8", "one.one.one.one"},
		"ping_count":       {"3"},
		"ping_interval_ms": {"15000"},
	})

	got := server.app.Config().PingTargets
	want := []string{"1.1.1.1", "8.8.8.8", "one.one.one.one"}
	if len(got) != len(want) {
		t.Fatalf("PingTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PingTargets[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The empty rows a form always carries — somebody added three and filled in one
// — must not become three probes against the default gateway.
func TestEmptyRowsAreDropped(t *testing.T) {
	server, ts := newServer(t, nil)

	post(t, ts.URL, "/save/sensors", url.Values{
		"net_enabled":      {"1"},
		"ping_enabled":     {"1"},
		"ping_target":      {"1.1.1.1", "", "  "},
		"ping_count":       {"3"},
		"ping_interval_ms": {"15000"},
	})

	if got := server.app.Config().PingTargets; len(got) != 1 || got[0] != "1.1.1.1" {
		t.Errorf("PingTargets = %v, want only the row that was filled in", got)
	}
}

// A machine that has never touched this setting keeps its single gateway probe.
func TestSavingWithNoTargetsLeavesTheGatewayProbe(t *testing.T) {
	server, ts := newServer(t, nil)

	post(t, ts.URL, "/save/sensors", url.Values{
		"net_enabled":      {"1"},
		"ping_enabled":     {"1"},
		"ping_target":      {""},
		"ping_count":       {"3"},
		"ping_interval_ms": {"15000"},
	})

	if got := server.app.Config().PingTargets; len(got) != 0 {
		t.Errorf("PingTargets = %v, want empty — that is what the gateway is", got)
	}
}

// The page has to render one row per configured target, or a saved list is
// invisible and the next save silently truncates it back to what is on screen.
func TestThePageRendersOneRowPerTarget(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.PingTargets = []string{"1.1.1.1", "8.8.8.8"}
	})

	_, body := get(t, ts.URL+"/capture")

	if got := strings.Count(body, `name="ping_target"`); got != 2 {
		t.Errorf("the page carries %d target rows, want 2", got)
	}
	for _, target := range []string{"1.1.1.1", "8.8.8.8"} {
		if !strings.Contains(body, `value="`+target+`"`) {
			t.Errorf("the page does not show the configured target %s", target)
		}
	}
}

// With none configured there is still a row, because a form with nothing to
// type into cannot be used to add the first target.
func TestThePageAlwaysOffersARow(t *testing.T) {
	_, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/capture")

	if got := strings.Count(body, `name="ping_target"`); got != 1 {
		t.Errorf("the page carries %d target rows with none configured, want 1", got)
	}
}
