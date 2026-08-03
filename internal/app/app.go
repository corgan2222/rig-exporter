//go:build windows

// Package app is the runtime that ties the pieces together: it polls the
// collector on a timer, fans each snapshot out to every enabled export target,
// and exposes a single Status that both the tray and the settings page render.
package app

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/corgan/rig-exporter/internal/autostart"
	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/export"
	"github.com/corgan/rig-exporter/internal/export/dataserver"
	"github.com/corgan/rig-exporter/internal/export/influxpush"
	"github.com/corgan/rig-exporter/internal/hamqtt"
	"github.com/corgan/rig-exporter/internal/metrics"
	"github.com/corgan/rig-exporter/internal/rtss"
	"github.com/corgan/rig-exporter/internal/sysinfo"
)

// Status is a consistent view of everything the UI shows.
type Status struct {
	Snapshot collector.Snapshot
	// Exports lists one entry per enabled target, in configuration order.
	Exports   []export.Status
	Config    config.Config
	Paused    bool
	Autostart bool
	UpdatedAt time.Time
}

// Export returns the status of the named target, if it is enabled.
func (s Status) Export(name string) (export.Status, bool) {
	for _, e := range s.Exports {
		if e.Name == name {
			return e, true
		}
	}
	return export.Status{}, false
}

// App owns the polling loop and the export targets.
type App struct {
	log     *slog.Logger
	cfgPath string
	system  *sysinfo.Provider
	reader  rtss.Reader

	mu        sync.RWMutex
	cfg       config.Config
	collector *collector.Collector
	sensors   *sensors
	runners   []*runner
	last      collector.Snapshot
	updatedAt time.Time
	paused    bool
	listeners []func(Status)

	stop    chan struct{}
	restart chan struct{}
	done    chan struct{}
}

// New builds the runtime. cfgPath is where ApplyConfig persists changes.
func New(cfg config.Config, cfgPath string, log *slog.Logger) *App {
	a := &App{
		log:     log,
		cfgPath: cfgPath,
		system:  sysinfo.New(),
		cfg:     cfg,
		stop:    make(chan struct{}),
		restart: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	metrics.SetDecimals(cfg.Decimals)
	a.collector, a.sensors = buildCollector(cfg, a.reader, a.system, log)
	a.runners = buildRunners(cfg, log)
	return a
}

// buildCollector wires the core source together with the optional ones the
// configuration asks for.
func buildCollector(cfg config.Config, reader rtss.Reader, system *sysinfo.Provider, log *slog.Logger) (*collector.Collector, *sensors) {
	c := collector.New(reader, system, cfg.IdleTimeoutMs, log)
	c.ReportVersion(config.VersionString())
	s := buildSensors(cfg, system, log)
	c.AddSource(s.sources...)
	return c, s
}

// CheckRTSS reports whether the RTSS shared memory can be read right now. It
// is what the startup check and the "RTSS missing" prompt are based on.
func (a *App) CheckRTSS() error { return a.reader.Available() }

// Start brings up every export target and begins polling. A target that fails
// to start is dropped rather than taking the whole app down: a busy port must
// not stop MQTT from working.
func (a *App) Start() {
	a.mu.RLock()
	runners, sensors := a.runners, a.sensors
	a.mu.RUnlock()

	sensors.start()
	live := startRunners(runners, a.log)

	a.mu.Lock()
	a.runners = live
	a.mu.Unlock()

	go a.loop()
}

func startRunners(runners []*runner, log *slog.Logger) []*runner {
	live := make([]*runner, 0, len(runners))
	for _, r := range runners {
		if err := r.target.Start(); err != nil {
			log.Error("export target could not start", "target", r.target.Status().Name, "error", err)
			continue
		}
		live = append(live, r)
	}
	return live
}

// Stop ends the polling loop and shuts every target down, so Home Assistant
// sees "offline" rather than waiting for the keepalive to expire.
func (a *App) Stop() {
	select {
	case <-a.stop:
		return // already stopping
	default:
		close(a.stop)
	}
	<-a.done

	a.mu.RLock()
	runners, sensors := a.runners, a.sensors
	a.mu.RUnlock()

	sensors.stop()
	for _, r := range runners {
		r.target.Stop()
	}
}

// loop reads the hardware on the poll interval and hands a reading to the
// export targets on the publish interval.
//
// The two are separate because they answer different questions: the tray and
// the settings page want a number that moves, while a broker and a time series
// database want one sample every couple of seconds. Publishing is counted in
// polls rather than timed on its own, so the two can never drift apart.
func (a *App) loop() {
	defer close(a.done)

	ticker := time.NewTicker(a.pollInterval())
	defer ticker.Stop()

	// polls counts reads since the last export rather than reads overall. The
	// two publish intervals differ by a factor of five, so counting from the
	// last export is what lets a game starting take effect at the next read
	// instead of at the end of the idle interval that happened to be running.
	polls := uint64(0)
	a.tick(0) // export immediately instead of waiting out the first interval

	for {
		select {
		case <-a.stop:
			return
		case <-a.restart:
			ticker.Reset(a.pollInterval())
			polls = 0
			a.tick(0)
		case <-ticker.C:
			polls++
			if a.tick(polls) {
				polls = 0
			}
		}
	}
}

func (a *App) pollInterval() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return time.Duration(a.cfg.PollIntervalMs) * time.Millisecond
}

// pollsPerPublish is how many reads happen per export. Normalize guarantees
// both publish intervals are a whole multiple of the poll interval.
//
// Which of the two applies is decided per reading rather than once at startup:
// a game starting is exactly the moment the faster pace becomes worth its
// traffic, and a game closing exactly the moment it stops being.
func (a *App) pollsPerPublish(rendering bool) uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.cfg.PollIntervalMs <= 0 {
		return 1
	}
	interval := a.cfg.IdlePublishIntervalMs
	if rendering {
		interval = a.cfg.PublishIntervalMs
	}
	every := uint64(interval / a.cfg.PollIntervalMs)
	if every == 0 {
		return 1
	}
	return every
}

// rendering reports whether a game is actually producing frames, which is what
// picks between the two publish intervals. Both halves matter: RTSS keeps an
// entry alive for a moment after the last frame, and a game sitting at zero
// frames a second has nothing to say that is worth a fast series.
func rendering(snap collector.Snapshot) bool {
	return snap.GameRunning() && snap.FPS() > 0
}

// tick takes one reading and reports whether it reached the export targets.
// polls is how many reads have happened since the last export, this one
// included; zero forces an export whatever the interval says.
//
// Whether an export is due can only be decided after the reading, because the
// pace depends on whether a game is rendering — which is one of the things the
// reading tells us.
func (a *App) tick(polls uint64) bool {
	a.mu.RLock()
	c, runners, paused := a.collector, a.runners, a.paused
	a.mu.RUnlock()

	snap := c.Collect()

	a.mu.Lock()
	a.last = snap
	a.updatedAt = time.Now()
	a.mu.Unlock()

	// While paused the counter keeps running, so lifting the pause exports at
	// once rather than after another full interval of silence.
	due := polls == 0 || polls >= a.pollsPerPublish(rendering(snap))
	if due && !paused {
		for _, r := range runners {
			r.export(snap)
		}
	}
	a.notify()
	return due && !paused
}

// Status returns the latest reading together with every target's state.
func (a *App) Status() Status {
	a.mu.RLock()
	status := Status{
		Snapshot:  a.last,
		Exports:   exportStatuses(a.runners),
		Config:    a.cfg,
		Paused:    a.paused,
		UpdatedAt: a.updatedAt,
	}
	a.mu.RUnlock()

	// Read from the registry rather than from config, so an entry removed
	// outside rig-exporter is reflected instead of trusted from the file.
	if on, err := autostart.Enabled(config.AppName); err == nil {
		status.Autostart = on
	}
	return status
}

func exportStatuses(runners []*runner) []export.Status {
	out := make([]export.Status, 0, len(runners))
	for _, r := range runners {
		out = append(out, r.target.Status())
	}
	return out
}

// Config returns a copy of the active configuration.
func (a *App) Config() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// SetPaused stops or resumes exporting. Collection keeps running so the tray
// and settings page still show live numbers while paused.
func (a *App) SetPaused(paused bool) {
	a.mu.Lock()
	a.paused = paused
	a.mu.Unlock()

	a.log.Info("exporting toggled", "paused", paused)
	a.notify()
}

// Paused reports whether exporting is suspended.
func (a *App) Paused() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.paused
}

// SetAutostart writes the registry entry and mirrors it into the config file.
func (a *App) SetAutostart(enabled bool) error {
	if err := autostart.Set(config.AppName, enabled); err != nil {
		return err
	}

	a.mu.Lock()
	a.cfg.Autostart = enabled
	cfg := a.cfg
	a.mu.Unlock()

	if err := config.Save(a.cfgPath, cfg); err != nil {
		return fmt.Errorf("persist autostart: %w", err)
	}
	a.notify()
	return nil
}

// ApplyConfig persists newCfg and brings the runtime in line with it.
//
// Export targets are torn down and rebuilt whenever anything they depend on
// changes, which is simpler than working out what each of them could adapt to
// in place. A change of identity (node id or topic prefixes) additionally
// retires the old Home Assistant entities first, so renaming a PC does not
// leave a permanently unavailable device behind.
func (a *App) ApplyConfig(newCfg config.Config) error {
	newCfg.Normalize()

	if err := config.Save(a.cfgPath, newCfg); err != nil {
		return err
	}

	a.mu.Lock()
	oldCfg := a.cfg
	oldRunners := a.runners
	oldSensors := a.sensors
	rebuild := exportsChanged(oldCfg, newCfg)
	identityChanged := topicsChanged(oldCfg, newCfg)
	rebuildSensors := sensorsChanged(oldCfg, newCfg)

	a.cfg = newCfg
	// Precision is package state in metrics, so it changes for the next
	// reading without rebuilding anything.
	metrics.SetDecimals(newCfg.Decimals)
	if rebuildSensors {
		a.collector, a.sensors = buildCollector(newCfg, a.reader, a.system, a.log)
	}
	newSensors := a.sensors
	a.mu.Unlock()

	if rebuildSensors {
		oldSensors.stop()
		newSensors.start()
	}

	if rebuild || identityChanged {
		if identityChanged {
			clearDiscovery(oldRunners)
		}
		for _, r := range oldRunners {
			r.target.Stop()
		}

		live := startRunners(buildRunners(newCfg, a.log), a.log)
		a.mu.Lock()
		a.runners = live
		a.mu.Unlock()
	}

	if oldCfg.Autostart != newCfg.Autostart {
		if err := autostart.Set(config.AppName, newCfg.Autostart); err != nil {
			a.log.Error("autostart update failed", "error", err)
		}
	}

	// The one-off cleanup of the previous application name's entities has now
	// either happened or will never be needed again.
	if newCfg.LegacyCleanupPending {
		newCfg.LegacyCleanupPending = false
		a.mu.Lock()
		a.cfg.LegacyCleanupPending = false
		a.mu.Unlock()
		if err := config.Save(a.cfgPath, newCfg); err != nil {
			a.log.Warn("could not clear the legacy cleanup flag", "error", err)
		}
	}

	a.log.Info("configuration applied",
		"exports_rebuilt", rebuild || identityChanged,
		"sensors_rebuilt", rebuildSensors,
		"identity_changed", identityChanged)

	select {
	case a.restart <- struct{}{}:
	default: // a reset is already pending, which achieves the same thing
	}
	return nil
}

// clearDiscovery retires the Home Assistant entities of the old identity.
func clearDiscovery(runners []*runner) {
	for _, r := range runners {
		if publisher, ok := r.target.(*hamqtt.Publisher); ok {
			publisher.ClearDiscovery()
		}
	}
}

// exportsChanged reports whether any target needs to be rebuilt.
func exportsChanged(a, b config.Config) bool {
	// The language is in here because entity names follow it: rebuilding the
	// publisher makes it re-announce every entity under its new name. Decimals
	// is in here for the same reason — the discovery payload carries the
	// display precision, and promising a decimal that no longer arrives would
	// render every value as x.0 until the next restart.
	return a.Language != b.Language ||
		a.Decimals != b.Decimals ||
		a.MQTTEnabled != b.MQTTEnabled ||
		a.MQTTHost != b.MQTTHost ||
		a.MQTTPort != b.MQTTPort ||
		a.MQTTUsername != b.MQTTUsername ||
		a.MQTTPassword != b.MQTTPassword ||
		a.MQTTTLS != b.MQTTTLS ||
		a.MQTTTLSInsecure != b.MQTTTLSInsecure ||
		a.ClientID != b.ClientID ||

		a.DataServerEnabled != b.DataServerEnabled ||
		a.DataBindAddress != b.DataBindAddress ||
		a.DataPort != b.DataPort ||
		a.DataToken != b.DataToken ||
		a.JSONEnabled != b.JSONEnabled ||
		a.PrometheusEnabled != b.PrometheusEnabled ||
		a.InfluxPullEnabled != b.InfluxPullEnabled ||

		a.InfluxPushEnabled != b.InfluxPushEnabled ||
		a.InfluxURL != b.InfluxURL ||
		a.InfluxOrg != b.InfluxOrg ||
		a.InfluxBucket != b.InfluxBucket ||
		a.InfluxToken != b.InfluxToken ||
		a.InfluxMeasurement != b.InfluxMeasurement
}

func topicsChanged(a, b config.Config) bool {
	return a.NodeID != b.NodeID ||
		a.TopicPrefix != b.TopicPrefix ||
		a.DiscoveryPrefix != b.DiscoveryPrefix ||
		a.DeviceName != b.DeviceName
}

// OnUpdate registers a callback invoked after every tick and state change.
// Callbacks run on the caller's goroutine and must not block.
func (a *App) OnUpdate(fn func(Status)) {
	a.mu.Lock()
	a.listeners = append(a.listeners, fn)
	a.mu.Unlock()
}

func (a *App) notify() {
	a.mu.RLock()
	listeners := make([]func(Status), len(a.listeners))
	copy(listeners, a.listeners)
	a.mu.RUnlock()

	if len(listeners) == 0 {
		return
	}
	status := a.Status()
	for _, fn := range listeners {
		fn(status)
	}
}

// buildRunners creates one runner per enabled export target.
func buildRunners(cfg config.Config, log *slog.Logger) []*runner {
	var runners []*runner

	if cfg.MQTTEnabled {
		runners = append(runners, newRunner(hamqtt.New(cfg, log), log))
	}
	if cfg.DataServerEnabled {
		runners = append(runners, newRunner(dataserver.New(cfg, log), log))
	}
	if cfg.InfluxPushEnabled {
		runners = append(runners, newRunner(influxpush.New(cfg, log), log))
	}
	return runners
}

// runner exports to one target without letting a slow target hold up the
// collection loop, and without letting exports of the same target overlap.
type runner struct {
	target export.Target
	log    *slog.Logger
	busy   atomic.Bool
}

func newRunner(target export.Target, log *slog.Logger) *runner {
	return &runner{target: target, log: log}
}

func (r *runner) export(snap collector.Snapshot) {
	if !r.busy.CompareAndSwap(false, true) {
		// The previous export is still in flight. Skipping keeps the reading
		// current instead of queueing values that are already stale.
		r.log.Debug("export skipped, target still busy", "target", r.target.Status().Name)
		return
	}

	go func() {
		defer r.busy.Store(false)
		if err := r.target.Export(snap); err != nil {
			r.log.Warn("export failed", "target", r.target.Status().Name, "error", err)
		}
	}()
}
