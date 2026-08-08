package dataserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

func testSnapshot() collector.Snapshot {
	snap := collector.Snapshot{RTSSStatus: collector.RTSSOK}
	snap.Add(
		metrics.Gauge(metrics.FPS, "", 143.2),
		metrics.Gauge(metrics.Frametime, "", 6.98),
		metrics.Text(metrics.Game, "", "Cyberpunk2077.exe"),
		metrics.Bool(metrics.GameRunning, "", true),
		metrics.Text(metrics.Resolution, "", "2560x1440"),
		metrics.Gauge(metrics.RefreshRate, "", 165),
		metrics.Gauge(metrics.CPULoad, "", 24.5),
		metrics.Gauge(metrics.RAMLoad, "", 51.3),
		metrics.Bool(metrics.RTSSUp, "", true),
		// A second GPU-style instance proves the instanced readings survive
		// every rendering path.
		metrics.Gauge(metrics.GPUTemperature, "0", 61.5),
	)
	return snap
}

// newTestServer returns a server wired to httptest, with one snapshot already
// exported unless withData is false.
func newTestServer(t *testing.T, mutate func(*config.Config), withData bool) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Defaults()
	cfg.NodeID = "corganpc2"
	cfg.DataServerEnabled = true
	cfg.JSONEnabled = true
	cfg.PrometheusEnabled = true
	cfg.InfluxPullEnabled = true
	if mutate != nil {
		mutate(&cfg)
	}

	s := New(cfg, applog.Discard())
	s.now = func() time.Time { return time.Unix(1700000000, 0) }
	if withData {
		if err := s.Export(testSnapshot()); err != nil {
			t.Fatalf("Export: %v", err)
		}
	}

	httpServer := httptest.NewServer(s.server.Handler)
	t.Cleanup(httpServer.Close)
	return s, httpServer
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

func TestJSONEndpointServesTheState(t *testing.T) {
	_, ts := newTestServer(t, nil, true)

	code, body := get(t, ts.URL+PathJSON)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}

	var state map[string]any
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state["fps"] != 143.2 || state["game"] != "Cyberpunk2077.exe" || state["refresh_rate"] != float64(165) {
		t.Errorf("state = %+v", state)
	}
	// Instanced readings must be keyed with their instance appended.
	if state["gpu0_temperature"] != 61.5 {
		t.Errorf("gpu0_temperature = %v, want 61.5", state["gpu0_temperature"])
	}
}

func TestPrometheusEndpointServesTheExposition(t *testing.T) {
	_, ts := newTestServer(t, nil, true)

	code, body := get(t, ts.URL+PathPrometheus)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, `rig_fps{host="corganpc2"} 143.2`) {
		t.Errorf("exposition:\n%s", body)
	}
}

func TestInfluxEndpointServesLineProtocol(t *testing.T) {
	_, ts := newTestServer(t, nil, true)

	code, body := get(t, ts.URL+PathInflux)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.HasPrefix(body, "rig,host=corganpc2,") {
		t.Errorf("line protocol:\n%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "1700000000000000000") {
		t.Errorf("timestamp missing:\n%s", body)
	}
}

func TestDisabledFormatsReturnNotFound(t *testing.T) {
	_, ts := newTestServer(t, func(c *config.Config) {
		c.PrometheusEnabled = false
		c.InfluxPullEnabled = false
	}, true)

	for _, path := range []string{PathPrometheus, PathInflux} {
		if code, _ := get(t, ts.URL+path); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
	if code, _ := get(t, ts.URL+PathJSON); code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", PathJSON, code)
	}
}

func TestEndpointsWaitForTheFirstReading(t *testing.T) {
	_, ts := newTestServer(t, nil, false)

	for _, path := range []string{PathJSON, PathPrometheus, PathInflux, PathHealth} {
		if code, _ := get(t, ts.URL+path); code != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d, want 503 before the first snapshot", path, code)
		}
	}
}

func TestTokenIsRequiredWhenConfigured(t *testing.T) {
	_, ts := newTestServer(t, func(c *config.Config) { c.DataToken = "s3cret" }, true)

	if code, _ := get(t, ts.URL+PathJSON); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated request = %d, want 401", code)
	}
	if code, _ := get(t, ts.URL+PathJSON+"?token=s3cret"); code != http.StatusOK {
		t.Errorf("query token = %d, want 200", code)
	}
	if code, _ := get(t, ts.URL+PathJSON+"?token=wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", code)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+PathPrometheus, nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bearer request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bearer token = %d, want 200", resp.StatusCode)
	}
}

// A liveness probe must not need the token, or monitoring has to be given a
// credential just to check that the process is up.
func TestHealthNeverRequiresAToken(t *testing.T) {
	_, ts := newTestServer(t, func(c *config.Config) { c.DataToken = "s3cret" }, true)

	code, body := get(t, ts.URL+PathHealth)
	if code != http.StatusOK || strings.TrimSpace(body) != "ok" {
		t.Errorf("health = %d %q", code, body)
	}
}

// Setting a token has to quieten the whole port, index page included.
//
// The index names the version and the node id and states in writing that a
// token is required, which tells an unauthenticated caller that the host is
// worth another look. The listener defaults to 0.0.0.0, so "another look" can
// come from anywhere on the network.
func TestTheIndexRequiresTheTokenToo(t *testing.T) {
	_, ts := newTestServer(t, func(c *config.Config) {
		c.DataToken = "s3cret"
		c.NodeID = "corganpc3"
	}, true)

	code, body := get(t, ts.URL+"/")
	if code != http.StatusUnauthorized {
		t.Errorf("GET / without a token = %d, want %d", code, http.StatusUnauthorized)
	}
	for _, leak := range []string{config.Version, "corganpc3", "token is required"} {
		if strings.Contains(body, leak) {
			t.Errorf("unauthenticated index leaks %q:\n%s", leak, body)
		}
	}
}

// Every deadline has to be set, not just the header one.
//
// Go falls IdleTimeout back to ReadTimeout, so leaving both unset means no idle
// deadline is ever applied and a keep-alive connection is held for the lifetime
// of the process. This listener defaults to 0.0.0.0 and /health needs no token,
// so the request that holds a connection open is free.
//
// The same four are set on the web interface's server. They were written from
// the same two-line block and are the kind of thing that comes apart when only
// one side is touched, so both sides are pinned.
func TestTheServerSetsEveryDeadline(t *testing.T) {
	s, _ := newTestServer(t, nil, true)

	for name, got := range map[string]time.Duration{
		"ReadHeaderTimeout": s.server.ReadHeaderTimeout,
		"ReadTimeout":       s.server.ReadTimeout,
		"WriteTimeout":      s.server.WriteTimeout,
		"IdleTimeout":       s.server.IdleTimeout,
	} {
		if got <= 0 {
			t.Errorf("%s = %v; an unset deadline is never applied", name, got)
		}
	}
}

// With no token configured the port is open by definition, and the index is
// the one thing that explains what the port is. It must keep working.
func TestTheIndexStaysOpenWithoutAToken(t *testing.T) {
	_, ts := newTestServer(t, func(c *config.Config) { c.NodeID = "corganpc3" }, true)

	code, body := get(t, ts.URL+"/")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "corganpc3") {
		t.Errorf("the index no longer says which machine this is:\n%s", body)
	}
}

func TestExportKeepsOnlyTheLatestReading(t *testing.T) {
	s, ts := newTestServer(t, nil, true)

	newer := testSnapshot()
	newer.Add(metrics.Gauge(metrics.FPS, "", 61.5))
	if err := s.Export(newer); err != nil {
		t.Fatalf("Export: %v", err)
	}

	_, body := get(t, ts.URL+PathJSON)
	if !strings.Contains(body, `"fps":61.5`) {
		t.Errorf("served a stale reading:\n%s", body)
	}
}

func TestStatusListsTheEnabledFormats(t *testing.T) {
	s, _ := newTestServer(t, nil, true)

	// Start was never called in this test, so the address is empty; the
	// format list is what is being checked here.
	if got := strings.Join(s.formats(), ","); got != "JSON,Prometheus,Influx" {
		t.Errorf("formats = %q", got)
	}
	if s.Status().Delivered != 1 {
		t.Errorf("Delivered = %d, want 1", s.Status().Delivered)
	}
}

func TestStartBindsAndServes(t *testing.T) {
	cfg := config.Defaults()
	cfg.NodeID = "corganpc2"
	cfg.DataBindAddress = "127.0.0.1"
	cfg.DataPort = 0 // let the OS pick a free port
	cfg.JSONEnabled = true

	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	if err := s.Export(testSnapshot()); err != nil {
		t.Fatalf("Export: %v", err)
	}

	status := s.Status()
	if !status.Healthy {
		t.Fatalf("status = %+v", status)
	}

	address := strings.TrimPrefix(strings.Split(status.Detail, " ")[0], "http://")
	code, body := get(t, "http://"+address+PathJSON)
	if code != http.StatusOK || !strings.Contains(body, `"fps":143.2`) {
		t.Errorf("GET %s = %d %s", address+PathJSON, code, body)
	}
}
