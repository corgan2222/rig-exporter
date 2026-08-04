//go:build windows

package webui

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/config"
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

func exportAPIStatus(t *testing.T, url, name string) map[string]any {
	t.Helper()

	exports, ok := apiStatus(t, url)["exports"].([]any)
	if !ok {
		t.Fatal("status API has no export target list")
	}
	for _, raw := range exports {
		status, ok := raw.(map[string]any)
		if ok && status["name"] == name {
			return status
		}
	}
	t.Fatalf("status API has no %s export target", name)
	return nil
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

// The hint about the Home Assistant database sits between the two cards and
// links to the block that fixes it.
func TestTheRecorderNoticeSitsBetweenTheCards(t *testing.T) {
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
}

// Read once, gone for good. The answer belongs in the configuration: the
// interface falls back to a random port when the configured one is taken, and
// a different port is a different origin — anything kept in the browser was
// thrown away on the next start, which is exactly how this was found.
func TestTheRecorderNoticeStaysAwayAcrossRestarts(t *testing.T) {
	server, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, `id="recorder-notice"`) {
		t.Fatal("the notice is not shown to somebody who has not read it")
	}

	resp := post(t, ts.URL, "/dismiss", url.Values{"what": {"recorder"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if !server.app.Config().RecorderNoticeRead {
		t.Fatal("the configuration does not remember that it was read")
	}

	_, body = get(t, ts.URL+"/")
	if strings.Contains(body, `id="recorder-notice"`) {
		t.Error("the notice came back after it was read")
	}

	// A fresh server over the same configuration is what a restart looks like.
	_, again := newServer(t, func(c *config.Config) { c.RecorderNoticeRead = true })
	if _, body := get(t, again.URL+"/"); strings.Contains(body, `id="recorder-notice"`) {
		t.Error("the notice came back after a restart")
	}
}

// A machine that does not provide game telemetry must be able to put the RTSS
// warning away permanently. This is an installation setting, not a browser
// preference, for the same reason as the recorder notice above.
func TestTheRTSSWarningCanRememberThatThisMachineHasNoGPU(t *testing.T) {
	server, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, `name="what" value="no_gpu"`) {
		t.Fatal("the RTSS warning has no no-GPU dismissal")
	}

	resp := post(t, ts.URL, "/dismiss", url.Values{"what": {"no_gpu"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if !server.app.Config().NoGPU {
		t.Fatal("the configuration does not remember no_gpu")
	}
	if got := apiStatus(t, ts.URL)["no_gpu"]; got != true {
		t.Errorf("status API no_gpu = %v, want true", got)
	}
}

// Server-side hiding prevents a flash of game-only controls before the first
// status poll. The elements stay in the document so a settings change in
// another tab can reveal them again without a reload.
func TestNoGPUHidesTheGameStatusOnFirstRender(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) { c.NoGPU = true })

	_, body := get(t, ts.URL+"/")
	for _, id := range []string{"tile-fps", "tile-frametime", "tile-game", "rtss-badge"} {
		tag := openingTagWithID(t, body, id)
		if !strings.Contains(tag, " hidden") {
			t.Errorf("%s is visible with no_gpu: %s", id, tag)
		}
	}
	if tag := openingTagWithID(t, body, "tile-resolution"); strings.Contains(tag, " hidden") {
		t.Errorf("no_gpu hid the resolution tile although only game status was requested: %s", tag)
	}
}

// The dismissal is reversible from the regular settings; otherwise one click
// would require editing config.json by hand to undo.
func TestNoGPUCanBeClearedInApplicationSettings(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) { c.NoGPU = true })

	_, body := get(t, ts.URL+"/export")
	if tag := openingTagWithID(t, body, "no_gpu"); !strings.Contains(tag, " checked") {
		t.Errorf("the no_gpu setting is not shown as enabled: %s", tag)
	}

	post(t, ts.URL, "/save/app", url.Values{
		"language": {server.app.Config().Language},
		"web_port": {"8787"},
	})
	if server.app.Config().NoGPU {
		t.Error("no_gpu stayed on although its settings checkbox was cleared")
	}
}

func openingTagWithID(t *testing.T, body, id string) string {
	t.Helper()

	position := strings.Index(body, `id="`+id+`"`)
	if position < 0 {
		t.Fatalf("no element with id %q", id)
	}
	start := strings.LastIndex(body[:position], "<")
	endOffset := strings.Index(body[position:], ">")
	if start < 0 || endOffset < 0 {
		t.Fatalf("could not isolate opening tag for %q", id)
	}
	return body[start : position+endOffset+1]
}

// Anything else must not be able to write into the configuration through it.
func TestOnlyAKnownNoticeCanBeDismissed(t *testing.T) {
	_, ts := newServer(t, nil)

	if resp := post(t, ts.URL, "/dismiss", url.Values{"what": {"everything"}}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// MQTT reconnects in the background, so the settings page must keep polling
// its live target status. Otherwise a failed connection remains invisible
// until somebody happens to open the log.
func TestTheMQTTStatusFollowsConnectionFailures(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.MQTTEnabled = true
		c.MQTTHost = "unreachable.example"
	})

	_, body := get(t, ts.URL+"/export")

	if !strings.Contains(body, `id="mqtt-status"`) {
		t.Fatal("no status line for the enabled MQTT target")
	}
	start := strings.Index(body, `id="mqtt-status"`)
	status := body[start:]
	status = status[:strings.Index(status, "</div>")]
	if !strings.Contains(status, "dot warn") {
		t.Error("an MQTT connection still in progress is not marked as transitional")
	}
	if strings.Contains(status, `class="note err"`) {
		t.Error("an MQTT connection still in progress is presented as an error")
	}
	if !strings.Contains(status, "unreachable.example:1883") {
		t.Error("the MQTT status does not name the broker")
	}
	if !strings.Contains(body, `refreshExportStatus("mqtt", targets)`) {
		t.Error("the poller does not pick MQTT out of the export targets")
	}
	if !strings.Contains(body, "setInterval(refreshExportStatuses") {
		t.Error("the MQTT status is fetched once but never again")
	}
	if !strings.Contains(body, "exportEscape(EL.lastError)") {
		t.Error("the MQTT error branch does not render the connection failure")
	}
}

func TestTheMQTTStatusShowsTheConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}

	server, ts := newServer(t, func(c *config.Config) {
		c.MQTTEnabled = true
		c.MQTTHost = "127.0.0.1"
		c.MQTTPort = port
	})
	server.app.Start()
	t.Cleanup(server.app.Stop)

	deadline := time.Now().Add(2 * time.Second)
	failed := false
	for time.Now().Before(deadline) && !failed {
		for _, status := range server.app.Status().Exports {
			failed = failed || status.Name == "mqtt" && status.Failed
		}
		if !failed {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !failed {
		t.Fatal("MQTT connection failure never reached the app status")
	}
	if got := exportAPIStatus(t, ts.URL, "mqtt")["failed"]; got != true {
		t.Errorf("MQTT API failed = %v, want true", got)
	}

	_, body := get(t, ts.URL+"/export")
	start := strings.Index(body, `id="mqtt-status"`)
	if start < 0 {
		t.Fatal("no MQTT status line")
	}
	status := body[start:]
	status = status[:strings.Index(status, "</div>")]
	if !strings.Contains(status, "dot bad") {
		t.Error("the failed MQTT target is not marked as failed")
	}
	if !strings.Contains(status, `class="note err"`) {
		t.Error("the MQTT connection error is not shown")
	}
	if !strings.Contains(status, `value="log"`) {
		t.Error("the MQTT failure offers no way to open the log")
	}
}

// A switched-off target says nothing: the box above already says it is off.
// The container is still there, empty and hidden, because the poller needs a
// place to write into when the target is switched on without a reload.
func TestTheInfluxStatusIsEmptyWhilePushIsOff(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) { c.InfluxPushEnabled = false })

	_, body := get(t, ts.URL+"/export")

	block := strings.Index(body, `id="influx-status"`)
	if block < 0 {
		t.Fatal("the container is missing, so nothing can fill it later")
	}
	line := body[block : strings.Index(body[block:], "</div>")+block]
	if !strings.Contains(line, "hidden") {
		t.Error("an empty status line is shown for a target that is switched off")
	}
	if strings.Contains(line, "badge") {
		t.Error("a status was rendered for a target that is switched off")
	}
}

// Saving redirects back here before the first write has been attempted, so a
// server-rendered snapshot would always look healthy. The page has to ask
// again by itself.
func TestTheInfluxStatusFollowsAlongWithoutAReload(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.InfluxPushEnabled = true
		c.InfluxURL = "http://influx.example:8086"
	})

	_, body := get(t, ts.URL+"/export")

	if !strings.Contains(body, `fetch("/api/status"`) {
		t.Error("the export page never asks for the current state")
	}
	if !strings.Contains(body, "setInterval(refreshExportStatuses") {
		t.Error("the status is fetched once but never again")
	}
	if !strings.Contains(body, `refreshExportStatus("influx", targets)`) {
		t.Error("the poller does not pick the push target out of the targets")
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
