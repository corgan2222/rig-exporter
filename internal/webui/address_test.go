//go:build windows

package webui

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/config"
)

// The link that reaches Home Assistant and the tray has to be typeable.
//
// Two things it must not take from the configuration: the port, because a busy
// one sends the server to an ephemeral one, and the host when the server listens
// on every interface, because 0.0.0.0 is an instruction to the kernel rather
// than an address anybody can open.
func TestTheReachableURLTakesThePortFromTheListener(t *testing.T) {
	cfg := config.Defaults()
	cfg.WebPort = 8787

	bound := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8787}
	if got := reachableURL(cfg, bound); got != "http://127.0.0.1:8787" {
		t.Errorf("URL = %q, want http://127.0.0.1:8787", got)
	}

	// The fallback case: configured 8787, actually listening on 48352.
	fellBack := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 48352}
	if got := reachableURL(cfg, fellBack); got != "http://127.0.0.1:48352" {
		t.Errorf("URL = %q, want the port the listener got", got)
	}
}

func TestTheReachableURLNeverPointsAtTheWildcardAddress(t *testing.T) {
	cfg := config.Defaults()
	cfg.WebPort = 8787
	cfg.WebBindAll = true

	bound := &net.TCPAddr{IP: net.IPv4zero, Port: 8787}
	got := reachableURL(cfg, bound)

	if strings.Contains(got, "0.0.0.0") {
		t.Errorf("URL = %q, which is what the listener bound, not an address", got)
	}
	if !strings.HasSuffix(got, ":8787") {
		t.Errorf("URL = %q, want it to keep the port", got)
	}
	// Without a default route localAddress falls back to loopback, which is
	// still a usable link — so only the wildcard is a failure.
	if !strings.HasPrefix(got, "http://") {
		t.Errorf("URL = %q, want an http URL", got)
	}
}

// The setting has to reach the socket, not only the string that describes it.
//
// Port 0 rather than the configured one: this test says which interfaces the
// server binds, and taking a fixed port would make it fail whenever something
// else on the machine holds it.
func TestTheSettingReachesTheSocket(t *testing.T) {
	for _, tc := range []struct {
		bindAll bool
		want    string
	}{
		{bindAll: false, want: "127.0.0.1"},
		{bindAll: true, want: "0.0.0.0"},
	} {
		server, _ := newServer(t, func(c *config.Config) {
			c.WebBindAll = tc.bindAll
			c.WebPort = 0
		})
		if err := server.Start(); err != nil {
			t.Fatalf("Start(bindAll=%v): %v", tc.bindAll, err)
		}
		t.Cleanup(server.Stop)

		// Reached through the URL the server itself reports, so this covers the
		// link as well as the binding.
		resp, err := http.Get(server.URL() + "/api/status")
		if err != nil {
			t.Fatalf("get %s: %v", server.URL(), err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if strings.Contains(server.URL(), "0.0.0.0") {
			t.Errorf("URL = %q, which is not something to open", server.URL())
		}
	}
}

// Loopback stays the default. Opening the page to the network is a decision
// about trusting a network, and it must never happen because of a default.
func TestTheInterfaceBindsToLoopbackUnlessAsked(t *testing.T) {
	cfg := config.Defaults()
	if cfg.WebBindAll {
		t.Error("the interface is open to the network out of the box")
	}
	if got := cfg.WebAddress(); !strings.HasPrefix(got, "127.0.0.1:") {
		t.Errorf("listen address = %q, want loopback", got)
	}

	cfg.WebBindAll = true
	if got := cfg.WebAddress(); !strings.HasPrefix(got, "0.0.0.0:") {
		t.Errorf("listen address = %q, want every interface", got)
	}
	// WebURL is what a local browser opens and stays loopback either way.
	if got := cfg.WebURL(); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Errorf("WebURL = %q, want loopback", got)
	}
}
