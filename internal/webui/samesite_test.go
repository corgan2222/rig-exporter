//go:build windows

package webui

import (
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/config"
)

// postFrom submits a form the way a particular browser situation would, so a
// test can say where the request came from.
func postFrom(t *testing.T, base, path string, values url.Values, headers map[string]string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, base+path,
		strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// A form post is a CORS simple request: no preflight, no permission asked. So a
// visited web page can submit one here, and without this check it lands.
func TestACrossSitePostIsRefused(t *testing.T) {
	server, ts := newServer(t, nil)
	before := server.app.Paused()

	resp := postFrom(t, ts.URL, "/pause", nil, map[string]string{
		"Sec-Fetch-Site": "cross-site",
		"Origin":         "http://angreifer.example",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if server.app.Paused() != before {
		t.Error("a cross-site post changed the paused state")
	}
}

// Every way in has to be covered, not just the one that was tested. These are
// the eight posts this interface accepts; a new one that forgets the middleware
// would be a new hole.
func TestEveryPostIsCoveredByTheCheck(t *testing.T) {
	paths := []string{
		"/save/mqtt", "/pause", "/language", "/open", "/dismiss",
		"/update", "/rung", "/logs/clear",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			_, ts := newServer(t, nil)

			resp := postFrom(t, ts.URL, path, nil, map[string]string{
				"Sec-Fetch-Site": "cross-site",
			})

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("POST %s from a foreign site = %d, want 403", path, resp.StatusCode)
			}
		})
	}
}

// A browser marks its own page's posts as same-origin, and a link typed into
// the address bar as none. Both are this interface being used as intended.
func TestThePagesOwnPostsStillWork(t *testing.T) {
	for _, site := range []string{"same-origin", "none"} {
		t.Run(site, func(t *testing.T) {
			server, ts := newServer(t, nil)
			before := server.app.Paused()

			resp := postFrom(t, ts.URL, "/pause", nil, map[string]string{
				"Sec-Fetch-Site": site,
				"Origin":         ts.URL,
			})

			if resp.StatusCode == http.StatusForbidden {
				t.Fatalf("the page's own post was refused")
			}
			if server.app.Paused() == before {
				t.Error("the page's own post did nothing")
			}
		})
	}
}

// An Origin from this listener is this listener, whatever a client does or does
// not say about the fetch site.
func TestAnOriginThatMatchesTheListenerIsAccepted(t *testing.T) {
	server, ts := newServer(t, nil)
	before := server.app.Paused()

	resp := postFrom(t, ts.URL, "/pause", nil, map[string]string{"Origin": ts.URL})

	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("a post from this listener's own origin was refused")
	}
	if server.app.Paused() == before {
		t.Error("the post did nothing")
	}
}

// And an Origin naming somebody else is not, even from a client that sends no
// Sec-Fetch-Site at all.
func TestAForeignOriginIsRefusedWithoutTheFetchHeader(t *testing.T) {
	_, ts := newServer(t, nil)

	resp := postFrom(t, ts.URL, "/pause", nil, map[string]string{
		"Origin": "http://angreifer.example",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

// Reading is not changing. A GET must not be caught by any of this, or the
// page stops loading from a bookmark.
func TestReadingIsNeverRefused(t *testing.T) {
	_, ts := newServer(t, nil)

	for _, path := range []string{"/", "/export", "/measurements", "/api/status"} {
		code, _ := get(t, ts.URL+path)
		if code == http.StatusForbidden {
			t.Errorf("GET %s was refused", path)
		}
	}
}

// A secret belongs to the target it was entered for. Moving the target without
// supplying the secret drops it — otherwise "change the URL" is a way to send
// the stored token to a stranger without ever having read it.
func TestMovingTheInfluxTargetDropsTheStoredToken(t *testing.T) {
	server, ts := newServer(t, func(cfg *config.Config) {
		cfg.InfluxPushEnabled = true
		cfg.InfluxURL = "http://influx.local:8086"
		cfg.InfluxToken = "geheim"
	})

	post(t, ts.URL, "/save/influx", url.Values{
		"influx_push_enabled": {"1"},
		"influx_url":          {"http://angreifer.example/"},
		"influx_org":          {"x"},
		"influx_bucket":       {"y"},
		"influx_measurement":  {"z"},
	})

	if got := server.app.Config().InfluxToken; got != "" {
		t.Fatalf("InfluxToken = %q after the target moved; want it dropped", got)
	}
}

// The same for the broker, whose target is the host and the port together.
func TestMovingTheBrokerDropsTheStoredPassword(t *testing.T) {
	for _, move := range []struct {
		name string
		form url.Values
	}{
		{"host", url.Values{"mqtt_host": {"angreifer.example"}, "mqtt_port": {"1883"}}},
		{"port", url.Values{"mqtt_host": {"broker.local"}, "mqtt_port": {"1884"}}},
	} {
		t.Run(move.name, func(t *testing.T) {
			server, ts := newServer(t, func(cfg *config.Config) {
				cfg.MQTTEnabled = true
				cfg.MQTTHost = "broker.local"
				cfg.MQTTPort = 1883
				cfg.MQTTPassword = "geheim"
			})

			form := url.Values{"mqtt_enabled": {"1"}, "mqtt_username": {"beliebig"}}
			maps.Copy(form, move.form)
			post(t, ts.URL, "/save/mqtt", form)

			if got := server.app.Config().MQTTPassword; got != "" {
				t.Fatalf("MQTTPassword = %q after the broker moved; want it dropped", got)
			}
		})
	}
}

// Saving a page without touching the target keeps the secret. This is the rule
// the whole interface rests on — the page never round-trips a secret, so a save
// that drops one would lose it on every unrelated edit.
func TestSavingWithoutMovingTheTargetKeepsTheSecret(t *testing.T) {
	server, ts := newServer(t, func(cfg *config.Config) {
		cfg.MQTTEnabled = true
		cfg.MQTTHost = "broker.local"
		cfg.MQTTPort = 1883
		cfg.MQTTPassword = "geheim"
		cfg.InfluxPushEnabled = true
		cfg.InfluxURL = "http://influx.local:8086"
		cfg.InfluxToken = "auch-geheim"
	})

	post(t, ts.URL, "/save/mqtt", url.Values{
		"mqtt_enabled":  {"1"},
		"mqtt_host":     {"broker.local"},
		"mqtt_port":     {"1883"},
		"mqtt_username": {"ein-anderer-name"},
	})
	post(t, ts.URL, "/save/influx", url.Values{
		"influx_push_enabled": {"1"},
		"influx_url":          {"http://influx.local:8086"},
		"influx_org":          {"eine-andere-org"},
		"influx_bucket":       {"y"},
		"influx_measurement":  {"z"},
	})

	cfg := server.app.Config()
	if cfg.MQTTPassword != "geheim" {
		t.Errorf("MQTTPassword = %q; an unrelated edit lost it", cfg.MQTTPassword)
	}
	if cfg.InfluxToken != "auch-geheim" {
		t.Errorf("InfluxToken = %q; an unrelated edit lost it", cfg.InfluxToken)
	}
}

// Moving the target and supplying the new secret in the same save is the
// ordinary way to move a broker. It must not drop what was just typed in.
func TestMovingTheTargetWithANewSecretKeepsTheNewOne(t *testing.T) {
	server, ts := newServer(t, func(cfg *config.Config) {
		cfg.MQTTEnabled = true
		cfg.MQTTHost = "broker.local"
		cfg.MQTTPort = 1883
		cfg.MQTTPassword = "alt"
	})

	post(t, ts.URL, "/save/mqtt", url.Values{
		"mqtt_enabled":  {"1"},
		"mqtt_host":     {"broker.neu"},
		"mqtt_port":     {"1883"},
		"mqtt_username": {"wer"},
		"mqtt_password": {"neu"},
	})

	if got := server.app.Config().MQTTPassword; got != "neu" {
		t.Fatalf("MQTTPassword = %q, want the newly entered one", got)
	}
}

// The data server's token is not a target that can move: that listener is on
// this machine, and the token is checked here rather than sent anywhere. Moving
// the bind address must not throw it away.
func TestMovingTheDataListenerKeepsItsToken(t *testing.T) {
	server, ts := newServer(t, func(cfg *config.Config) {
		cfg.DataServerEnabled = true
		cfg.DataPort = 9838
		cfg.DataToken = "geheim"
	})

	post(t, ts.URL, "/save/data", url.Values{
		"data_server_enabled": {"1"},
		"data_bind_address":   {"0.0.0.0"},
		"data_port":           {"9999"},
		"json_enabled":        {"1"},
	})

	if got := server.app.Config().DataToken; got != "geheim" {
		t.Errorf("DataToken = %q; the listener moved, but it does not travel", got)
	}
}
