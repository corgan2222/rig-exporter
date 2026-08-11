//go:build windows

// Package app is the runtime that ties the pieces together: it polls the
// collector on a timer, fans each snapshot out to every enabled export target,
// and exposes a single Status that both the tray and the settings page render.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/corgan2222/rig-exporter/internal/autostart"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/crashlog"
	"github.com/corgan2222/rig-exporter/internal/export"
	"github.com/corgan2222/rig-exporter/internal/export/dataserver"
	"github.com/corgan2222/rig-exporter/internal/export/influxpush"
	"github.com/corgan2222/rig-exporter/internal/hamqtt"
	"github.com/corgan2222/rig-exporter/internal/hardware/gpu"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/rtss"
	"github.com/corgan2222/rig-exporter/internal/sysinfo"
	"github.com/corgan2222/rig-exporter/internal/updater"
)

type updateController interface {
	updater.Controller
	Start()
	Stop()
	SetCheckEnabled(bool)
}

// Status is a consistent view of everything the UI shows.
type Status struct {
	Snapshot collector.Snapshot
	// Exports lists one entry per enabled target, in configuration order.
	Exports   []export.Status
	Config    config.Config
	Paused    bool
	Autostart bool
	UpdatedAt time.Time
	// Update is what the updater currently knows. Zero when this build cannot
	// update itself at all, which is why UpdateAvailable is derived here
	// rather than by every reader comparing two version strings.
	Update          updater.State
	UpdateAvailable bool
	// Churn is the share of published entities whose value actually differed
	// from the previous publish, and how many publishes that was averaged
	// over. It is what turns a database estimate from a guess into an
	// arithmetic problem: Home Assistant stores a row on change, not on
	// publish, and the difference between those two is most of the answer.
	Churn        float64
	ChurnSamples int
	// Crash is the previous session's record, when it did not end on purpose.
	// Nil once it has been seen, and nil on every ordinary start — which is
	// what makes a banner built from it something a user notices.
	Crash *crashlog.Report
}

// SetCrash hands over what the previous session left behind, so the interface
// can say so. Called once at startup.
func (a *App) SetCrash(report *crashlog.Report) {
	a.mu.Lock()
	a.crash = report
	a.mu.Unlock()
	a.notify()
}

// DismissCrash stops showing the record. The file stays where it is — somebody
// who dismissed a banner by accident should still be able to find the report,
// and it is pruned by age rather than by a click.
func (a *App) DismissCrash() {
	a.mu.Lock()
	a.crash = nil
	a.mu.Unlock()
	a.notify()
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
	updates updateController

	mu        sync.RWMutex
	cfg       config.Config
	collector *collector.Collector
	sensors   *sensors
	runners   []*runner
	last      collector.Snapshot
	updatedAt time.Time
	// What the previous publish carried, and the running share of entities
	// that differ from one publish to the next. Guarded by mu with the rest.
	lastPublished map[string]string
	churn         float64
	churnSamples  int
	paused        bool
	// crash is what the previous session left behind, held until somebody has
	// seen it. Guarded by mu like the rest.
	crash     *crashlog.Report
	listeners []func(Status)

	// webURLFn reports where the settings interface is really listening. It
	// arrives after New, because the web server picks its port later, and it is
	// read from the export goroutine — hence a lock of its own rather than a
	// plain field.
	webURLMu sync.RWMutex
	webURLFn func() string

	stop    chan struct{}
	restart chan struct{}
	done    chan struct{}
}

// SetWebURL tells the app where the settings interface can be reached, so the
// Home Assistant device page can link to it.
//
// Called once the web server has bound a port, which is after the runtime is
// built: the address is not knowable earlier, because a busy port sends the
// server to an ephemeral one.
func (a *App) SetWebURL(fn func() string) {
	a.webURLMu.Lock()
	a.webURLFn = fn
	a.webURLMu.Unlock()
}

// currentWebURL is that address, empty while nothing has reported one.
func (a *App) currentWebURL() string {
	a.webURLMu.RLock()
	fn := a.webURLFn
	a.webURLMu.RUnlock()

	if fn == nil {
		return ""
	}
	return fn()
}

// New builds the runtime. cfgPath is where ApplyConfig persists changes.
func New(cfg config.Config, cfgPath string, log *slog.Logger, updates updateController) *App {
	a := &App{
		log:     log,
		cfgPath: cfgPath,
		system:  sysinfo.New(),
		cfg:     cfg,
		updates: updates,
		stop:    make(chan struct{}),
		restart: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	applyMetricsOptions(cfg)
	a.collector, a.sensors = buildCollector(cfg, a.reader, a.system, log)
	a.runners = a.buildRunners(cfg, log)
	return a
}

// applyMetricsOptions hands the two settings that live as package state in
// metrics over to it. Both take effect from the next reading, so neither needs
// anything rebuilt — which is exactly why they are package state and not
// constructor arguments to a dozen hardware sources.
func applyMetricsOptions(cfg config.Config) {
	metrics.SetDecimals(cfg.Decimals)
	metrics.SetSelection(selectionFor(cfg))
}

// selectionFor turns the stored rung and the hand-picked exceptions into the
// set of measurements that is actually collected.
func selectionFor(cfg config.Config) map[string]bool {
	return metrics.Resolve(
		metrics.Preset(cfg.Measurements.Preset),
		cfg.Measurements.Added,
		cfg.Measurements.Removed,
	)
}

// buildCollector wires the core source together with the optional ones the
// configuration asks for.
func buildCollector(cfg config.Config, reader rtss.Reader, system *sysinfo.Provider, log *slog.Logger) (*collector.Collector, *sensors) {
	c := collector.New(reader, system, cfg.IdleTimeoutMs, log)
	c.ReportVersion(config.VersionString())
	c.ReportSelfUsage(cfg.SelfUsageEnabled)
	// Registered whatever the graphics group is set to, because the frame rate
	// belongs to the always-on core group — the same reason RTSS is read even
	// with the graphics group switched off. On a machine without an AMD driver
	// this never produces a reading.
	c.UseFrameRateFallback(gpu.ADLXOrigin, gpu.ADLXFrameRate)
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

	if a.updates != nil {
		// Set before Start, or the first check would already be on its way
		// while the setting says it should not happen at all.
		a.updates.SetCheckEnabled(a.Config().UpdateCheckEnabled)
		a.updates.Start()
	}
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
	if a.updates != nil {
		a.updates.Stop()
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
	due := polls == 0 || polls >= a.pollsPerPublish(snap.Rendering())
	if due && !paused {
		for _, r := range runners {
			r.export(snap)
		}
		a.measureChurn(snap)
	}
	a.notify()
	return due && !paused
}

// churnSmoothing weights the newest publish against the running average. A
// tenth: enough that starting a game shows up within a minute, little enough
// that one noisy sample does not become the estimate.
const churnSmoothing = 0.1

// measureChurn counts how many published entities carry a different value than
// they did at the previous publish.
//
// Measured rather than assumed, because this is the number the whole database
// estimate rests on and there is no honest way to guess it: it depends on the
// hardware, on what the machine is doing, and above all on whether decimals are
// switched on — a temperature to one decimal changes almost every time, the
// same temperature rounded to a whole degree changes a few times an hour.
func (a *App) measureChurn(snap collector.Snapshot) {
	entities := snap.Entities()
	if len(entities) == 0 {
		return
	}

	current := make(map[string]string, len(entities))
	for _, r := range entities {
		current[r.Key()] = fmt.Sprint(r.Value())
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.lastPublished != nil {
		changed := 0
		for key, value := range current {
			if previous, seen := a.lastPublished[key]; !seen || previous != value {
				changed++
			}
		}
		share := float64(changed) / float64(len(current))
		if a.churnSamples == 0 {
			a.churn = share
		} else {
			a.churn += (share - a.churn) * churnSmoothing
		}
		a.churnSamples++
	}
	a.lastPublished = current
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

		Churn:        a.churn,
		ChurnSamples: a.churnSamples,
		Crash:        a.crash,
	}
	a.mu.RUnlock()

	// Read from the registry rather than from config, so an entry removed
	// outside rig-exporter is reflected instead of trusted from the file.
	if on, err := autostart.Enabled(config.AppName); err == nil {
		status.Autostart = on
	}
	if a.updates != nil {
		status.Update = a.updates.State()
		status.UpdateAvailable = status.Update.LatestVersion != "" &&
			status.Update.LatestVersion != status.Update.InstalledVersion
	}
	return status
}

// RequestUpdateInstall installs the release the last check found.
//
// The interface offers this as a button rather than leaving it to Home
// Assistant, because somebody who has just been told a new version exists is
// looking at the page that told them. Nothing happens without the click:
// checking and installing stay two separate decisions.
func (a *App) RequestUpdateInstall() error {
	if a.updates == nil {
		return errors.New("this build cannot update itself")
	}
	return a.updates.RequestInstall()
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
	// The last reading was taken under the old configuration, so it still names
	// every measurement that is about to be dropped — with its real instances,
	// which no list of definitions could supply. Captured under the same lock
	// that swaps the configuration, or a tick landing in between would replace
	// it with an already-filtered one and there would be nothing left to retire.
	lastSnapshot := a.last
	rebuild := exportsChanged(oldCfg, newCfg)
	identityChanged := topicsChanged(oldCfg, newCfg)
	rebuildSensors := sensorsChanged(oldCfg, newCfg)

	a.cfg = newCfg
	applyMetricsOptions(newCfg)
	if a.updates != nil {
		a.updates.SetCheckEnabled(newCfg.UpdateCheckEnabled)
	}
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

		live := startRunners(a.buildRunners(newCfg, a.log), a.log)
		a.mu.Lock()
		a.runners = live
		a.mu.Unlock()
	}

	// After the rebuild, on whichever targets are now live. Handing the
	// retirement to a publisher that is about to be thrown away would lose it,
	// and a freshly built one is not connected yet — it queues and flushes when
	// it connects. Skipped when the identity changed, because clearDiscovery
	// above already retired everything.
	if !identityChanged {
		a.mu.RLock()
		liveRunners := a.runners
		a.mu.RUnlock()
		retireDropped(liveRunners, oldCfg, newCfg, lastSnapshot, a.log)
		retireUnselected(liveRunners, newCfg)
	}

	// Unconditionally, the way SetAutostart from the tray has always done it.
	//
	// The old condition compared the configuration against the configuration,
	// and the page shows the registry: Status calls autostart.Enabled, which
	// checks the Run value against the path this executable is at right now. So
	// a moved or freshly downloaded binary makes the page say off while
	// config.json still says on — and ticking the box then changes nothing,
	// because old and new agree. That is precisely the case somebody is trying
	// to repair when they tick it.
	//
	// One registry write per save is the price. It is idempotent, and two ways
	// to the same switch that follow two different rules is worse.
	if err := autostart.Set(config.AppName, newCfg.Autostart); err != nil {
		a.log.Error("autostart update failed", "error", err)
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

// retireDropped removes the Home Assistant entities that the new configuration
// will no longer produce.
//
// Only a narrowed sensor set counts. Switching a group off, or hardware that
// stops answering, deliberately leaves its entities alone: those come back, and
// an entity that comes back after being retired has lost its history for
// nothing. Narrowing the set is different — it is a statement about what should
// exist, not about what happened to be readable this second.
func retireDropped(runners []*runner, oldCfg, newCfg config.Config, snap collector.Snapshot, log *slog.Logger) {
	dropped := droppedBySelection(oldCfg, newCfg, snap)
	dropped = append(dropped, droppedBySelfUsage(oldCfg, newCfg, snap)...)
	if len(dropped) == 0 {
		return
	}

	log.Info("retiring entities the new configuration no longer produces", "count", len(dropped))
	for _, r := range runners {
		if publisher, ok := r.target.(*hamqtt.Publisher); ok {
			publisher.Retire(hamqtt.EntityRefs(dropped))
		}
	}
}

// retireUnselected clears anything still announced that the new selection does
// not carry.
//
// The list above is built from the last reading, which is the right answer for
// everything the machine was producing at that moment — and blind to an entity
// whose measurement had gone quiet: a drive that spun down, a card that stopped
// answering. Home Assistant is still being told those exist, because the
// discovery message is retained. This asks the publisher what it announced
// instead, which is exactly the set with a retained message on the broker.
func retireUnselected(runners []*runner, newCfg config.Config) {
	selection := selectionFor(newCfg)
	for _, r := range runners {
		if publisher, ok := r.target.(*hamqtt.Publisher); ok {
			publisher.RetireUnselected(func(defID string) bool { return selection[defID] })
		}
	}
}

// droppedBySelection is the decision on its own: which of the readings taken
// under the old configuration the new one will no longer produce.
//
// Compared set against set rather than rung against rung, because the ladder
// is no longer the only way to lose a measurement — unticking a single one has
// to retire its entity just as surely as sliding the whole thing down. Going
// the other way only adds measurements, and announceNew picks those up by
// itself.
func droppedBySelection(oldCfg, newCfg config.Config, snap collector.Snapshot) []metrics.Reading {
	before, after := selectionFor(oldCfg), selectionFor(newCfg)

	var dropped []metrics.Reading
	for _, r := range snap.Entities() {
		if before[r.Def.ID] && !after[r.Def.ID] {
			dropped = append(dropped, r)
		}
	}
	return dropped
}

// droppedBySelfUsage is the same decision for the two self-usage figures:
// switching the group off says they should stop existing, and they are in the
// standard set, so the check above never covers them.
func droppedBySelfUsage(oldCfg, newCfg config.Config, snap collector.Snapshot) []metrics.Reading {
	if !oldCfg.SelfUsageEnabled || newCfg.SelfUsageEnabled {
		return nil
	}

	var dropped []metrics.Reading
	for _, r := range snap.Entities() {
		switch r.Def.ID {
		case metrics.ExporterCPU.ID, metrics.ExporterMemory.ID:
			dropped = append(dropped, r)
		}
	}
	return dropped
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
	//
	// The sensor set is not: going from standard to extended only adds
	// measurements, and announceNew picks those up on its own at the next
	// reading.
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
//
// A method rather than a function because the MQTT publisher needs the address
// of the settings interface, and that is not known yet the first time this runs.
// Passing the accessor rather than the string is what lets the publisher ask
// again later.
func (a *App) buildRunners(cfg config.Config, log *slog.Logger) []*runner {
	var runners []*runner

	if cfg.MQTTEnabled {
		runners = append(runners, newRunner(hamqtt.New(cfg, log, a.currentWebURL, a.updates), log))
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
