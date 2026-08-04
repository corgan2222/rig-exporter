// Package dataserver is the pull-based export target: an HTTP listener that
// Home Assistant, Prometheus or Telegraf fetch the current reading from.
//
// It holds only the most recent snapshot. There is no history, because every
// consumer of this endpoint keeps its own.
package dataserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/export"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// Endpoint paths. They are stable, because they end up pasted into Home
// Assistant configuration and Prometheus scrape configs.
const (
	PathJSON       = "/api/state"
	PathPrometheus = "/metrics"
	PathInflux     = "/influx"
	PathHealth     = "/health"
)

const shutdownTimeout = 3 * time.Second

// Server is the HTTP export target.
type Server struct {
	cfg config.Config
	log *slog.Logger

	counter export.Counter

	mu       sync.RWMutex
	snapshot collector.Snapshot
	haveData bool
	address  string
	lastErr  string

	server *http.Server
	// now is injected so the InfluxDB timestamp is testable.
	now func() time.Time
}

// New builds the server. Nothing is listening until Start is called.
func New(cfg config.Config, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, log: log, now: time.Now}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathHealth, s.handleHealth)
	mux.HandleFunc("GET "+PathJSON, s.guard(cfg.JSONEnabled, s.handleJSON))
	mux.HandleFunc("GET "+PathPrometheus, s.guard(cfg.PrometheusEnabled, s.handlePrometheus))
	mux.HandleFunc("GET "+PathInflux, s.guard(cfg.InfluxPullEnabled, s.handleInflux))
	mux.HandleFunc("GET /", s.handleIndex)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Start binds the listener.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.cfg.DataAddress())
	if err != nil {
		s.setError(err.Error())
		return fmt.Errorf("listen on %s: %w", s.cfg.DataAddress(), err)
	}

	s.mu.Lock()
	s.address = listener.Addr().String()
	s.lastErr = ""
	s.mu.Unlock()

	s.log.Info("data server listening",
		"address", s.cfg.DataAddress(),
		"json", s.cfg.JSONEnabled,
		"prometheus", s.cfg.PrometheusEnabled,
		"influx", s.cfg.InfluxPullEnabled,
		"token", s.cfg.DataToken != "")

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.setError(err.Error())
			s.log.Error("data server stopped", "error", err)
		}
	}()
	return nil
}

// Export stores the snapshot for the next request to pick up.
func (s *Server) Export(snap collector.Snapshot) error {
	s.mu.Lock()
	s.snapshot = snap
	s.haveData = true
	s.mu.Unlock()

	s.counter.Inc()
	return nil
}

// Stop shuts the listener down.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = s.server.Shutdown(ctx)
	s.log.Info("data server stopped")
}

// Status reports where the server is reachable and which formats it serves.
func (s *Server) Status() export.Status {
	s.mu.RLock()
	address, lastErr := s.address, s.lastErr
	s.mu.RUnlock()

	lang := s.cfg.Lang()
	status := export.Status{
		Name:      "http",
		Label:     i18n.T(lang, "export.dataserver"),
		Healthy:   address != "" && lastErr == "",
		Failed:    lastErr != "",
		Delivered: s.counter.Count(),
	}
	switch {
	case lastErr != "":
		status.Detail = lastErr
	case address != "":
		status.Detail = "http://" + address + " · " + strings.Join(s.formats(), ", ")
	default:
		status.Detail = i18n.T(lang, "export.notStarted")
	}
	return status
}

func (s *Server) formats() []string {
	var formats []string
	if s.cfg.JSONEnabled {
		formats = append(formats, "JSON")
	}
	if s.cfg.PrometheusEnabled {
		formats = append(formats, "Prometheus")
	}
	if s.cfg.InfluxPullEnabled {
		formats = append(formats, "Influx")
	}
	return formats
}

func (s *Server) setError(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}

// guard wraps a handler with the enabled switch and the optional token check.
func (s *Server) guard(enabled bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			http.NotFound(w, r)
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+config.AppName+`"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// authorized checks the bearer token, if one is configured. The comparison is
// constant time so a wrong token cannot be found byte by byte.
func (s *Server) authorized(r *http.Request) bool {
	want := s.cfg.DataToken
	if want == "" {
		return true
	}

	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if got == "" {
		// Query parameter fallback: Prometheus and some Home Assistant setups
		// find it easier to put the token in the URL than in a header.
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// snapshotFor returns the latest reading, and whether one has been taken yet.
func (s *Server) snapshotFor() (collector.Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot, s.haveData
}

func (s *Server) handleJSON(w http.ResponseWriter, _ *http.Request) {
	snap, ok := s.snapshotFor()
	if !ok {
		http.Error(w, "no reading yet", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(snap.JSON()); err != nil {
		s.log.Debug("json encode failed", "error", err)
	}
}

func (s *Server) handlePrometheus(w http.ResponseWriter, _ *http.Request) {
	snap, ok := s.snapshotFor()
	if !ok {
		http.Error(w, "no reading yet", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", metrics.PrometheusContentType)
	w.Write(snap.Prometheus(s.cfg.NodeID))
}

func (s *Server) handleInflux(w http.ResponseWriter, _ *http.Request) {
	snap, ok := s.snapshotFor()
	if !ok {
		http.Error(w, "no reading yet", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", metrics.InfluxContentType)
	w.Write(snap.Influx(s.cfg.InfluxMeasurement, s.cfg.NodeID, s.now()))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	_, ok := s.snapshotFor()
	if !ok {
		http.Error(w, "starting", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}

// handleIndex lists the endpoints that are switched on, which saves guessing
// the paths when wiring up Home Assistant.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "rig-exporter %s — data endpoints for %s\n\n", config.Version, s.cfg.NodeID)
	if s.cfg.JSONEnabled {
		fmt.Fprintf(w, "%-12s JSON, for the Home Assistant RESTful sensor\n", PathJSON)
	}
	if s.cfg.PrometheusEnabled {
		fmt.Fprintf(w, "%-12s Prometheus text exposition\n", PathPrometheus)
	}
	if s.cfg.InfluxPullEnabled {
		fmt.Fprintf(w, "%-12s InfluxDB line protocol\n", PathInflux)
	}
	fmt.Fprintf(w, "%-12s liveness check, never requires a token\n", PathHealth)
	if s.cfg.DataToken != "" {
		fmt.Fprint(w, "\nA token is required: send Authorization: Bearer <token> or ?token=<token>.\n")
	}
}
