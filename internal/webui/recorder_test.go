//go:build windows

package webui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/metrics"
)

// snapshotWith builds a reading set by hand, so a test does not depend on what
// the machine running it happens to have plugged in.
func snapshotWith(readings ...metrics.Reading) collector.Snapshot {
	var snap collector.Snapshot
	snap.Add(readings...)
	return snap
}

// The whole point of generating the block instead of printing an example is
// that the entity ids are the ones this machine actually publishes.
func TestTheRecorderSnippetNamesTheEntitiesThatExist(t *testing.T) {
	cfg := config.Defaults()
	cfg.NodeID = "corgan_pc3"

	snippet := recorderSnippet(cfg, snapshotWith(
		metrics.Gauge(metrics.FPS, "", 143),
		metrics.Gauge(metrics.CPULoad, "", 37),
		metrics.Gauge(metrics.GPUTemperature, "0", 45),
		metrics.Gauge(metrics.GPUTemperature, "1", 46),
		metrics.Gauge(metrics.GPUCoreClock, "0", 1515), // momentary, not worth keeping
		metrics.Text(metrics.GPUName, "0", "RTX 2080"), // static, not worth keeping
	))

	for _, want := range []string{
		"      - sensor.re_corgan_pc3_fps\n",
		"      - sensor.re_corgan_pc3_cpu\n",
		"      - sensor.re_corgan_pc3_gpu0_temperature\n",
		"      - sensor.re_corgan_pc3_gpu1_temperature\n",
	} {
		if !strings.Contains(snippet, want) {
			t.Errorf("missing include line %q in:\n%s", strings.TrimSpace(want), snippet)
		}
	}
	for _, unwanted := range []string{"gpu0_core_clock", "gpu0_name"} {
		if strings.Contains(snippet, unwanted) {
			t.Errorf("%s was included although its history says nothing:\n%s", unwanted, snippet)
		}
	}
}

// Two graphics cards give two lines, one gives one. A snippet that has to be
// corrected by hand before pasting is worse than none.
func TestTheRecorderSnippetFollowsTheHardwareThatIsThere(t *testing.T) {
	cfg := config.Defaults()
	cfg.NodeID = "pc"

	one := recorderSnippet(cfg, snapshotWith(metrics.Gauge(metrics.GPUTemperature, "0", 45)))
	if strings.Contains(one, "gpu1_temperature") {
		t.Errorf("a second card appeared out of nowhere:\n%s", one)
	}

	// A machine with every sensor group switched off still needs the excludes:
	// the entities are already in Home Assistant from before.
	none := recorderSnippet(cfg, snapshotWith())
	if !strings.Contains(none, "- sensor.re_pc_*") || !strings.Contains(none, "- binary_sensor.re_pc_*") {
		t.Errorf("the excludes went missing when there was nothing to include:\n%s", none)
	}
	if strings.Contains(none, "include:") {
		t.Errorf("an empty include list was written out, which is invalid YAML:\n%s", none)
	}
}

// The glob has to match the entity ids this program really produces, or it
// silently protects nothing.
func TestTheExcludeGlobMatchesTheEntityIds(t *testing.T) {
	cfg := config.Defaults()
	cfg.NodeID = "corganpc2"

	prefix := cfg.ObjectPrefix()
	if got := cfg.ObjectID("cpu"); !strings.HasPrefix(got, prefix) {
		t.Fatalf("entity id %q does not start with the glob prefix %q", got, prefix)
	}
	if !strings.Contains(recorderSnippet(cfg, snapshotWith()), "- sensor."+prefix+"*") {
		t.Error("the snippet does not use the prefix the entity ids actually carry")
	}
}

// The card sits on the export page and carries the generated block, not a
// placeholder.
func TestTheExportPageShowsTheRecorderBlock(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) { c.NodeID = "corganpc2" })

	code, body := get(t, ts.URL+"/export")
	if code != http.StatusOK {
		t.Fatalf("GET /export = %d", code)
	}
	if !strings.Contains(body, `id="recorder"`) {
		t.Error("the storage card is not on the page")
	}
	if !strings.Contains(body, "purge_keep_days") || !strings.Contains(body, "sensor.re_corganpc2_*") {
		t.Error("the page does not carry the generated recorder block")
	}
}

// The tab icon is the tray icon, served from the same compiled-in ICO.
func TestTheFaviconIsTheTrayIcon(t *testing.T) {
	_, ts := newServer(t, nil)

	resp, err := http.Get(ts.URL + "/favicon.ico")
	if err != nil {
		t.Fatalf("GET /favicon.ico: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/x-icon" {
		t.Errorf("content type = %q", got)
	}
	if resp.ContentLength <= 0 {
		t.Error("the icon was served empty")
	}
}
