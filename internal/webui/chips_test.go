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

// Reading a value and changing it should not be two searches, so every chip
// leads to the control that decides it.
func TestEveryChipLinksToItsSetting(t *testing.T) {
	_, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/")

	for _, want := range []struct{ id, href string }{
		{"c-set", "/capture#sensors"},
		{"c-decimals", "/capture#capture"},
		{"c-entities", "/capture#sensors"},
		{"c-interval", "/capture#capture"},
	} {
		if !strings.Contains(body, `id="`+want.id+`" href="`+want.href+`"`) {
			t.Errorf("chip %s does not link to %s", want.id, want.href)
		}
	}
}

// The export targets come first, the settings chips below them.
func TestTheExportBadgesComeBeforeTheSettingChips(t *testing.T) {
	_, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/")
	if strings.Index(body, `id="export-badges"`) > strings.Index(body, `id="c-set"`) {
		t.Error("the setting chips are above the export badges")
	}
}

// The hint about the Home Assistant database sits between the two cards, links
// to the block that fixes it, and can be put away for good.
func TestTheRecorderNoticeIsShownOnceAndCanBeDismissed(t *testing.T) {
	_, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/")

	notice := strings.Index(body, `id="recorder-notice"`)
	if notice < 0 {
		t.Fatal("no notice about the Home Assistant database")
	}
	if status, hardware := strings.Index(body, `id="export-badges"`), strings.Index(body, `id="sensor-groups"`); !(status < notice && notice < hardware) {
		t.Errorf("the notice is not between the status and the hardware card (%d, %d, %d)", status, notice, hardware)
	}
	if !strings.Contains(body, `href="/export#recorder"`) {
		t.Error("the notice does not link to the recorder block")
	}
	if !strings.Contains(body, `rig.recorderNoticeRead`) {
		t.Error("dismissing the notice is not remembered")
	}
}

// A switched-off target says nothing: the box above already says it is off.
func TestTheInfluxStatusIsAbsentWhilePushIsOff(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) { c.InfluxPushEnabled = false })

	_, body := get(t, ts.URL+"/export")
	if strings.Contains(body, `id="influx-status"`) {
		t.Error("a status line appeared for a target that is switched off")
	}
}

// With push on, the line shows what the target is doing — and while nothing has
// gone wrong it must not offer a log button, or the offer means nothing.
func TestTheInfluxStatusShowsTheTargetWhilePushIsOn(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.InfluxPushEnabled = true
		c.InfluxURL = "http://influx.example:8086"
		c.InfluxBucket = "rig"
		c.InfluxToken = "t"
	})

	_, body := get(t, ts.URL+"/export")

	if !strings.Contains(body, `id="influx-status"`) {
		t.Fatal("no status line for the enabled push target")
	}
	if !strings.Contains(body, "influx.example:8086") {
		t.Error("the status line does not name where it writes to")
	}

	// The log button lives in the error branch only.
	status := body[strings.Index(body, `id="influx-status"`):]
	status = status[:strings.Index(status, "</div>")]
	if strings.Contains(status, `value="log"`) {
		t.Error("a healthy target offers the error log")
	}
	if strings.Contains(status, "dot bad") {
		t.Error("a target that has not failed is marked as failed")
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
