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

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/assets"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/export"
	"github.com/corgan2222/rig-exporter/internal/export/dataserver"
	"github.com/corgan2222/rig-exporter/internal/hardware/gpu"
	"github.com/corgan2222/rig-exporter/internal/hardware/pawnio"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
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
	// rows holds each panel's rows for a few polls after their reading stops
	// arriving, so an intermittent counter does not resize the panel under the
	// reader. Display only — see linger.go.
	rows *lingerStore
}

// New parses the templates and prepares the HTTP handlers.
func New(application *app.App, log *slog.Logger) (*Server, error) {
	pages := map[string]*template.Template{}
	for _, page := range []string{"status", "capture", "measurements", "export"} {
		tmpl, err := template.ParseFS(templateFS, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s page: %w", page, err)
		}
		pages[page] = tmpl
	}

	s := &Server{app: application, log: log, pages: pages, rows: newLingerStore()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleStatus)
	mux.HandleFunc("GET /capture", s.handleCapture)
	mux.HandleFunc("GET /measurements", s.handleMeasurements)
	mux.HandleFunc("GET /export", s.handleExport)
	// One endpoint per block, so a page can never switch off a setting it does
	// not show: an unchecked box and an absent field look identical in a form
	// submission.
	mux.HandleFunc("POST /save/{block}", s.handleSave)
	mux.HandleFunc("POST /pause", s.handlePause)
	mux.HandleFunc("POST /language", s.handleLanguage)
	mux.HandleFunc("POST /open", s.handleOpen)
	mux.HandleFunc("POST /dismiss", s.handleDismiss)
	mux.HandleFunc("POST /update", s.handleUpdate)
	// Moving the slider is its own thing: it takes a rung and forgets the
	// exceptions, which the form full of ticks cannot express.
	mux.HandleFunc("POST /rung", s.handleRung)
	mux.HandleFunc("GET /api/status", s.handleAPIStatus)
	// The full crash record, as it was written. Linked from the banner rather
	// than shown in it: a goroutine dump is pages long.
	mux.HandleFunc("GET /crash", s.handleCrash)
	// The same record as a file to keep. A report that has to be reached by
	// opening a folder is a report that gets described from memory instead.
	mux.HandleFunc("GET /crash/download", s.handleCrashDownload)
	// One log file, by the name the page listed. Only a name that came out of
	// that listing is served — see handleLog.
	mux.HandleFunc("GET /logs/{name}", s.handleLog)
	mux.HandleFunc("GET /logs/{name}/download", s.handleLogDownload)
	// Tidying up: removes the kept records, never the one being written.
	mux.HandleFunc("POST /logs/clear", s.handleClearLogs)
	// One kept crash report, turned into a prepared GitHub form. A crash from
	// last week is still worth reporting, and its banner is long gone.
	mux.HandleFunc("GET /logs/{name}/issue", s.handleLogIssue)
	// The same icon the tray shows, so a pinned tab is recognisable as this
	// program rather than as a blank page.
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	// The same mark as a PNG: the page header draws it, and Home Assistant
	// points its update card at it.
	mux.HandleFunc("GET /icon.png", s.handleIconPNG)

	// The old single settings page, kept so a bookmark still lands somewhere.
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/export", http.StatusMovedPermanently)
	})

	// All four deadlines, not just the header one. Go falls IdleTimeout back to
	// ReadTimeout, and with both unset no idle deadline is ever applied: a
	// keep-alive connection that has finished one request is then held for the
	// lifetime of the process, goroutine and socket included. ReadHeaderTimeout
	// alone also leaves a request body free to trickle forever.
	//
	// Nothing this interface serves is long-lived — the largest response is a
	// rotated log of a couple of megabytes — so the numbers can be short. They
	// matter because the settings page offers web_bind_all, which moves this
	// listener off loopback and onto every interface.
	s.server = &http.Server{
		// Wrapped, not registered per route: a route added later would
		// otherwise be a hole nobody notices, and this one is easy to forget
		// because the missing check has no symptom until somebody uses it.
		Handler:           sameSite(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s, nil
}

// Start listens on the configured port. If that port is busy it falls back to
// an ephemeral one rather than leaving the user without an interface; URL
// reports where it actually ended up.
func (s *Server) Start() error {
	cfg := s.app.Config()
	address := cfg.WebAddress()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		s.log.Warn("web port unavailable, using a random one", "address", address, "error", err)
		fallback := "127.0.0.1:0"
		if cfg.WebBindAll {
			fallback = "0.0.0.0:0"
		}
		listener, err = net.Listen("tcp", fallback)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", fallback, err)
		}
	}

	s.url = reachableURL(cfg, listener.Addr())
	s.log.Info("web interface listening",
		"url", s.url, "bound", listener.Addr().String(), "network", cfg.WebBindAll)

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("web interface stopped", "error", err)
		}
	}()
	return nil
}

// URL is the address to open in a browser, and the one the Home Assistant
// device page links to.
func (s *Server) URL() string { return s.url }

// reachableURL turns what the listener bound onto something somebody can type.
//
// The port has to come from the listener rather than from the configuration,
// because a busy port sends the server to an ephemeral one. The host cannot:
// bound to every interface the listener says 0.0.0.0, which is an instruction
// to the kernel and not an address. Then the machine's own address on the
// default route is what another machine would use — and it is also what makes
// the "Visit" link on the device page work from Home Assistant.
func reachableURL(cfg config.Config, bound net.Addr) string {
	host := "127.0.0.1"
	if cfg.WebBindAll {
		host = localAddress(host)
	}

	port := cfg.WebPort
	if tcp, ok := bound.(*net.TCPAddr); ok {
		port = tcp.Port
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

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
	AMDDriverURL    string
	PawnIOURL       string
	CoolingURL      string
	// CrashIssueURL is a prepared GitHub issue for the pending crash record,
	// empty when there is nothing to report or the offer is switched off.
	CrashIssueURL string
	// Logs is every record this program keeps, and LogTail the last lines of
	// the running one — enough to see what happened without opening a file.
	Logs     []logFile
	LogLines []logLine
	// Where the name in the header and the credit in the footer point.
	ProjectURL string
	AuthorURL  string
	AuthorName string
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
	// RecorderYAML is the ready-to-paste Home Assistant recorder block for
	// exactly the entities this machine publishes.
	RecorderYAML string
	// Measurements is the tree, the slider and the estimate.
	Measurements measurementsData
}

// T translates an interface string into the active language.
func (p pageData) T(key string) string { return i18n.T(p.Lang, key) }

// LogTailNote says how much of the running log is on the page.
func (p pageData) LogTailNote() string {
	return strings.Replace(p.T("settings.logs.lastLines"), "%1", strconv.Itoa(tailLines), 1)
}

// HasCrashLogs reports whether any session ever failed to shut down. Used to
// say so plainly rather than leaving an absence to be interpreted.
func (p pageData) HasCrashLogs() bool {
	for _, file := range p.Logs {
		if file.Kind == crashKind {
			return true
		}
	}
	return false
}

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

// worthKeeping names the measurements whose history earns the room it takes in
// the Home Assistant database. Everything else this program publishes is
// proposed for exclusion in the recorder snippet.
//
// The criterion is whether an hourly average over months says anything. A
// temperature, a load, a free-space figure and a latency do; a momentary clock
// rate, a fan speed in the middle of a game and a throughput spike do not — for
// those the ten-day detail Home Assistant keeps anyway is the useful window.
// The longer reasoning, with measurements, is in maybe_later.md.
// The two rankings are here for a different reason from the rest. An hourly
// average of a program name says nothing, and Home Assistant builds no
// statistics from attributes at all — so their history is the only history they
// will ever have. Excluding them would leave the charts they exist for with
// nothing to draw.
var worthKeeping = []string{
	metrics.FPS.ID,
	metrics.CPULoad.ID,
	metrics.RAMLoad.ID,
	metrics.CPUTemperature.ID,
	metrics.GPUTemperature.ID,
	metrics.GPULoad.ID,
	metrics.DiskFreePercent.ID,
	metrics.PingRTT.ID,
	metrics.TopCPU.ID,
	metrics.TopMemory.ID,
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
		Lang:      lang,
		Languages: i18n.Available,
		Active:    active,
		TitleKey:  titleKey,
		AppName:   config.AppName,
		// The release only. The build identifier is still on the version
		// entity and in the log, where somebody chasing a specific commit
		// looks; in the header it was six characters of hash on every page,
		// answering a question nobody was asking.
		Version:         config.Version,
		ConfigDir:       configDir(),
		Config:          cfg,
		Status:          status,
		RTSSDownloadURL: config.RTSSDownloadURL,
		AfterburnerURL:  config.AfterburnerURL,
		AMDDriverURL:    config.AMDDriverURL,
		PawnIOURL:       config.PawnIOURL,
		CoolingURL:      config.CoolingURL,
		CrashIssueURL:   crashIssueURL(status),
		Logs:            logFiles(),
		LogLines:        logLinesOf(runningLogTail()),
		ProjectURL:      config.ProjectURL,
		AuthorURL:       config.AuthorURL,
		AuthorName:      config.AuthorName,
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
		RecorderYAML:    recorderSnippet(cfg, status.Snapshot),
		Measurements:    measurementsFor(status, lang),
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
	"sensors":      "/capture",
	"capture":      "/measurements",
	"measurements": "/measurements",
	"mqtt":         "/export",
	"ha":           "/export",
	"data":         "/export",
	"influx":       "/export",
	"app":          "/export",
}

// localAddress is the machine's own IPv4 address on the interface that carries
// the default route.
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

// handleSave applies one block of settings.
//
// Only the fields of that block are read: a form carries no evidence of the
// checkboxes it does not contain, so applying the whole configuration from a
// partial form would silently switch off everything on the other page.
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
	// What was there before the form was applied. Config returns a copy, so
	// this is a real snapshot and not a second view of the same struct.
	previous := cfg
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
		// Unless the broker moved. The password was entered for one broker,
		// and carrying it to another is how a save that never showed the
		// secret still manages to send it somewhere new. Host and port
		// together are the target: a different port is a different broker.
		if cfg.MQTTHost != previous.MQTTHost || cfg.MQTTPort != previous.MQTTPort {
			cfg.MQTTPassword = enteredSecret(r, "mqtt_password")
		}

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
		// The same rule as for the broker. The push carries this token in an
		// Authorization header on every interval, so the URL is the whole
		// question of who receives it.
		if cfg.InfluxURL != previous.InfluxURL {
			cfg.InfluxToken = enteredSecret(r, "influx_token")
		}

	case "sensors":
		cfg.GPUEnabled = r.FormValue("gpu_enabled") != ""
		cfg.CPUDetailEnabled = r.FormValue("cpu_detail_enabled") != ""
		cfg.CPUPerCore = r.FormValue("cpu_per_core") != ""
		cfg.RAMDetailEnabled = r.FormValue("ram_detail_enabled") != ""
		cfg.PawnIOEnabled = r.FormValue("pawnio_enabled") != ""
		cfg.SpecialHardwareEnabled = r.FormValue("special_hardware_enabled") != ""
		cfg.DiskEnabled = r.FormValue("disk_enabled") != ""
		cfg.DiskInclude = splitList(r.FormValue("disk_include"))
		cfg.NetEnabled = r.FormValue("net_enabled") != ""
		cfg.NetAllAdapters = r.FormValue("net_all_adapters") != ""
		cfg.BatteryEnabled = r.FormValue("battery_enabled") != ""
		cfg.PingEnabled = r.FormValue("ping_enabled") != ""
		cfg.PingTarget = strings.TrimSpace(r.FormValue("ping_target"))
		cfg.PingCount = formInt(r, "ping_count", cfg.PingCount)
		cfg.PingIntervalMs = formInt(r, "ping_interval_ms", cfg.PingIntervalMs)
		cfg.SelfUsageEnabled = r.FormValue("self_usage_enabled") != ""
		cfg.TopProcessesEnabled = r.FormValue("top_processes_enabled") != ""
		cfg.TopProcessesCount = formInt(r, "top_processes_count", cfg.TopProcessesCount)
		cfg.TopProcessesIntervalMs = formInt(r, "top_processes_interval_ms", cfg.TopProcessesIntervalMs)

	case "measurements":
		saveMeasurements(&cfg, r)

	case "capture":
		cfg.PollIntervalMs = formInt(r, "poll_interval_ms", cfg.PollIntervalMs)
		cfg.PublishIntervalMs = formInt(r, "interval_ms", cfg.PublishIntervalMs)
		cfg.IdlePublishIntervalMs = formInt(r, "idle_interval_ms", cfg.IdlePublishIntervalMs)
		cfg.IdleTimeoutMs = formInt(r, "idle_timeout_ms", cfg.IdleTimeoutMs)
		cfg.Decimals = r.FormValue("decimals") != ""

	case "app":
		cfg.Language = r.FormValue("language")
		cfg.NoGPU = r.FormValue("no_gpu") != ""
		cfg.WebPort = formInt(r, "web_port", cfg.WebPort)
		cfg.WebBindAll = r.FormValue("web_bind_all") != ""
		cfg.Autostart = r.FormValue("autostart") != ""
		cfg.Debug = r.FormValue("debug") != ""
		cfg.UpdateCheckEnabled = r.FormValue("update_check_enabled") != ""
		cfg.CrashReportOffered = r.FormValue("crash_report_offered") != ""
	}

	err := s.app.ApplyConfig(cfg)
	if err != nil {
		s.log.Error("apply config", "block", block, "error", err)
	}
	respondSave(w, r, page, "#"+block, err)
}

// respondSave answers a save.
//
// A page that applies every change as it is made posts in the background and
// has no use for a redirect — following one would fetch the whole page again
// for every keystroke. It says so with a header, and gets an empty answer.
// An ordinary form submit still wants the redirect it has always got.
func respondSave(w http.ResponseWriter, r *http.Request, page, anchor string, err error) {
	if r.Header.Get("X-Quiet") == "1" {
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Redirect(w, r, page+"?error="+neturl.QueryEscape(err.Error())+anchor, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, page+"?saved=1"+anchor, http.StatusSeeOther)
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

// handleDismiss puts a one-off hint away for good.
//
// Stored in the configuration rather than in the browser: the interface moves
// to a random port whenever the configured one is taken, and a different port
// is a different origin, so anything kept in local storage was gone again on
// the next start.
func (s *Server) handleDismiss(w http.ResponseWriter, r *http.Request) {
	notice := r.FormValue("what")
	// The crash banner is not a setting. It is dismissed for this session and
	// the record stays on disk, so dismissing it by accident loses nothing.
	if notice == "crash" {
		s.app.DismissCrash()
		http.Redirect(w, r, backTo(r), http.StatusSeeOther)
		return
	}

	cfg := s.app.Config()
	switch notice {
	case "recorder":
		cfg.RecorderNoticeRead = true
	case "no_gpu":
		cfg.NoGPU = true
	default:
		http.NotFound(w, r)
		return
	}

	if err := s.app.ApplyConfig(cfg); err != nil {
		s.log.Error("could not remember dismissed notice", "notice", notice, "error", err)
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

// enteredSecret is what this request actually carried, and nothing else.
//
// Used where the target moved. "Blank keeps what is stored" is right while the
// target stays put and wrong the moment it does not: a secret was entered for
// one broker or one database, and taking it along to a new address is how a
// page that never shows a secret still manages to send it somewhere new. Whoever
// moves the target types the secret again, or there is none.
func enteredSecret(r *http.Request, field string) string {
	return strings.TrimSpace(r.FormValue(field))
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

// amdPresence reports whether a Radeon is in the inventory, and whether its own
// driver is the source of the live readings.
//
// The vendor comes from the card, the origin from the individual readings —
// both are already collected, so this asks the snapshot rather than adding a
// second path through the collector. An AMD card with a silent driver is the
// only case where the AMD download is worth offering, and it is exactly the
// case a machine with the display driver alone lands in.
func amdPresence(snap collector.Snapshot) (card, driver bool) {
	for _, reading := range snap.Readings {
		if reading.Def.ID == metrics.GPUVendor.ID && strings.EqualFold(reading.Text, "AMD") {
			card = true
		}
		if reading.Origin == gpu.ADLXOrigin {
			driver = true
		}
	}
	// A driver answering without a card named is not a state worth describing,
	// and saying so would only produce a banner nobody can act on.
	return card, card && driver
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
	NoGPU       bool   `json:"no_gpu"`

	// What an idle FPS tile means depends on the card. AMDCard says a Radeon is
	// in the inventory; AMDDriver says its own driver is the one answering, in
	// which case temperatures, clocks, fan and power are already there and only
	// the frame rate is missing. The two apart are what lets the banner ask for
	// the right thing instead of always naming Afterburner.
	AMDCard   bool `json:"amd_card"`
	AMDDriver bool `json:"amd_driver"`

	// FPSOrigin is empty when the frame rate came from RTSS or from nowhere,
	// and names the driver when it stood in. GPUPresent decides whether the
	// dismiss button may claim there is no graphics card, which on a machine
	// that has one would simply be untrue.
	FPSOrigin  string `json:"fps_origin"`
	GPUPresent bool   `json:"gpu_present"`
	// FPSAvailable answers "RTSS or the graphics driver?" once, so the tile
	// does not have to reassemble the question out of the other fields.
	FPSAvailable bool `json:"fps_available"`

	// The update box. It comes through the API rather than the template
	// because a check that finishes a second after the page loaded should put
	// the box on screen without a reload.
	Update updateStatus `json:"update"`

	// Groups carries the optional sensor groups, so the page can show GPU,
	// disk and network readings without the server knowing what they are.
	Groups  []groupStatus  `json:"groups"`
	Exports []exportStatus `json:"exports"`

	Paused    bool   `json:"paused"`
	UpdatedAt string `json:"updated_at"`

	// What the exporter is currently doing, for the chips under the tiles.
	// They come through the API rather than the template because the page is
	// not reloaded when a setting is saved from the other tab.
	Preset      string `json:"preset"`
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
	// Stale marks a row the last reading did not contain, held on screen by
	// the linger so that one missed poll does not resize the panel. The value
	// is the last one actually measured, and the page dims it: a number that is
	// no longer being taken must not look like one that is.
	//
	// Display only. Nothing that leaves the machine is built from these rows —
	// see linger.go.
	Stale bool `json:"stale"`

	// key identifies the reading across polls, and defID puts a row back where
	// it belongs when it returns. Both are unexported, so neither reaches the
	// browser: the page has no use for them, and the key is an export
	// identifier that has no business being restated here.
	key   string
	defID string
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
	Failed    bool   `json:"failed"`
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

// handleIconPNG serves the mark for anything that renders in a browser.
//
// Home Assistant fetches this one from wherever it is being viewed, so it has
// to be reachable from there — which is the same condition the device link is
// under, and why the picture is only advertised when the interface listens on
// the network.
func (s *Server) handleIconPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "icon.png", time.Time{}, bytes.NewReader(assets.IconPNG))
}

func (s *Server) handleAPIStatus(w http.ResponseWriter, _ *http.Request) {
	resp := s.statusFor(s.app.Status())

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Debug("status encode failed", "error", err)
	}
}

// statusFor is the whole payload the dashboard polls for, built from one
// reading.
//
// Separate from the handler so that the path the page actually takes can be
// driven from a test with a reading of its own choosing — an App will not hand
// out one. That matters for the row linger below: it is a single call, and
// dropping it leaves every test of the store itself green while the panels go
// back to flickering. Measured that way round before this was split out.
func (s *Server) statusFor(st app.Status) statusResponse {
	snap := st.Snapshot
	lang := st.Config.Lang()
	amdCard, amdDriver := amdPresence(snap)

	resp := statusResponse{
		FPS:          snap.FPS(),
		Frametime:    snap.FrametimeMs(),
		Game:         snap.Game(),
		Resolution:   snap.Resolution(),
		RefreshRate:  snap.RefreshHz(),
		CPU:          snap.CPUPercent(),
		RAM:          snap.RAMPercent(),
		RAMUsedMB:    uint64(snap.Number(metrics.RAMUsed.ID)),
		RAMTotalMB:   uint64(snap.Number(metrics.RAMTotal.ID)),
		RTSSStatus:   string(snap.RTSSStatus),
		RTSSMessage:  snap.RTSSMessage,
		RTSSVersion:  snap.RTSSVersion,
		NoGPU:        st.Config.NoGPU,
		AMDCard:      amdCard,
		AMDDriver:    amdDriver,
		FPSOrigin:    snap.FPSOrigin,
		GPUPresent:   snap.Has(metrics.GPUName.ID),
		FPSAvailable: snap.HasFrameRate(),
		// Through the linger, which holds a row for a few polls after its
		// reading stops arriving. It works on the rendered panels and on
		// nothing else: the snapshot above is the one the exporters were
		// handed, and it stays exactly as it was collected.
		Groups:      s.rows.keep(groupStatuses(st, lang), st.UpdatedAt, pollPace(st)),
		Exports:     make([]exportStatus, 0, len(st.Exports)),
		Paused:      st.Paused,
		Preset:      st.Config.Measurements.Preset,
		Decimals:    st.Config.Decimals,
		EntityCount: len(snap.Entities()),
		PublishMs:   publishPace(st),
		Rendering:   snap.Rendering(),
		Update:      updateStatusOf(st),
	}
	for _, e := range st.Exports {
		resp.Exports = append(resp.Exports, exportStatus{
			Name:      e.Name,
			Label:     e.Label,
			Healthy:   e.Healthy,
			Failed:    e.Failed,
			Detail:    e.Detail,
			Delivered: e.Delivered,
		})
	}
	if !st.UpdatedAt.IsZero() {
		resp.UpdatedAt = st.UpdatedAt.Format(time.TimeOnly)
	}
	return resp
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

// pollPace is how often a new reading is taken, which is the unit the row
// linger counts in.
//
// The poll interval and not either publish interval: the dashboard is fed every
// poll, whether or not that reading was also exported, so a poll is what "one
// tick" means to somebody watching the page.
func pollPace(st app.Status) time.Duration {
	return time.Duration(st.Config.PollIntervalMs) * time.Millisecond
}

// updateStatus is the update box, flattened for the page.
type updateStatus struct {
	// Available drives the box. False while nothing newer exists, while the
	// check is switched off, and on a build that cannot update itself.
	Available bool   `json:"available"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Summary   string `json:"summary"`
	// InProgress keeps the button from being pressed twice, and says why the
	// program is about to disappear for a moment.
	InProgress bool `json:"in_progress"`
	// Error is the last failure. Shown, because an install that quietly did
	// nothing is the worst of the three possible outcomes.
	Error string `json:"error"`
}

func updateStatusOf(st app.Status) updateStatus {
	return updateStatus{
		Available:  st.UpdateAvailable,
		Installed:  st.Update.InstalledVersion,
		Latest:     st.Update.LatestVersion,
		Title:      st.Update.Title,
		URL:        st.Update.ReleaseURL,
		Summary:    st.Update.ReleaseSummary,
		InProgress: st.Update.InProgress,
		Error:      st.Update.LastError,
	}
}

// handleUpdate installs the release the last check found.
//
// A POST with nothing in it: the only thing to say is "yes". Which release it
// installs is whatever the check settled on, and offering the version as a
// parameter would only invite somebody to ask for one that was never verified.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if err := s.app.RequestUpdateInstall(); err != nil {
		s.log.Warn("update install refused", "error", err)
		http.Redirect(w, r, "/?error="+neturl.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.log.Info("update install requested from the interface")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// groupStatuses turns the optional sensor groups into display rows.
//
// A group that is switched on but produced nothing is reported as unavailable
// with the source's own explanation, which is what tells the user that
// Afterburner is not running rather than leaving an empty panel.
func groupStatuses(st app.Status, lang i18n.Lang) []groupStatus {
	snap := st.Snapshot
	enabled := map[metrics.Group]bool{
		metrics.GroupGPU:     st.Config.GPUEnabled,
		metrics.GroupCPU:     st.Config.CPUDetailEnabled,
		metrics.GroupRAM:     st.Config.RAMDetailEnabled,
		metrics.GroupDisk:    st.Config.DiskEnabled,
		metrics.GroupNet:     st.Config.NetEnabled,
		metrics.GroupCooling: st.Config.SpecialHardwareEnabled,
		metrics.GroupBattery: st.Config.BatteryEnabled,
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
		// A missing graphics card or an unreachable adapter is worth a panel
		// saying so: the user probably expected one. A missing battery is not.
		// Most machines are desktops, and a permanently empty box telling
		// somebody their tower has no battery is noise on every page load. The
		// same is true of a cooling controller: most machines have none, and
		// the ones that do have a cooler nobody here can read.
		if optionalGroups[group] && !status.Available && status.Error == "" {
			continue
		}
		if status.Enabled && status.Available {
			status.Rows = rowsFor(snap, group, lang)
		}
		out = append(out, status)
	}
	return out
}

// optionalGroups are the two whose absence is not news.
//
// Every other group belongs to hardware the machine certainly has, so a panel
// saying "no data" is an answer worth showing. A battery in a tower and a
// water cooler on a machine with an air cooler are not missing — they were
// never there.
var optionalGroups = map[metrics.Group]bool{
	metrics.GroupBattery: true,
	metrics.GroupCooling: true,
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
			key:      reading.Key(),
			defID:    reading.Def.ID,
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
	case metrics.KindTable:
		return r.TableText()
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
