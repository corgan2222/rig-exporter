//go:build windows

package webui

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// newServer wires a server against a real App with everything that would
// reach the network switched off, so the handlers can be exercised without a
// broker, a data server or a collection loop.
func newServer(t *testing.T, mutate func(*config.Config)) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Defaults()
	cfg.MQTTEnabled = false
	cfg.DataServerEnabled = false
	cfg.NodeID = "corganpc2"
	cfg.DeviceName = "CorganPC2"
	if mutate != nil {
		mutate(&cfg)
	}
	cfg.Normalize()

	application := app.New(cfg, t.TempDir()+`\config.json`, applog.Discard(), nil)
	server, err := New(application, applog.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	httpServer := httptest.NewServer(server.server.Handler)
	t.Cleanup(httpServer.Close)
	return server, httpServer
}

// post submits a form without following the redirect, which is what carries
// the outcome.
func post(t *testing.T, base, path string, values url.Values) *http.Response {
	t.Helper()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.PostForm(base+path, values)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestEveryPageRenders(t *testing.T) {
	_, ts := newServer(t, nil)

	for path, marker := range map[string]string{
		"/":        "sensor-groups",
		"/capture": `action="/save/sensors"`,
		"/export":  `action="/save/mqtt"`,
	} {
		code, body := get(t, ts.URL+path)
		if code != http.StatusOK {
			t.Errorf("GET %s = %d", path, code)
			continue
		}
		if !strings.Contains(body, marker) {
			t.Errorf("GET %s does not look like the right page", path)
		}
	}
}

// This is the reason each block posts to its own endpoint. A form carries no
// evidence of the checkboxes it does not contain, so a save that applied the
// whole configuration would switch off everything on the other page.
func TestSavingOneBlockLeavesTheOthersAlone(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) {
		c.MQTTEnabled = true
		c.GPUEnabled = true
		c.DiskEnabled = true
	})

	// The capture form has no MQTT fields at all.
	resp := post(t, ts.URL, "/save/capture", url.Values{
		"poll_interval_ms": {"250"},
		"interval_ms":      {"1000"},
		"idle_timeout_ms":  {"4000"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}

	cfg := server.app.Config()
	if cfg.PollIntervalMs != 250 || cfg.PublishIntervalMs != 1000 {
		t.Errorf("intervals = %d/%d, want 250/1000", cfg.PollIntervalMs, cfg.PublishIntervalMs)
	}
	if !cfg.MQTTEnabled {
		t.Error("saving the capture block switched MQTT off")
	}
	if !cfg.GPUEnabled || !cfg.DiskEnabled {
		t.Error("saving the capture block switched a sensor group off")
	}
}

// The two publish rates are separate fields on the same card, and the decimals
// box travels with them.
func TestTheCaptureBlockCarriesBothPublishRatesAndTheDecimals(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) { c.Decimals = true })

	resp := post(t, ts.URL, "/save/capture", url.Values{
		"poll_interval_ms": {"500"},
		"interval_ms":      {"2000"},
		"idle_interval_ms": {"10000"},
		"idle_timeout_ms":  {"3000"},
		// "decimals" absent: an unticked box submits nothing at all.
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}

	cfg := server.app.Config()
	if cfg.PublishIntervalMs != 2000 || cfg.IdlePublishIntervalMs != 10000 {
		t.Errorf("rates = %d/%d, want 2000 in game and 10000 idle",
			cfg.PublishIntervalMs, cfg.IdlePublishIntervalMs)
	}
	if cfg.Decimals {
		t.Error("decimals stayed on although the box was not submitted")
	}
}

// The sensor set is the first control on the sensors card, and the box below
// it lists both sets out of the catalogue rather than out of a typed-up
// example.
func TestTheSensorSetIsOfferedWithBothListings(t *testing.T) {
	_, ts := newServer(t, nil)

	code, body := get(t, ts.URL+"/capture")
	if code != http.StatusOK {
		t.Fatalf("GET /capture = %d", code)
	}
	if !strings.Contains(body, `name="sensor_set"`) {
		t.Fatal("no way to choose the sensor set")
	}
	// Extended is the default, so it must come up selected.
	if !strings.Contains(body, `<option value="extended" selected>`) {
		t.Error("extended is not preselected")
	}
	// The listing is generated: a measurement from each set has to appear.
	for _, want := range []string{"<details", "<code>fps</code>", "<code>cpu_clock</code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the listing does not contain %q", want)
		}
	}
	// It is the first control, ahead of the group checkboxes.
	if strings.Index(body, `name="sensor_set"`) > strings.Index(body, `name="gpu_enabled"`) {
		t.Error("the sensor set is not the first control on the card")
	}
}

func TestSavingTheSensorSetKeepsIt(t *testing.T) {
	server, ts := newServer(t, nil)

	post(t, ts.URL, "/save/sensors", url.Values{
		"sensor_set":  {"standard"},
		"gpu_enabled": {"1"},
	})
	if got := server.app.Config().SensorSet; got != config.SensorSetStandard {
		t.Errorf("sensor set = %q, want standard", got)
	}

	// And back, because a one-way switch would be a trap.
	post(t, ts.URL, "/save/sensors", url.Values{
		"sensor_set":  {"extended"},
		"gpu_enabled": {"1"},
	})
	if got := server.app.Config().SensorSet; got != config.SensorSetExtended {
		t.Errorf("sensor set = %q, want extended", got)
	}
}

// Within its own block an absent checkbox does mean off, which is how a box
// gets unticked at all.
func TestAnAbsentCheckboxWithinTheBlockMeansOff(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) {
		c.GPUEnabled = true
		c.DiskEnabled = true
	})

	post(t, ts.URL, "/save/sensors", url.Values{"disk_enabled": {"1"}})

	cfg := server.app.Config()
	if cfg.GPUEnabled {
		t.Error("GPU stayed on although its box was not submitted")
	}
	if !cfg.DiskEnabled {
		t.Error("disks were switched off although their box was submitted")
	}
}

// The capture page has to say what PawnIO can do here, whatever the answer is.
// "Install it" and "restart as administrator" are different problems with
// different fixes, and giving someone the wrong one wastes their afternoon.
func TestTheCapturePageReportsWhatPawnIOCanDo(t *testing.T) {
	_, ts := newServer(t, nil)

	code, body := get(t, ts.URL+"/capture")
	if code != http.StatusOK {
		t.Fatalf("GET /capture = %d", code)
	}
	if !strings.Contains(body, `name="pawnio_enabled"`) {
		t.Error("no way to switch PawnIO on")
	}

	// Whatever this machine's state, one of the four sentences must be there,
	// and it must not be the empty string.
	note := pawnIOStatus(i18n.DE)
	if strings.TrimSpace(note) == "" {
		t.Fatal("pawnIOStatus said nothing")
	}
	if !strings.Contains(body, template.HTMLEscapeString(note)) {
		t.Errorf("the page does not carry the PawnIO note %q", note)
	}
	t.Logf("PawnIO note on this machine: %s", note)
}

// Switching PawnIO on must not be a side effect of saving anything else: it
// means running the whole program elevated.
func TestPawnIOStaysOffUnlessAsked(t *testing.T) {
	server, ts := newServer(t, nil)

	post(t, ts.URL, "/save/sensors", url.Values{"gpu_enabled": {"1"}})
	if server.app.Config().PawnIOEnabled {
		t.Error("PawnIO switched itself on")
	}

	post(t, ts.URL, "/save/sensors", url.Values{"pawnio_enabled": {"1"}})
	if !server.app.Config().PawnIOEnabled {
		t.Error("PawnIO did not switch on when it was asked to")
	}
}

func TestUnknownBlockIsRejected(t *testing.T) {
	_, ts := newServer(t, nil)

	if resp := post(t, ts.URL, "/save/passwords", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// A blank secret field means "keep what is stored"; clearing it is a separate,
// deliberate checkbox.
func TestSecretsSurviveASaveThatDoesNotMentionThem(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) { c.MQTTPassword = "s3cret" })

	post(t, ts.URL, "/save/mqtt", url.Values{
		"mqtt_enabled":  {"1"},
		"mqtt_host":     {"broker.example"},
		"mqtt_port":     {"1883"},
		"mqtt_password": {""},
	})
	if got := server.app.Config().MQTTPassword; got != "s3cret" {
		t.Errorf("password = %q, want it kept", got)
	}

	post(t, ts.URL, "/save/mqtt", url.Values{
		"mqtt_enabled":   {"1"},
		"mqtt_host":      {"broker.example"},
		"mqtt_port":      {"1883"},
		"clear_password": {"1"},
	})
	if got := server.app.Config().MQTTPassword; got != "" {
		t.Errorf("password = %q, want it cleared", got)
	}
}

func TestLanguageSwitchAppliesAndReturns(t *testing.T) {
	server, ts := newServer(t, nil)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/language",
		strings.NewReader("lang=en"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", ts.URL+"/capture")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /language: %v", err)
	}
	defer resp.Body.Close()

	if server.app.Config().Lang() != i18n.EN {
		t.Errorf("language = %q, want en", server.app.Config().Lang())
	}
	if location := resp.Header.Get("Location"); location != "/capture" {
		t.Errorf("Location = %q, want the page the switch was used on", location)
	}
}

// The referer decides where the language switch returns to, and it comes from
// the browser: it must not be able to send one somewhere else.
func TestRefererCannotRedirectOffSite(t *testing.T) {
	for _, referer := range []string{"http://evil.example/pwn", "", "::::"} {
		req, _ := http.NewRequest(http.MethodPost, "/language", nil)
		req.Header.Set("Referer", referer)

		if got := backTo(req); !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
			t.Errorf("backTo(%q) = %q, which is not a local path", referer, got)
		}
	}
}

func TestOpenRejectsAnythingNotOffered(t *testing.T) {
	_, ts := newServer(t, nil)

	if resp := post(t, ts.URL, "/open", url.Values{"what": {"../../secrets"}}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// Endpoint URLs are meant to be pasted into a configuration on another
// machine, so a wildcard bind has to become a reachable address.
func TestEndpointsUseAnAddressNotAWildcard(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataServerEnabled = true
	cfg.DataBindAddress = "0.0.0.0"
	cfg.DataPort = 9838
	cfg.DeviceName = "CorganPC2"
	cfg.JSONEnabled = true
	cfg.PrometheusEnabled = true

	endpoints := endpointsFor(cfg, i18n.DE)
	if len(endpoints) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(endpoints))
	}
	for _, e := range endpoints {
		if strings.Contains(e.URL, "0.0.0.0") {
			t.Errorf("%s still points at the wildcard: %s", e.Label, e.URL)
		}
		if !strings.HasPrefix(e.URL, "http://") || !strings.Contains(e.URL, ":9838") {
			t.Errorf("%s has a malformed URL: %s", e.Label, e.URL)
		}
	}

	cfg.DataServerEnabled = false
	if endpointsFor(cfg, i18n.DE) != nil {
		t.Error("endpoints were listed although the server is off")
	}
}

// A specific bind address is what the user chose; it must be left alone.
func TestASpecificBindAddressIsKept(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataServerEnabled = true
	cfg.DataBindAddress = "127.0.0.1"
	cfg.DataPort = 9838
	cfg.JSONEnabled = true
	cfg.PrometheusEnabled = false
	cfg.InfluxPullEnabled = false

	endpoints := endpointsFor(cfg, i18n.DE)
	if len(endpoints) != 1 || !strings.HasPrefix(endpoints[0].URL, "http://127.0.0.1:9838") {
		t.Errorf("endpoints = %+v", endpoints)
	}
}

func TestStatusAPIIsWellFormed(t *testing.T) {
	_, ts := newServer(t, nil)

	code, body := get(t, ts.URL+"/api/status")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	var resp statusResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Every optional group appears, whether or not it has data: the page
	// needs to say "switched off" as much as it needs to show readings.
	if len(resp.Groups) != len(metrics.Groups)-1 {
		t.Errorf("got %d groups, want one per optional group", len(resp.Groups))
	}
	for _, g := range resp.Groups {
		if g.Key == "" || g.Label == "" {
			t.Errorf("group %+v is missing its key or label", g)
		}
	}
}
