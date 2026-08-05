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
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/updater"
)

// newServer wires a server against a real App with everything that would
// reach the network switched off, so the handlers can be exercised without a
// broker, a data server or a collection loop.
func newServer(t *testing.T, mutate func(*config.Config)) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Defaults()
	// Pinned for the same reason as in the hamqtt tests: Defaults takes the
	// language from Windows, and a page rendered in one language cannot be
	// checked against a string fetched in another. A test that depends on the
	// host's locale is not a test. Cases that are about the language switch
	// set it themselves through mutate.
	cfg.Language = string(i18n.DE)
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
// box travels with them. They live on the measurement page now, because all
// but one of them decide how much of the selection reaches a database.
func TestTheCaptureBlockCarriesBothPublishRatesAndTheDecimals(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) { c.Decimals = true })

	post(t, ts.URL, "/save/capture", url.Values{
		"poll_interval_ms": {"500"},
		"interval_ms":      {"2000"},
		"idle_interval_ms": {"20000"},
		"idle_timeout_ms":  {"5000"},
	})

	cfg := server.app.Config()
	if cfg.PublishIntervalMs != 2000 || cfg.IdlePublishIntervalMs != 20000 {
		t.Errorf("publish rates = %d / %d, want 2000 / 20000",
			cfg.PublishIntervalMs, cfg.IdlePublishIntervalMs)
	}
	// The box was left out of the form, which within its own block means off.
	if cfg.Decimals {
		t.Error("the decimals survived a save that did not mention them")
	}
}

// The measurement page carries the whole choice now: a rung to slide between
// and a tick per measurement, generated from the catalogue rather than from a
// typed-up example.
func TestTheMeasurementPageOffersTheRungAndTheTree(t *testing.T) {
	_, ts := newServer(t, nil)

	code, body := get(t, ts.URL+"/measurements")
	if code != http.StatusOK {
		t.Fatalf("GET /measurements = %d", code)
	}
	if !strings.Contains(body, `id="rung-slider"`) {
		t.Error("there is no slider")
	}
	// One tick per measurement, out of the catalogue.
	for _, want := range []string{`value="fps"`, `value="cpu_clock"`, `value="gpu_engine_3d"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the tree does not offer %q", want)
		}
	}
	// Counted on the attribute pair, not on the name alone: the script below
	// selects the same boxes and would otherwise be counted as three more.
	if got := strings.Count(body, `name="measurement" value=`); got != len(metrics.All) {
		t.Errorf("%d ticks for %d measurements", got, len(metrics.All))
	}
	// The estimate needs its inputs, and the assumptions have to be on the
	// page rather than buried in the number.
	for _, want := range []string{`id="est-entities"`, `id="est-rows"`, `id="est-size"`, `id="est-basis"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the estimate is missing %q", want)
		}
	}
	// The capture settings moved here, because all but one of them decide how
	// much of the selection reaches a database.
	if !strings.Contains(body, `name="interval_ms"`) || !strings.Contains(body, `name="decimals"`) {
		t.Error("the capture settings did not come along")
	}
	// And they are gone from where they used to be.
	if _, capture := get(t, ts.URL+"/capture"); strings.Contains(capture, `name="sensor_set"`) {
		t.Error("the old sensor-set control is still on the capture page")
	}
}

// The form that carries the ticks has to carry the rung with them.
//
// Without it the rung arrives empty, saveMeasurements falls back to extended,
// and ticking a single box on the minimal rung quietly switches every
// measurement on. That is what it did.
func TestTheTickFormCarriesTheRungItIsMeasuredAgainst(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) {
		c.Measurements.Preset = config.PresetMinimal
	})

	_, body := get(t, ts.URL+"/measurements")
	want := `<input type="hidden" name="preset" id="picks-preset" value="` + config.PresetMinimal + `">`
	if !strings.Contains(body, want) {
		t.Error("the tick form does not say which rung the ticks are exceptions to")
	}
}

// Every change on that page is applied where it is made, so the page posts in
// the background and has no use for the redirect a form submit gets.
func TestAQuietSaveAnswersWithoutARedirect(t *testing.T) {
	server, ts := newServer(t, nil)

	form := url.Values{"preset": {config.PresetMinimal}}
	request, err := http.NewRequest(http.MethodPost, ts.URL+"/save/measurements", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Quiet", "1")

	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNoContent {
		t.Errorf("quiet save = %d, want 204", response.StatusCode)
	}
	if got := server.app.Config().Measurements.Preset; got != config.PresetMinimal {
		t.Errorf("rung = %q, want minimal — the save did not take", got)
	}
}

// Sliding the rung is a different statement from ticking a box: it means
// "forget what I picked and give me this".
func TestSlidingTheRungReplacesTheHandPickedChoice(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) {
		c.Measurements.Preset = config.PresetExtended
		c.Measurements.Added = []string{"cpu_clock"}
		c.Measurements.Removed = []string{"fps"}
	})

	post(t, ts.URL, "/rung", url.Values{"preset": {config.PresetMinimal}})

	got := server.app.Config().Measurements
	if got.Preset != config.PresetMinimal {
		t.Errorf("rung = %q, want minimal", got.Preset)
	}
	if len(got.Added) != 0 || len(got.Removed) != 0 {
		t.Errorf("the exceptions survived the slider: %+v", got)
	}
}

// Ticking boxes stores the difference from the rung, not the list of ticks —
// so a measurement the catalogue gains later joins the rung by itself.
func TestTickingBoxesStoresTheDifferenceFromTheRung(t *testing.T) {
	server, ts := newServer(t, nil)

	// Everything the minimal rung carries, minus fps, plus cpu_clock.
	form := url.Values{"preset": {config.PresetMinimal}}
	for _, id := range metrics.PresetIDs(metrics.PresetMinimal) {
		if id != metrics.FPS.ID {
			form.Add("measurement", id)
		}
	}
	form.Add("measurement", metrics.CPUClock.ID)

	post(t, ts.URL, "/save/measurements", form)

	got := server.app.Config().Measurements
	if got.Preset != config.PresetMinimal {
		t.Fatalf("rung = %q, want minimal", got.Preset)
	}
	if len(got.Added) != 1 || got.Added[0] != metrics.CPUClock.ID {
		t.Errorf("added = %v, want just the clock", got.Added)
	}
	if len(got.Removed) != 1 || got.Removed[0] != metrics.FPS.ID {
		t.Errorf("removed = %v, want just fps", got.Removed)
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
	server, ts := newServer(t, nil)

	code, body := get(t, ts.URL+"/capture")
	if code != http.StatusOK {
		t.Fatalf("GET /capture = %d", code)
	}
	if !strings.Contains(body, `name="pawnio_enabled"`) {
		t.Error("no way to switch PawnIO on")
	}

	// Whatever this machine's state, one of the four sentences must be there,
	// and it must not be the empty string.
	//
	// Fetched in the language the page was rendered in, not a fixed one:
	// comparing a German sentence against an English page is how this failed on
	// the build server, and hard-coding the language here would only move the
	// trap rather than remove it.
	note := pawnIOStatus(server.app.Config().Lang())
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
	// Every optional group appears, whether or not it has data: the page needs
	// to say "switched off" as much as it needs to show readings. Two do not.
	// The core group has its own tiles, and the battery is left out where
	// there is none — this server has no collection loop, so there is none.
	present := map[string]bool{}
	for _, g := range resp.Groups {
		if g.Key == "" || g.Label == "" {
			t.Errorf("group %+v is missing its key or label", g)
		}
		present[g.Key] = true
	}
	for _, group := range metrics.Groups {
		want := group != metrics.GroupCore && group != metrics.GroupBattery
		if got := present[string(group)]; got != want {
			t.Errorf("group %q present = %v, want %v", group, got, want)
		}
	}
}

// A missing battery is not a fault to report. Most machines are desktops, and a
// permanently empty panel saying so would be noise on every page load — the
// screenshot that prompted this showed "BATTERY — No data." on a tower.
func TestABatteryPanelOnlyExistsWhereThereIsABattery(t *testing.T) {
	status := func(fill func(*collector.Snapshot)) app.Status {
		snap := collector.Snapshot{SourceErrors: map[metrics.Group]string{}}
		if fill != nil {
			fill(&snap)
		}
		return app.Status{Config: config.Defaults(), Snapshot: snap}
	}
	hasBattery := func(groups []groupStatus) (groupStatus, bool) {
		for _, group := range groups {
			if group.Key == string(metrics.GroupBattery) {
				return group, true
			}
		}
		return groupStatus{}, false
	}

	t.Run("a desktop gets no panel", func(t *testing.T) {
		groups := groupStatuses(status(nil), i18n.DE)
		if group, found := hasBattery(groups); found {
			t.Errorf("a machine without a battery got a panel: %+v", group)
		}
		// The other groups still report their absence, which is the behaviour
		// this exception is carved out of.
		if len(groups) == 0 {
			t.Fatal("every group disappeared, not just the battery")
		}
	})

	t.Run("a laptop gets one", func(t *testing.T) {
		groups := groupStatuses(status(func(snap *collector.Snapshot) {
			snap.Set.Add(metrics.Gauge(metrics.BatteryCharge, "", 87))
		}), i18n.DE)

		group, found := hasBattery(groups)
		if !found {
			t.Fatal("a machine with a battery got no panel")
		}
		if !group.Available || len(group.Rows) == 0 {
			t.Errorf("the panel is there but empty: %+v", group)
		}
	})

	// A battery that is present but unreadable is a real failure, and hiding it
	// would leave somebody with a laptop wondering where the readings went.
	t.Run("a failure is still shown", func(t *testing.T) {
		groups := groupStatuses(status(func(snap *collector.Snapshot) {
			snap.SourceErrors[metrics.GroupBattery] = "the battery device would not answer"
		}), i18n.DE)

		group, found := hasBattery(groups)
		if !found {
			t.Fatal("a battery source that failed was hidden")
		}
		if group.Error == "" {
			t.Error("the panel is there but says nothing about the failure")
		}
	})
}

// fakeUpdates stands in for the real manager. Its methods are exported, which
// is what lets a test outside package app satisfy an interface declared there
// without exporting the interface itself.
type fakeUpdates struct {
	state      updater.State
	installed  int
	installErr error
	enabled    bool
}

func (f *fakeUpdates) State() updater.State                 { return f.state }
func (f *fakeUpdates) Subscribe(func(updater.State)) func() { return func() {} }
func (f *fakeUpdates) RequestInstall() error {
	f.installed++
	return f.installErr
}
func (f *fakeUpdates) Start()                  {}
func (f *fakeUpdates) Stop()                   {}
func (f *fakeUpdates) SetCheckEnabled(on bool) { f.enabled = on }

func newServerWithUpdates(t *testing.T, updates *fakeUpdates) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Defaults()
	cfg.Language = string(i18n.DE)
	cfg.MQTTEnabled = false
	cfg.DataServerEnabled = false
	cfg.NodeID = "corganpc2"
	cfg.Normalize()

	application := app.New(cfg, t.TempDir()+`\config.json`, applog.Discard(), updates)
	server, err := New(application, applog.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	httpServer := httptest.NewServer(server.server.Handler)
	t.Cleanup(httpServer.Close)
	return server, httpServer
}

// The box only appears when there is something newer. An exporter that is up
// to date shows nothing at all rather than a reassuring green nothing.
func TestTheUpdateBoxOnlyAppearsWhenThereIsSomethingNewer(t *testing.T) {
	upToDate := &fakeUpdates{state: updater.State{
		InstalledVersion: config.Version, LatestVersion: config.Version,
	}}
	_, ts := newServerWithUpdates(t, upToDate)

	var resp statusResponse
	_, body := get(t, ts.URL+"/api/status")
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Update.Available {
		t.Errorf("an update was offered against an identical version: %+v", resp.Update)
	}

	newer := &fakeUpdates{state: updater.State{
		InstalledVersion: config.Version,
		LatestVersion:    "9.9.9",
		Title:            "v9.9.9",
		ReleaseURL:       "https://example.invalid/v9.9.9",
	}}
	_, ts2 := newServerWithUpdates(t, newer)

	_, body = get(t, ts2.URL+"/api/status")
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Update.Available {
		t.Fatalf("no update offered although 9.9.9 exists: %+v", resp.Update)
	}
	if resp.Update.Latest != "9.9.9" || resp.Update.URL == "" {
		t.Errorf("the box has nothing to show: %+v", resp.Update)
	}
}

// The button is the whole point of point three: an install that a person asks
// for, rather than one only Home Assistant can trigger.
func TestTheInstallButtonAsksTheUpdater(t *testing.T) {
	updates := &fakeUpdates{state: updater.State{
		InstalledVersion: config.Version, LatestVersion: "9.9.9",
	}}
	_, ts := newServerWithUpdates(t, updates)

	resp := post(t, ts.URL, "/update", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect back to the page", resp.StatusCode)
	}
	if updates.installed != 1 {
		t.Errorf("the updater was asked %d times, want once", updates.installed)
	}
}

// A build without a working updater must refuse rather than panic, and say so
// where the user can read it.
func TestTheInstallButtonRefusesWithoutAnUpdater(t *testing.T) {
	_, ts := newServer(t, nil)

	resp := post(t, ts.URL, "/update", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); !strings.Contains(location, "error=") {
		t.Errorf("redirected to %q without saying what went wrong", location)
	}
}
