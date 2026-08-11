// Package updater checks signed GitHub releases and prepares a verified
// executable for the Windows restart helper.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Masterminds/semver/v3"
)

const (
	// Twice a day was a guess made before anybody had used it. Six hours is
	// still nothing against a GitHub API that allows sixty unauthenticated
	// requests an hour, and it means a release published in the morning is
	// offered the same day rather than the next one.
	defaultCheckInterval  = 6 * time.Hour
	defaultCheckTimeout   = 30 * time.Second
	defaultInstallTimeout = 5 * time.Minute
	maxReleaseSize        = 50 << 20
	maxReleaseSummary     = 255

	// softwareTitle is what Home Assistant means by the title of an update
	// entity: the name of the software, not the name of the release.
	//
	// The GitHub release name went in here, which is "v1.7.1" — so the update
	// card never mentioned rig-exporter at all, and the overview row read
	// "v1.7.1 1.7.1", the title and the version saying the same thing twice.
	softwareTitle = "rig-exporter"

	// installSection opens the part of the release notes that explains how to
	// download and verify the file. Useful on the release page, worthless in a
	// notification with 255 characters to spend — and it is our own template
	// that puts it there, so cutting on it is cutting on something we own.
	installSection = "## Installing"
)

var (
	ErrBusy     = errors.New("an update is already in progress")
	ErrNoUpdate = errors.New("no newer update is available")
)

// State is the complete user-visible update state. Home Assistant consumes the
// version and release fields; LastError remains local because MQTT update
// entities have no standard error field.
type State struct {
	InstalledVersion string
	LatestVersion    string
	Title            string
	ReleaseSummary   string
	ReleaseURL       string
	LastError        string
	InProgress       bool
	UpdatePercentage *float64
}

// Controller is the small interface the MQTT adapter needs.
type Controller interface {
	State() State
	Subscribe(func(State)) func()
	RequestInstall() error
}

// Release is provider-independent release metadata kept behind the updater
// seam. Provider-specific objects stay private to their adapter.
type Release struct {
	Version string
	// No Title. GitHub names every release after its version, so it carried
	// nothing the version did not already say, and Home Assistant wants the
	// name of the software there instead — see softwareTitle.
	Notes  string
	URL    string
	Size   int
	native any
}

// PreparedUpdate is handed to the Windows apply adapter only after the release
// has been downloaded and cryptographically verified.
type PreparedUpdate struct {
	StagedPath     string
	ExecutablePath string
	ConfigPath     string
	Version        string
	SHA256         string
}

type releaseSource interface {
	Latest(context.Context) (Release, bool, error)
	// Stage downloads and verifies the release into target. report is called
	// with the percentage downloaded so far; it may be called any number of
	// times, including none at all when the source cannot tell.
	Stage(ctx context.Context, release Release, target string, report func(float64)) error
}

type applyPreparer interface {
	Prepare(PreparedUpdate) error
}

// Options defines the facts that differ between the running application and
// tests. Empty durations use conservative production defaults.
type Options struct {
	CurrentVersion string
	ExecutablePath string
	ConfigPath     string
	TempRoot       string
	CheckInterval  time.Duration
	CheckTimeout   time.Duration
	InstallTimeout time.Duration
	RequestRestart func()
	Logger         *slog.Logger
}

// Manager owns release checks and exactly one possible installation.
type Manager struct {
	source releaseSource
	apply  applyPreparer
	opts   Options
	log    *slog.Logger

	mu        sync.RWMutex
	state     State
	available *Release
	listeners map[uint64]func(State)
	nextID    uint64
	started   bool
	stopped   bool

	checkMu sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// checksOff switches the periodic look at GitHub off without tearing the
	// manager down. Stop is terminal — a stopped manager cannot be started
	// again — so a setting somebody can toggle twice in a minute needs
	// something gentler than the lifecycle.
	checksOff atomic.Bool
}

// SetCheckEnabled turns the periodic check on or off. On is the default.
//
// Switching it off also withdraws any update already found: the box offering
// it would otherwise stay on screen, offering exactly what the user just said
// they did not want to hear about. An install already running is not touched —
// it has been asked for, and abandoning it half way is worse than finishing.
func (m *Manager) SetCheckEnabled(on bool) {
	if m.checksOff.Swap(!on) == !on {
		return // no change
	}
	if on {
		go m.checkWithTimeout()
		return
	}
	m.mu.RLock()
	inProgress := m.state.InProgress
	m.mu.RUnlock()
	if !inProgress {
		m.setNoUpdate()
	}
}

// CheckEnabled reports whether the periodic check is running.
func (m *Manager) CheckEnabled() bool { return !m.checksOff.Load() }

func newManager(source releaseSource, apply applyPreparer, opts Options) *Manager {
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = defaultCheckInterval
	}
	if opts.CheckTimeout <= 0 {
		opts.CheckTimeout = defaultCheckTimeout
	}
	if opts.InstallTimeout <= 0 {
		opts.InstallTimeout = defaultInstallTimeout
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		source: source,
		apply:  apply,
		opts:   opts,
		log:    opts.Logger,
		state: State{
			InstalledVersion: opts.CurrentVersion,
			LatestVersion:    opts.CurrentVersion,
			Title:            softwareTitle,
		},
		listeners: map[uint64]func(State){},
		ctx:       ctx,
		cancel:    cancel,
	}
}

// State returns a snapshot that callers may keep.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneState(m.state)
}

func cloneState(state State) State {
	if state.UpdatePercentage != nil {
		percentage := *state.UpdatePercentage
		state.UpdatePercentage = &percentage
	}
	return state
}

// Subscribe observes state changes and immediately receives the current state.
// The returned function must be called when the subscriber is discarded.
func (m *Manager) Subscribe(fn func(State)) func() {
	if fn == nil {
		return func() {}
	}

	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.listeners[id] = fn
	state := cloneState(m.state)
	m.mu.Unlock()

	fn(state)
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.listeners, id)
			m.mu.Unlock()
		})
	}
}

func (m *Manager) notify() {
	m.mu.RLock()
	state := cloneState(m.state)
	listeners := make([]func(State), 0, len(m.listeners))
	for _, fn := range m.listeners {
		listeners = append(listeners, fn)
	}
	m.mu.RUnlock()

	for _, fn := range listeners {
		fn(state)
	}
}

// Check refreshes the newest installable release. Checks are serialized so a
// manual check and the background ticker cannot race provider state.
func (m *Manager) Check(ctx context.Context) error {
	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	release, found, err := m.source.Latest(ctx)
	if err != nil {
		return m.checkFailed(fmt.Errorf("check for updates: %w", err))
	}
	if !found {
		m.setNoUpdate()
		return nil
	}

	current, err := semver.NewVersion(m.opts.CurrentVersion)
	if err != nil {
		return m.checkFailed(fmt.Errorf("parse installed version %q: %w", m.opts.CurrentVersion, err))
	}
	latest, err := semver.NewVersion(release.Version)
	if err != nil {
		return m.checkFailed(fmt.Errorf("parse release version %q: %w", release.Version, err))
	}
	if !latest.GreaterThan(current) {
		m.setNoUpdate()
		return nil
	}
	if release.Size <= 0 || release.Size > maxReleaseSize {
		return m.checkFailed(fmt.Errorf("release asset size %d is outside the allowed range", release.Size))
	}

	m.mu.Lock()
	m.available = &release
	m.state.LatestVersion = release.Version
	m.state.Title = softwareTitle
	m.state.ReleaseSummary = summarize(release.Notes)
	m.state.ReleaseURL = release.URL
	m.state.LastError = ""
	m.mu.Unlock()
	m.notify()
	m.log.Info("update available", "installed", m.opts.CurrentVersion, "latest", release.Version)
	return nil
}

func (m *Manager) checkFailed(err error) error {
	m.mu.Lock()
	m.state.LastError = err.Error()
	m.mu.Unlock()
	m.notify()
	m.log.Warn("update check failed", "error", err)
	return err
}

func (m *Manager) setNoUpdate() {
	m.mu.Lock()
	m.available = nil
	m.state.LatestVersion = m.opts.CurrentVersion
	m.state.Title = softwareTitle
	m.state.ReleaseSummary = ""
	m.state.ReleaseURL = ""
	m.state.LastError = ""
	m.mu.Unlock()
	m.notify()
}

// summarize turns the release notes into the one paragraph Home Assistant has
// room for: what changed, and nothing about how to install it.
func summarize(notes string) string {
	if cut := strings.Index(notes, installSection); cut >= 0 {
		notes = notes[:cut]
	}
	text := strings.Join(strings.Fields(notes), " ")
	if utf8.RuneCountInString(text) <= maxReleaseSummary {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxReleaseSummary-1])) + "…"
}

// RequestInstall starts installing the release selected by the last successful
// check. It returns immediately so an MQTT callback never blocks shutdown.
func (m *Manager) RequestInstall() error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return errors.New("updater is stopped")
	}
	if m.state.InProgress {
		m.mu.Unlock()
		return ErrBusy
	}
	if m.available == nil {
		m.mu.Unlock()
		return ErrNoUpdate
	}
	release := *m.available
	m.state.InProgress = true
	m.state.UpdatePercentage = nil
	m.state.LastError = ""
	// Add while holding the same lock Stop uses to close the lifecycle. That
	// makes a concurrent install request either fully owned by the manager or
	// rejected as stopped; Wait must never race a fresh Add.
	m.wg.Add(1)
	m.mu.Unlock()
	m.notify()

	go func() {
		defer m.wg.Done()
		m.install(release)
	}()
	return nil
}

func (m *Manager) install(release Release) {
	// Preparing the Windows transaction starts a helper that waits for this
	// process to exit. Refuse the operation before downloading or launching it
	// when the owning application cannot request that shutdown.
	if m.opts.RequestRestart == nil {
		m.installFailed(errors.New("prepare update restart: shutdown callback is unavailable"))
		return
	}

	ctx, cancel := context.WithTimeout(m.ctx, m.opts.InstallTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp(m.opts.TempRoot, "rig-exporter-update-")
	if err != nil {
		m.installFailed(fmt.Errorf("create update staging directory: %w", err))
		return
	}
	defer os.RemoveAll(tempDir)

	stagedPath := filepath.Join(tempDir, "rig-exporter.exe")
	if err := m.source.Stage(ctx, release, stagedPath, m.reportProgress); err != nil {
		m.installFailed(fmt.Errorf("download and verify update %s: %w", release.Version, err))
		return
	}
	digest, err := fileSHA256(stagedPath)
	if err != nil {
		m.installFailed(fmt.Errorf("hash verified update %s: %w", release.Version, err))
		return
	}
	if err := m.apply.Prepare(PreparedUpdate{
		StagedPath:     stagedPath,
		ExecutablePath: m.opts.ExecutablePath,
		ConfigPath:     m.opts.ConfigPath,
		Version:        release.Version,
		SHA256:         digest,
	}); err != nil {
		m.installFailed(fmt.Errorf("prepare update restart: %w", err))
		return
	}
	// The download is over, so the percentage goes with it. The restart below
	// usually ends the process before anybody reads the state again — but
	// "usually" is not "always": RequestRestart can be declined, and a bar left
	// standing at 100 would claim an install is still running.
	m.mu.Lock()
	m.state.UpdatePercentage = nil
	m.mu.Unlock()
	m.notify()

	m.log.Info("update prepared; restarting", "version", release.Version)
	m.opts.RequestRestart()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// reportProgress records how much of the update has been downloaded.
//
// Only while an install is actually running. The source calls this from its own
// goroutine, and a late call after the install finished would otherwise leave a
// bar standing at some percentage with nothing behind it — which is exactly the
// claim this field is supposed to stop making.
func (m *Manager) reportProgress(percent float64) {
	percent = min(max(percent, 0), 100)

	m.mu.Lock()
	if !m.state.InProgress {
		m.mu.Unlock()
		return
	}
	m.state.UpdatePercentage = &percent
	m.mu.Unlock()

	m.notify()
}

func (m *Manager) installFailed(err error) {
	m.mu.Lock()
	m.state.InProgress = false
	m.state.UpdatePercentage = nil
	m.state.LastError = err.Error()
	m.mu.Unlock()
	m.notify()
	m.log.Error("update failed", "error", err)
}

// Start performs one non-blocking check and then checks periodically.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.started || m.stopped {
		m.mu.Unlock()
		return
	}
	m.started = true
	// See RequestInstall: pairing Add with the lifecycle lock prevents Stop
	// from reaching Wait while this goroutine is only half-started.
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		m.runChecks()
	}()
}

func (m *Manager) runChecks() {
	m.checkWithTimeout()
	ticker := time.NewTicker(m.opts.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkWithTimeout()
		}
	}
}

func (m *Manager) checkWithTimeout() {
	if m.checksOff.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.opts.CheckTimeout)
	defer cancel()
	_ = m.Check(ctx)
}

// Stop cancels network and staging work and waits for updater goroutines.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	m.mu.Unlock()
	m.cancel()
	m.wg.Wait()
}

var _ Controller = (*Manager)(nil)
