//go:build windows

package webui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/corgan/rig-exporter/internal/config"
)

func apiStatus(t *testing.T, url string) map[string]any {
	t.Helper()

	code, body := get(t, url+"/api/status")
	if code != http.StatusOK {
		t.Fatalf("GET /api/status = %d", code)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return out
}

// The chips are filled from the API rather than the template, so a setting
// saved in another tab reaches them without a reload. That only works if the
// API actually carries them.
func TestTheStatusApiCarriesWhatTheChipsShow(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.SensorSet = config.SensorSetStandard
		c.Decimals = false
		c.PublishIntervalMs = 2000
		c.IdlePublishIntervalMs = 10000
	})

	s := apiStatus(t, ts.URL)

	for _, key := range []string{"sensor_set", "decimals", "entity_count", "publish_ms", "rendering"} {
		if _, ok := s[key]; !ok {
			t.Errorf("the status API does not carry %q", key)
		}
	}
	if s["sensor_set"] != config.SensorSetStandard {
		t.Errorf("sensor_set = %v, want standard", s["sensor_set"])
	}
	if s["decimals"] != false {
		t.Errorf("decimals = %v, want false", s["decimals"])
	}
}

// Nothing is rendering in a test, so the chip has to name the idle rate. A
// number without saying which of the two it is would be worse than none.
func TestThePaceChipNamesTheRateThatIsInForce(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.PollIntervalMs = 500
		c.PublishIntervalMs = 2000
		c.IdlePublishIntervalMs = 10000
	})

	s := apiStatus(t, ts.URL)

	if s["rendering"] != false {
		t.Fatalf("rendering = %v, want false with no game", s["rendering"])
	}
	if got := s["publish_ms"]; got != float64(10000) {
		t.Errorf("publish_ms = %v, want the idle rate 10000", got)
	}
}

// The count moved out of the MQTT badge, so it must no longer be there — and
// it has to be reachable even with MQTT switched off, which is the reason it
// moved.
func TestTheEntityCountLeftTheMqttBadge(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) { c.MQTTEnabled = false })

	s := apiStatus(t, ts.URL)
	if _, ok := s["entity_count"].(float64); !ok {
		t.Errorf("entity_count = %v, want a number even without MQTT", s["entity_count"])
	}

	for _, e := range s["exports"].([]any) {
		export := e.(map[string]any)
		if export["name"] != "mqtt" {
			continue
		}
		if detail, _ := export["detail"].(string); strings.Contains(detail, "entities") ||
			strings.Contains(detail, "Entities") {
			t.Errorf("the MQTT badge still carries the entity count: %q", detail)
		}
	}
}

// A measurement that is not collected must not leave an empty tile behind.
func TestTheStatusPageCanHideTheResolutionTile(t *testing.T) {
	_, ts := newServer(t, nil)

	code, body := get(t, ts.URL+"/")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	if !strings.Contains(body, `id="tile-resolution"`) {
		t.Error("the resolution tile has no id, so nothing can hide it")
	}
	if !strings.Contains(body, `$("tile-resolution").hidden = !s.resolution`) {
		t.Error("nothing hides the resolution tile when the value is absent")
	}
}
