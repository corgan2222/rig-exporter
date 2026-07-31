package influxpush

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/corgan/rig-exporter/internal/applog"
	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/metrics"
)

type capturedWrite struct {
	path  string
	query url.Values
	auth  string
	body  string
}

// newInflux stands in for an InfluxDB server and reports what was written.
func newInflux(t *testing.T, status int) (*httptest.Server, <-chan capturedWrite) {
	t.Helper()

	writes := make(chan capturedWrite, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		writes <- capturedWrite{
			path:  r.URL.Path,
			query: r.URL.Query(),
			auth:  r.Header.Get("Authorization"),
			body:  string(body),
		}
		if status >= 400 {
			http.Error(w, "bucket not found", status)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, writes
}

func newClient(t *testing.T, influxURL string, mutate func(*config.Config)) *Client {
	t.Helper()

	cfg := config.Defaults()
	cfg.NodeID = "corganpc2"
	cfg.InfluxPushEnabled = true
	cfg.InfluxURL = influxURL
	cfg.InfluxOrg = "home"
	cfg.InfluxBucket = "gaming"
	cfg.InfluxToken = "tok3n"
	cfg.InfluxMeasurement = "rig"
	if mutate != nil {
		mutate(&cfg)
	}

	c := New(cfg, applog.Discard())
	c.now = func() time.Time { return time.Unix(1700000000, 0) }
	return c
}

func snapshot() collector.Snapshot {
	snap := collector.Snapshot{RTSSStatus: collector.RTSSOK}
	snap.Add(
		metrics.Gauge(metrics.FPS, "", 143.2),
		metrics.Text(metrics.Game, "", "Cyberpunk2077.exe"),
		metrics.Text(metrics.Resolution, "", "2560x1440"),
		metrics.Gauge(metrics.RefreshRate, "", 165),
		metrics.Gauge(metrics.CPULoad, "", 24.5),
		metrics.Gauge(metrics.RAMLoad, "", 51.3),
		metrics.Bool(metrics.RTSSUp, "", true),
	)
	return snap
}

func waitFor(t *testing.T, writes <-chan capturedWrite) capturedWrite {
	t.Helper()

	select {
	case w := <-writes:
		return w
	case <-time.After(3 * time.Second):
		t.Fatal("no write reached the server")
		return capturedWrite{}
	}
}

func TestWritesLineProtocolToTheV2API(t *testing.T) {
	server, writes := newInflux(t, http.StatusNoContent)

	c := newClient(t, server.URL, nil)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	if err := c.Export(snapshot()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	got := waitFor(t, writes)

	if got.path != "/api/v2/write" {
		t.Errorf("path = %q", got.path)
	}
	if got.query.Get("bucket") != "gaming" || got.query.Get("org") != "home" {
		t.Errorf("query = %v", got.query)
	}
	if got.query.Get("precision") != "ns" {
		t.Errorf("precision = %q, want ns", got.query.Get("precision"))
	}
	if got.auth != "Token tok3n" {
		t.Errorf("authorization = %q", got.auth)
	}
	if !strings.HasPrefix(got.body, "rig,host=corganpc2,") || !strings.Contains(got.body, "fps=143.2") {
		t.Errorf("body = %q", got.body)
	}
}

func TestOrgIsOmittedForInfluxV1(t *testing.T) {
	server, writes := newInflux(t, http.StatusNoContent)

	c := newClient(t, server.URL, func(cfg *config.Config) { cfg.InfluxOrg = "" })
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	c.Export(snapshot())
	got := waitFor(t, writes)

	if _, present := got.query["org"]; present {
		t.Errorf("org was sent even though it is unset: %v", got.query)
	}
}

func TestServerErrorIsReportedInStatus(t *testing.T) {
	server, writes := newInflux(t, http.StatusNotFound)

	c := newClient(t, server.URL, nil)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	c.Export(snapshot())
	waitFor(t, writes)

	// The worker records the failure just after responding, so give it a
	// moment before reading the status.
	deadline := time.Now().Add(2 * time.Second)
	var status = c.Status()
	for time.Now().Before(deadline) && status.Healthy {
		time.Sleep(10 * time.Millisecond)
		status = c.Status()
	}

	if status.Healthy {
		t.Fatalf("status still healthy after a 404: %+v", status)
	}
	if !strings.Contains(status.Detail, "404") {
		t.Errorf("detail = %q, want it to mention the status code", status.Detail)
	}
	if status.Delivered != 0 {
		t.Errorf("Delivered = %d, want 0", status.Delivered)
	}
}

func TestStartRefusesAnIncompleteTarget(t *testing.T) {
	c := newClient(t, "", func(cfg *config.Config) { cfg.InfluxURL = "" })

	if err := c.Start(); err == nil {
		t.Error("Start accepted an empty URL")
	}
}

// Export must never block the collection loop, even while a write is in
// flight against a server that is not responding.
func TestExportDoesNotBlockOnASlowServer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := newClient(t, server.URL, nil)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Release the handler before stopping, so Stop does not have to wait out
	// the HTTP client timeout.
	defer func() {
		close(release)
		c.Stop()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			c.Export(snapshot())
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Export blocked while the server was unresponsive")
	}
}
