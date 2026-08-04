//go:build windows

// Package webui serves the local interface: a dashboard that shows what is
// being measured, and a settings page that changes it.
//
// The two are separate pages rather than one long scroll, because they are
// used at different times — the dashboard is glanced at, the settings are
// visited once and then left alone. It binds to loopback only.
package webui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/corgan/rig-exporter/internal/app"
	"github.com/corgan/rig-exporter/internal/assets"
	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/export"
	"github.com/corgan/rig-exporter/internal/export/dataserver"
	"github.com/corgan/rig-exporter/internal/hardware/pawnio"
	"github.com/corgan/rig-exporter/internal/i18n"
	"github.com/corgan/rig-exporter/internal/metrics"
	"github.com/corgan/rig-exporter/internal/winapi"
)

//go:embed templates/*.html
var templateFS embed.FS

const shutdownTimeout = 3 * time.Second

// Server is the local web interface.
type Server struct {
	app *app.App
	log *slog.Logger
	// Each page is its own template set: both define a "content" block, and
	// one set cannot hold two definitions of the same name.
	pages  map[string]*template.Template
	server *http.Server
	// url is the resolved address, which can differ from the configured port
	// if that port was taken.
	url string
}

// New parses the templates and prepares the HTTP handlers.
func New(application *app.App, log *slog.Logger) (*Server, error) {
	pages := map[string]*template.Template{}
	for _, page := range []string{"status", "capture", "export"} {
		tmpl, err := template.ParseFS(templateFS, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s page: %w", page, err)
		}
		pages[page] = tmpl
	}

	s := &Server{app: application, log: log, pages: pages}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleStatus)
	mux.HandleFunc("GET /capture", s.handleCapture)
	mux.HandleFunc("GET /export", s.handleExport)
	// One endpoint per block, so a page can never switch off a setting it does
	// not show: an unchecked box and an absent field look identical in a form
	// submission.
	mux.HandleFunc("POST /save/{block}", s.handleSave)
	mux.HandleFunc("POST /pause", s.handlePause)
	mux.HandleFunc("POST /language", s.handleLanguage)
	mux.HandleFunc("POST /open", s.handleOpen)
	mux.HandleFunc("GET /api/status", s.handleAPIStatus)
	// The same icon the tray shows, so a pinned tab is recognisable as this
	// program rather than as a blank page.
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)

	// The old single settings page, kept so a bookmark still lands somewhere.
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/export", http.StatusMovedPermanently)
	})

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s, nil
}

// Start listens on the configured loopback port. If that port is busy it
// falls back to an ephemeral one rather than leaving the user without an
// interface; URL reports where it actually ended up.
func (s *Server) Start() error {
	address := s.app.Config().WebAddress()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		s.log.Warn("web port unavailable, using a random one", "address", address, "error", err)
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("listen on loopback: %w", err)
		}
	}

	s.url = "http://" + listener.Addr().String()
	s.log.Info("web interface listening", "url", s.url)

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("web interface stopped", "error", err)
		}
	}()
	return nil
}

// URL is the address to open in a browser.
func (s *Server) URL() string { return s.url }

// Stop shuts the server down.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

// pageData is what the templates render. Both pages share it, so the layout
// can rely on every field being there.
type pageData struct {
	Lang      i18n.Lang
	Languages []i18n.Language
	// Active names the current page for the navigation highlight.
	Active   string
	TitleKey string

	AppName   string
	Version   string
	ConfigDir string
	Config    config.Config
	Status    app.Status

	Saved bool
	Error string

	RTSSDownloadURL string
	AfterburnerURL  string
	// PawnIOStatus says in one sentence what PawnIO can do here. It is built
	// fresh on every render: the user may install it, or restart elevated,
	// while the page is open, and a stale "not installed" would send them
	// chasing a problem they already fixed.
	PawnIOStatus string
	// Origins is what actually supplied the last reading, and what each gave.
	Origins []originRow
	// RefreshMs is how often the dashboard polls, derived from the configured
	// read interval so the page moves at the speed the user asked for.
	RefreshMs int
	Endpoints []endpoint

	MinIntervalMs int
	MaxIntervalMs int
	MinIdleMs     int
	MaxIdleMs     int

	// The Has* flags drive the "leave blank to keep" hints without ever
	// sending a secret to the browser.
	HasPassword    bool
	HasDataToken   bool
	HasInfluxToken bool
	DiskInclude    string
	EntityCount    int
	// RecorderYAML is the ready-to-paste Home Assistant recorder block for
	// exactly the entities this machine publishes.
	RecorderYAML string
	// The two sensor sets, for the box that says what each one contains.
	// ExtendedSet holds what the extended set adds, not the whole of it.
	StandardSet []setEntry
	ExtendedSet []setEntry
}

// T translates an interface string into the active language.
func (p pageData) T(key string) string { return i18n.T(p.Lang, key) }

// ExportStatus is the live state of one export target, or nil when that target
// is switched off — Status.Exports holds an entry per enabled target only.
//
// A pointer rather than the two-value lookup because a template cannot take a
// second return value, and `{{ with }}` on nil is exactly the "not enabled, say
// nothing" case.
func (p pageData) ExportStatus(name string) *export.Status {
	if status, ok := p.Status.Export(name); ok {
		return &status
	}
	return nil
}

// H is T for catalogue entries that contain markup, such as an inline <code>.
// Only the catalogue feeds it, never user input.
func (p pageData) H(key string) template.HTML {
	return template.HTML(i18n.T(p.Lang, key)) //nolint:gosec // catalogue text, not user input
}

// endpoint is one row of the data server's URL list.
type endpoint struct {
	Label string
	Path  string
	URL   string
}

// setEntry is one measurement in the sensor-set listing: the identifier a
// dashboard keys on, and the name a person reads.
type setEntry struct {
	ID    string
	Label string
}

func setEntries(defs []metrics.Definition, lang i18n.Lang) []setEntry {
	out := make([]setEntry, 0, len(defs))
	for _, d := range defs {
		out = append(out, setEntry{ID: d.ID, Label: d.Name.In(lang)})
	}
	return out
}

// worthKeeping names the measurements whose history earns the room it takes in
// the Home Assistant database. Everything else this program publishes is
// proposed for exclusion in the recorder snippet.
//
// The criterion is whether an hourly average over months says anything. A
// temperature, a load, a free-space figure and a latency do; a momentary clock
// rate, a fan speed in the middle of a game and a throughput spike do not — for
// those the ten-day detail Home Assistant keeps anyway is the useful window.
// The longer reasoning, with measurements, is in maybe_later.md.
var worthKeeping = []string{
	metrics.FPS.ID,
	metrics.CPULoad.ID,
	metrics.RAMLoad.ID,
	metrics.CPUTemperature.ID,
	metrics.GPUTemperature.ID,
	metrics.GPULoad.ID,
	metrics.DiskFreePercent.ID,
	metrics.PingRTT.ID,
}

// recorderSnippet builds the recorder block for this machine.
//
// It names the entities that actually exist rather than the ones a generic
// example would guess at: two graphics cards give two temperature lines, a
// switched-off sensor group gives none. A snippet somebody has to correct by
// hand before pasting is worse than no snippet.
func recorderSnippet(cfg config.Config, snap collector.Snapshot) string {
	var keep []string
	for _, r := range snap.Entities() {
		if slices.Contains(worthKeeping, r.Def.ID) {
			keep = append(keep, r.Def.Component()+"."+cfg.ObjectID(r.Key()))
		}
	}

	var b strings.Builder
	b.WriteString("recorder:\n")
	b.WriteString("  purge_keep_days: 10\n")
	b.WriteString("  exclude:\n    entity_globs:\n")
	fmt.Fprintf(&b, "      - sensor.%s*\n", cfg.ObjectPrefix())
	fmt.Fprintf(&b, "      - binary_sensor.%s*\n", cfg.ObjectPrefix())
	if len(keep) > 0 {
		b.WriteString("  include:\n    entities:\n")
		for _, id := range keep {
			fmt.Fprintf(&b, "      - %s\n", id)
		}
	}
	return b.String()
}

func (s *Server) newPageData(active, titleKey string) pageData {
	status := s.app.Status()
	cfg := status.Config
	lang := cfg.Lang()

	return pageData{
		Lang:            lang,
		Languages:       i18n.Available,
		Active:          active,
		TitleKey:        titleKey,
		AppName:         config.AppName,
		Version:         config.VersionString(),
		ConfigDir:       configDir(),
		Config:          cfg,
		Status:          status,
		RTSSDownloadURL: config.RTSSDownloadURL,
		AfterburnerURL:  config.AfterburnerURL,
		PawnIOStatus:    pawnIOStatus(lang),
		Origins:         originsFor(status.Snapshot, lang),
		RefreshMs:       cfg.PollIntervalMs,
		Endpoints:       endpointsFor(cfg, lang),
		MinIntervalMs:   config.MinIntervalMs,
		MaxIntervalMs:   config.MaxIntervalMs,
		MinIdleMs:       config.MinIdleMs,
		MaxIdleMs:       config.MaxIdleMs,
		HasPassword:     cfg.MQTTPassword != "",
		HasDataToken:    cfg.DataToken != "",
		HasInfluxToken:  cfg.InfluxToken != "",
		DiskInclude:     strings.Join(cfg.DiskInclude, ", "),
		EntityCount:     len(status.Snapshot.Entities()),
		RecorderYAML:    recorderSnippet(cfg, status.Snapshot),
		StandardSet:     setEntries(metrics.StandardDefinitions(), lang),
		ExtendedSet:     setEntries(metrics.ExtendedDefinitions(), lang),
	}
}

func (s *Server) render(w http.ResponseWriter, page string, data pageData) {
	tmpl, ok := s.pages[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		s.log.Error("render page", "page", page, "error", err)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.render(w, "status", s.newPageData("status", "page.status"))
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "capture", "page.capture")
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "export", "page.export")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, page, titleKey string) {
	data := s.newPageData(page, titleKey)
	data.Saved = r.URL.Query().Get("saved") == "1"
	data.Error = r.URL.Query().Get("error")
	s.render(w, page, data)
}

// configDir is where the interface tells the user their files live.
func configDir() string {
	dir, err := config.Dir()
	if err != nil {
		return `%APPDATA%\` + config.AppName
	}
	return dir
}

// endpointsFor lists the data-server URLs that are switched on.
//
// The host part is the machine's own address rather than its name: these URLs
// are meant to be pasted into a Home Assistant or Prometheus configuration on
// another machine, and an address works there even when name resolution on the
// local network does not.
func endpointsFor(cfg config.Config, lang i18n.Lang) []endpoint {
	if !cfg.DataServerEnabled {
		return nil
	}

	host := cfg.DataBindAddress
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = localAddress(cfg.DeviceName)
	}
	base := fmt.Sprintf("http://%s:%d", host, cfg.DataPort)

	var out []endpoint
	if cfg.JSONEnabled {
		out = append(out, endpoint{i18n.T(lang, "endpoints.json"), dataserver.PathJSON, base + dataserver.PathJSON})
	}
	if cfg.PrometheusEnabled {
		out = append(out, endpoint{i18n.T(lang, "endpoints.prometheus"), dataserver.PathPrometheus, base + dataserver.PathPrometheus})
	}
	if cfg.InfluxPullEnabled {
		out = append(out, endpoint{i18n.T(lang, "endpoints.influx"), dataserver.PathInflux, base + dataserver.PathInflux})
	}
	return out
}

// blockPages maps each settings block onto the page it lives on, which is
// where a save returns to.
var blockPages = map[string]string{
	"sensors": "/capture",
	"capture": "/capture",
	"mqtt":    "/export",
	"ha":      "/export",
	"data":    "/export",
	"influx":  "/export",
	"app":     "/export",
}

// handleSave applies one block of settings.
//
// Only the fields of that block are read: a form carries no evidence of the
// checkboxes it does not contain, so applying the whole configuration from a
// partial form would silently switch off everything on the other page.
// localAddress is the machine's own IPv4 address on the interface that
// carries the default route.
//
// The connection is never established — a UDP socket only picks a route — but
// choosing the route is exactly what reveals which address another machine
// would see. Falls back to the given name if there is no route at all.
func localAddress(fallback string) string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err != nil {
		return fallback
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return fallback
	}
	return addr.IP.String()
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	block := r.PathValue("block")
	page, known := blockPages[block]
	if !known {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	cfg := s.app.Config()
	switch block {
	case "mqtt":
		cfg.MQTTEnabled = r.FormValue("mqtt_enabled") != ""
		cfg.MQTTHost = strings.TrimSpace(r.FormValue("mqtt_host"))
		cfg.MQTTPort = formInt(r, "mqtt_port", cfg.MQTTPort)
		cfg.MQTTUsername = strings.TrimSpace(r.FormValue("mqtt_username"))
		cfg.MQTTTLS = r.FormValue("mqtt_tls") != ""
		cfg.MQTTTLSInsecure = r.FormValue("mqtt_tls_insecure") != ""
		cfg.ClientID = strings.TrimSpace(r.FormValue("client_id"))
		// An empty password field means "keep what is stored", so the page
		// never has to round-trip the secret.
		cfg.MQTTPassword = updateSecret(r, "mqtt_password", "clear_password", cfg.MQTTPassword)

	case "ha":
		cfg.DeviceName = strings.TrimSpace(r.FormValue("device_name"))
		cfg.NodeID = strings.TrimSpace(r.FormValue("node_id"))
		cfg.TopicPrefix = strings.TrimSpace(r.FormValue("topic_prefix"))
		cfg.DiscoveryPrefix = strings.TrimSpace(r.FormValue("discovery_prefix"))

	case "data":
		cfg.DataServerEnabled = r.FormValue("data_server_enabled") != ""
		cfg.DataBindAddress = strings.TrimSpace(r.FormValue("data_bind_address"))
		cfg.DataPort = formInt(r, "data_port", cfg.DataPort)
		cfg.JSONEnabled = r.FormValue("json_enabled") != ""
		cfg.PrometheusEnabled = r.FormValue("prometheus_enabled") != ""
		cfg.InfluxPullEnabled = r.FormValue("influx_pull_enabled") != ""
		cfg.DataToken = updateSecret(r, "data_token", "clear_data_token", cfg.DataToken)

	case "influx":
		cfg.InfluxPushEnabled = r.FormValue("influx_push_enabled") != ""
		cfg.InfluxURL = strings.TrimSpace(r.FormValue("influx_url"))
		cfg.InfluxOrg = strings.TrimSpace(r.FormValue("influx_org"))
		cfg.InfluxBucket = strings.TrimSpace(r.FormValue("influx_bucket"))
		cfg.InfluxMeasurement = strings.TrimSpace(r.FormValue("influx_measurement"))
		cfg.InfluxToken = updateSecret(r, "influx_token", "clear_influx_token", cfg.InfluxToken)

	case "sensors":
		cfg.SensorSet = r.FormValue("sensor_set")
		cfg.GPUEnabled = r.FormValue("gpu_enabled") != ""
		cfg.CPUDetailEnabled = r.FormValue("cpu_detail_enabled") != ""
		cfg.CPUPerCore = r.FormValue("cpu_per_core") != ""
		cfg.RAMDetailEnabled = r.FormValue("ram_detail_enabled") != ""
		cfg.PawnIOEnabled = r.FormValue("pawnio_enabled") != ""
		cfg.DiskEnabled = r.FormValue("disk_enabled") != ""
		cfg.DiskInclude = splitList(r.FormValue("disk_include"))
		cfg.NetEnabled = r.FormValue("net_enabled") != ""
		cfg.NetAllAdapters = r.FormValue("net_all_adapters") != ""
		cfg.PingEnabled = r.FormValue("ping_enabled") != ""
		cfg.PingTarget = strings.TrimSpace(r.FormValue("ping_target"))
		cfg.PingCount = formInt(r, "ping_count", cfg.PingCount)
		cfg.PingIntervalMs = formInt(r, "ping_interval_ms", cfg.PingIntervalMs)

	case "capture":
		cfg.PollIntervalMs = formInt(r, "poll_interval_ms", cfg.PollIntervalMs)
		cfg.PublishIntervalMs = formInt(r, "interval_ms", cfg.PublishIntervalMs)
		cfg.IdlePublishIntervalMs = formInt(r, "idle_interval_ms", cfg.IdlePublishIntervalMs)
		cfg.IdleTimeoutMs = formInt(r, "idle_timeout_ms", cfg.IdleTimeoutMs)
		cfg.Decimals = r.FormValue("decimals") != ""

	case "app":
		cfg.Language = r.FormValue("language")
		cfg.WebPort = formInt(r, "web_port", cfg.WebPort)
		cfg.Autostart = r.FormValue("autostart") != ""
		cfg.Debug = r.FormValue("debug") != ""
	}

	if err := s.app.ApplyConfig(cfg); err != nil {
		s.log.Error("apply config", "block", block, "error", err)
		http.Redirect(w, r, page+"?error="+neturl.QueryEscape(err.Error())+"#"+block, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, page+"?saved=1#"+block, http.StatusSeeOther)
}

// openTargets maps the footer buttons onto what they open.
var openTargets = map[string]func() (string, error){
	"config": config.Path,
	"log":    config.LogPath,
	"folder": config.Dir,
}

// handleOpen hands a configuration file to the shell.
//
// A browser refuses to follow a file:// link from an http page, so the server
// opens it instead — it runs on the same machine as the browser, which is the
// whole premise of a loopback-only interface.
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	resolve, known := openTargets[r.FormValue("what")]
	if !known {
		http.NotFound(w, r)
		return
	}

	path, err := resolve()
	if err == nil {
		err = winapi.OpenURL(path)
	}
	if err != nil {
		s.log.Error("open failed", "what", r.FormValue("what"), "error", err)
	}
	http.Redirect(w, r, backTo(r), http.StatusSeeOther)
}

// updateSecret applies the "blank keeps, checkbox clears" rule to one field.
func updateSecret(r *http.Request, field, clearField, current string) string {
	switch {
	case r.FormValue(clearField) != "":
		return ""
	case r.FormValue(field) != "":
		return strings.TrimSpace(r.FormValue(field))
	default:
		return current
	}
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	s.app.SetPaused(!s.app.Paused())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLanguage switches the language without touching anything else, so the
// switcher works from any page and returns to it.
func (s *Server) handleLanguage(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config()
	cfg.Language = string(i18n.Parse(r.FormValue("lang")))

	if err := s.app.ApplyConfig(cfg); err != nil {
		s.log.Error("apply language", "error", err)
	}
	http.Redirect(w, r, backTo(r), http.StatusSeeOther)
}

// backTo returns the page the request came from, restricted to this server's
// own paths so the header cannot be used to bounce a browser elsewhere.
func backTo(r *http.Request) string {
	referer, err := neturl.Parse(r.Referer())
	if err != nil || referer.Path == "" || !strings.HasPrefix(referer.Path, "/") {
		return "/"
	}
	return referer.Path
}

// statusResponse is the JSON the dashboard polls for live values.
type statusResponse struct {
	FPS         float64 `json:"fps"`
	Frametime   float64 `json:"frametime"`
	Game        string  `json:"game"`
	Resolution  string  `json:"resolution"`
	RefreshRate int     `json:"refresh_rate"`
	CPU         float64 `json:"cpu"`
	RAM         float64 `json:"ram"`
	RAMUsedMB   uint64  `json:"ram_used_mb"`
	RAMTotalMB  uint64  `json:"ram_total_mb"`

	RTSSStatus  string `json:"rtss_status"`
	RTSSMessage string `json:"rtss_message"`
	RTSSVersion string `json:"rtss_version"`

	// Groups carries the optional sensor groups, so the page can show GPU,
	// disk and network readings without the server knowing what they are.
	Groups  []groupStatus  `json:"groups"`
	Exports []exportStatus `json:"exports"`

	Paused    bool   `json:"paused"`
	UpdatedAt string `json:"updated_at"`

	// What the exporter is currently doing, for the chips under the tiles.
	// They come through the API rather than the template because the page is
	// not reloaded when a setting is saved from the other tab.
	SensorSet   string `json:"sensor_set"`
	Decimals    bool   `json:"decimals"`
	EntityCount int    `json:"entity_count"`
	// PublishMs is the pace in force right now, and Rendering says which of
	// the two it is — showing a number without saying which one would be
	// worse than showing nothing.
	PublishMs int  `json:"publish_ms"`
	Rendering bool `json:"rendering"`
}

// groupStatus is one optional sensor group as the page renders it.
type groupStatus struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Error     string `json:"error"`
	Rows      []row  `json:"rows"`
}

// row is one reading, already formatted for display.
//
// The label comes in two forms so the page can list readings either way: by
// measurement, where the instance has to be part of the label, or by device,
// where it is the heading and would only repeat.
type row struct {
	Label string `json:"label"`
	Short string `json:"short"`
	// Instance names the device this reading belongs to, empty for the ones
	// that exist only once.
	Instance string `json:"instance"`
	// Device is the heading to show above this reading when the page groups
	// by device: the instance on its own, or something more telling when the
	// group offers it.
	Device string `json:"device"`
	Value  string `json:"value"`
}

// deviceNames names the reading that identifies a device within its group.
// A bare "0" says much less above a block of readings than the card's model
// does; a drive letter or an adapter name already stands on its own.
var deviceNames = map[metrics.Group]string{
	metrics.GroupGPU: metrics.GPUName.ID,
}

// exportStatus is one target's state, rendered as a badge on the page.
type exportStatus struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Healthy   bool   `json:"healthy"`
	Detail    string `json:"detail"`
	Delivered uint64 `json:"delivered"`
}

// handleFavicon serves the tray icon. It is the same multi-resolution ICO the
// notification area uses, so the browser picks whichever size it wants and the
// tab matches the tray.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// A zero time leaves out Last-Modified, which is right: the icon is
	// compiled in and only ever changes with the program itself.
	http.ServeContent(w, r, "favicon.ico", time.Time{}, bytes.NewReader(assets.Icon))
}

func (s *Server) handleAPIStatus(w http.ResponseWriter, _ *http.Request) {
	st := s.app.Status()
	snap := st.Snapshot
	lang := st.Config.Lang()

	resp := statusResponse{
		FPS:         snap.FPS(),
		Frametime:   snap.FrametimeMs(),
		Game:        snap.Game(),
		Resolution:  snap.Resolution(),
		RefreshRate: snap.RefreshHz(),
		CPU:         snap.CPUPercent(),
		RAM:         snap.RAMPercent(),
		RAMUsedMB:   uint64(snap.Number(metrics.RAMUsed.ID)),
		RAMTotalMB:  uint64(snap.Number(metrics.RAMTotal.ID)),
		RTSSStatus:  string(snap.RTSSStatus),
		RTSSMessage: snap.RTSSMessage,
		RTSSVersion: snap.RTSSVersion,
		Groups:      groupStatuses(st, lang),
		Exports:     make([]exportStatus, 0, len(st.Exports)),
		Paused:      st.Paused,
		SensorSet:   st.Config.SensorSet,
		Decimals:    st.Config.Decimals,
		EntityCount: len(snap.Entities()),
		PublishMs:   publishPace(st),
		Rendering:   snap.Rendering(),
	}
	for _, e := range st.Exports {
		resp.Exports = append(resp.Exports, exportStatus{
			Name:      e.Name,
			Label:     e.Label,
			Healthy:   e.Healthy,
			Detail:    e.Detail,
			Delivered: e.Delivered,
		})
	}
	if !st.UpdatedAt.IsZero() {
		resp.UpdatedAt = st.UpdatedAt.Format(time.TimeOnly)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Debug("status encode failed", "error", err)
	}
}

// publishPace is the export interval in force at this moment: the game rate
// while something is rendering, the idle rate otherwise. Reading it off the
// snapshot rather than off a flag keeps the chip honest — it says what is
// happening, not what was configured for a case that does not apply.
func publishPace(st app.Status) int {
	if st.Snapshot.Rendering() {
		return st.Config.PublishIntervalMs
	}
	return st.Config.IdlePublishIntervalMs
}

// groupStatuses turns the optional sensor groups into display rows.
//
// A group that is switched on but produced nothing is reported as unavailable
// with the source's own explanation, which is what tells the user that
// Afterburner is not running rather than leaving an empty panel.
func groupStatuses(st app.Status, lang i18n.Lang) []groupStatus {
	snap := st.Snapshot
	enabled := map[metrics.Group]bool{
		metrics.GroupGPU:  st.Config.GPUEnabled,
		metrics.GroupCPU:  st.Config.CPUDetailEnabled,
		metrics.GroupRAM:  st.Config.RAMDetailEnabled,
		metrics.GroupDisk: st.Config.DiskEnabled,
		metrics.GroupNet:  st.Config.NetEnabled,
	}

	out := make([]groupStatus, 0, len(enabled))
	for _, group := range metrics.Groups {
		if group == metrics.GroupCore {
			continue // the core values have their own tiles
		}

		status := groupStatus{
			Key:       string(group),
			Label:     group.Label(lang),
			Enabled:   enabled[group],
			Available: snap.HasGroup(group),
			Error:     snap.SourceErrors[group],
		}
		if status.Enabled && status.Available {
			status.Rows = rowsFor(snap, group, lang)
		}
		out = append(out, status)
	}
	return out
}

// originRow is one supplier and what it currently provides.
type originRow struct {
	Name string
	// Count is how many readings came from here, which is what separates a
	// supplier carrying the machine from one contributing a single value.
	Count int
	// Values names the distinct measurements, so the answer to "what do I lose
	// if I close this program" is on the page rather than in the documentation.
	Values string
}

// originsFor lists what supplied the last reading.
//
// Built from the readings themselves rather than from a table of what each
// source is supposed to provide. A table would describe the intended design;
// this describes the machine in front of the user, including the case where a
// program is installed but quietly contributing nothing.
func originsFor(snap collector.Snapshot, lang i18n.Lang) []originRow {
	summaries := snap.Origins()

	rows := make([]originRow, 0, len(summaries))
	for _, origin := range summaries {
		names := origin.Names(lang)
		rows = append(rows, originRow{
			Name:   origin.Name,
			Count:  len(origin.Readings),
			Values: strings.Join(names, ", "),
		})
	}
	return rows
}

// pawnIOStatus says what PawnIO can do on this machine right now.
//
// Four states, four different things for the reader to do, and telling them
// apart is the entire point: "install it" and "restart as administrator" are
// not interchangeable advice, and offering either to someone who already has a
// working setup is worse than saying nothing.
func pawnIOStatus(lang i18n.Lang) string {
	state := pawnio.Detect()

	switch state.Availability {
	case pawnio.Ready:
		return fmt.Sprintf(i18n.T(lang, "settings.sensors.pawnioReady"), state.Version)
	case pawnio.NeedsElevation:
		return fmt.Sprintf(i18n.T(lang, "settings.sensors.pawnioNeedsAdmin"), state.Version)
	case pawnio.DriverUnavailable:
		return fmt.Sprintf(i18n.T(lang, "settings.sensors.pawnioBroken"), state.Version)
	default:
		return i18n.T(lang, "settings.sensors.pawnioMissing")
	}
}

// rowsFor collects one group's readings, ordered so that grouping them by
// device on the page needs no re-sorting: every instance's readings are
// already adjacent.
func rowsFor(snap collector.Snapshot, group metrics.Group, lang i18n.Lang) []row {
	nameID := deviceNames[group]
	names := map[string]string{}

	var rows []row
	for _, reading := range snap.Entities() {
		if reading.Def.PanelGroup() != group {
			continue
		}
		if nameID != "" && reading.Def.ID == nameID && reading.Text != "" {
			names[reading.Instance] = reading.Text
		}
		rows = append(rows, row{
			Label:    reading.DisplayName(lang),
			Short:    reading.Def.Name.In(lang),
			Instance: reading.Instance,
			Value:    formatValue(reading),
		})
	}

	for i, r := range rows {
		rows[i].Device = r.Instance
		if name, ok := names[r.Instance]; ok {
			rows[i].Device = r.Instance + " · " + name
		}
	}

	// Ordered so that grouping by device on the page needs no re-sorting:
	// every instance's readings are already adjacent, and numeric instances
	// count rather than sort as text, so core 10 comes after core 9.
	sort.SliceStable(rows, func(i, j int) bool {
		return metrics.LessInstance(rows[i].Instance, rows[j].Instance)
	})
	return rows
}

func formatValue(r metrics.Reading) string {
	switch r.Def.Kind {
	case metrics.KindText:
		return r.Text
	case metrics.KindBool:
		if r.Bool {
			return "1"
		}
		return "0"
	default:
		value := strconv.FormatFloat(r.Number, 'f', r.Def.EffectivePrecision(), 64)
		if r.Def.Unit == "" {
			return value
		}
		return value + " " + r.Def.Unit
	}
}

// splitList turns a comma or space separated field into a list, dropping the
// empty entries a trailing separator leaves behind.
func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func formInt(r *http.Request, field string, fallback int) int {
	value := strings.TrimSpace(r.FormValue(field))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}
