package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeReleaseSource struct {
	latest      Release
	found       bool
	latestErr   error
	stageErr    error
	stageStart  chan struct{}
	stageFinish chan struct{}

	mu          sync.Mutex
	staged      []Release
	stageTarget []string
}

func (s *fakeReleaseSource) Latest(context.Context) (Release, bool, error) {
	return s.latest, s.found, s.latestErr
}

func (s *fakeReleaseSource) Stage(_ context.Context, release Release, target string) error {
	if s.stageStart != nil {
		close(s.stageStart)
	}
	if s.stageFinish != nil {
		<-s.stageFinish
	}
	if s.stageErr == nil {
		if err := os.WriteFile(target, []byte("verified "+release.Version), 0o600); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.staged = append(s.staged, release)
	s.stageTarget = append(s.stageTarget, target)
	s.mu.Unlock()
	return s.stageErr
}

type fakeApplyPreparer struct {
	err   error
	plans []PreparedUpdate
}

func (p *fakeApplyPreparer) Prepare(update PreparedUpdate) error {
	p.plans = append(p.plans, update)
	return p.err
}

func newTestManager(source releaseSource, apply applyPreparer) *Manager {
	return newManager(source, apply, Options{
		CurrentVersion: "1.6.3",
		ExecutablePath: `C:\Program Files\rig-exporter\rig-exporter.exe`,
		ConfigPath:     `C:\Users\Stefan\config.json`,
		CheckInterval:  time.Hour,
	})
}

func TestProductionUpdaterRequiresAControlledRestart(t *testing.T) {
	_, err := New(Options{CurrentVersion: "1.6.3"})
	if err == nil || !strings.Contains(err.Error(), "restart callback") {
		t.Fatalf("New error = %v, want missing restart callback", err)
	}
}

func TestCheckPublishesANewerReleaseWithABriefChangelog(t *testing.T) {
	notes := strings.Repeat("Verbesserte GPU-Erkennung und MQTT-Status. ", 20)
	source := &fakeReleaseSource{found: true, latest: Release{
		Version: "1.6.4",
		Title:   "rig-exporter 1.6.4",
		Notes:   notes,
		URL:     "https://github.com/corgan2222/rig-exporter/releases/tag/v1.6.4",
		Size:    12 << 20,
	}}
	manager := newTestManager(source, &fakeApplyPreparer{})

	var observed State
	unsubscribe := manager.Subscribe(func(state State) { observed = state })
	t.Cleanup(unsubscribe)

	if err := manager.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}

	state := manager.State()
	if state.InstalledVersion != "1.6.3" || state.LatestVersion != "1.6.4" {
		t.Fatalf("versions = %q -> %q", state.InstalledVersion, state.LatestVersion)
	}
	if state.ReleaseURL != source.latest.URL || state.Title != source.latest.Title {
		t.Errorf("release metadata = %#v", state)
	}
	if len([]rune(state.ReleaseSummary)) > 255 {
		t.Errorf("release summary has %d runes, want at most 255", len([]rune(state.ReleaseSummary)))
	}
	if !strings.HasPrefix(state.ReleaseSummary, "Verbesserte GPU-Erkennung") {
		t.Errorf("release summary = %q", state.ReleaseSummary)
	}
	if observed.LatestVersion != state.LatestVersion {
		t.Errorf("subscriber saw %q, want %q", observed.LatestVersion, state.LatestVersion)
	}
}

func TestCheckNeverOffersADowngrade(t *testing.T) {
	manager := newTestManager(&fakeReleaseSource{found: true, latest: Release{
		Version: "1.6.2",
		Title:   "older",
	}}, &fakeApplyPreparer{})

	if err := manager.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	state := manager.State()
	if state.LatestVersion != state.InstalledVersion {
		t.Errorf("versions = %q -> %q, want no downgrade", state.InstalledVersion, state.LatestVersion)
	}
	if err := manager.RequestInstall(); !errors.Is(err, ErrNoUpdate) {
		t.Errorf("RequestInstall error = %v, want ErrNoUpdate", err)
	}
}

func TestInstallStagesOnlyTheCheckedReleaseAndRequestsOneRestart(t *testing.T) {
	source := &fakeReleaseSource{
		found:       true,
		latest:      Release{Version: "1.6.4", Size: 12 << 20},
		stageStart:  make(chan struct{}),
		stageFinish: make(chan struct{}),
	}
	apply := &fakeApplyPreparer{}
	manager := newTestManager(source, apply)
	manager.opts.TempRoot = t.TempDir()
	restart := make(chan struct{})
	manager.opts.RequestRestart = func() { close(restart) }
	t.Cleanup(manager.Stop)

	if err := manager.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := manager.RequestInstall(); err != nil {
		t.Fatalf("RequestInstall: %v", err)
	}
	<-source.stageStart
	if err := manager.RequestInstall(); !errors.Is(err, ErrBusy) {
		t.Errorf("second RequestInstall error = %v, want ErrBusy", err)
	}
	close(source.stageFinish)

	select {
	case <-restart:
	case <-time.After(time.Second):
		t.Fatal("restart was not requested after staging")
	}

	source.mu.Lock()
	defer source.mu.Unlock()
	if len(source.staged) != 1 || source.staged[0].Version != "1.6.4" {
		t.Fatalf("staged releases = %#v", source.staged)
	}
	if len(apply.plans) != 1 {
		t.Fatalf("prepared updates = %d, want 1", len(apply.plans))
	}
	plan := apply.plans[0]
	if plan.Version != "1.6.4" || plan.ExecutablePath != manager.opts.ExecutablePath || plan.ConfigPath != manager.opts.ConfigPath {
		t.Errorf("prepared update = %#v", plan)
	}
	if plan.StagedPath == "" || plan.StagedPath != source.stageTarget[0] {
		t.Errorf("staged path = %q / %q", plan.StagedPath, source.stageTarget[0])
	}
	wantDigest := sha256.Sum256([]byte("verified 1.6.4"))
	if plan.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Errorf("prepared SHA-256 = %q", plan.SHA256)
	}
	if !manager.State().InProgress {
		t.Error("update stopped reporting in_progress before the restart")
	}
}

func TestInstallFailureReturnsToIdleWithoutRequestingRestart(t *testing.T) {
	stageErr := errors.New("signature verification failed")
	source := &fakeReleaseSource{
		found:    true,
		latest:   Release{Version: "1.6.4", Size: 12 << 20},
		stageErr: stageErr,
	}
	apply := &fakeApplyPreparer{}
	manager := newTestManager(source, apply)
	manager.opts.TempRoot = t.TempDir()
	restarted := make(chan struct{}, 1)
	manager.opts.RequestRestart = func() { restarted <- struct{}{} }
	t.Cleanup(manager.Stop)

	failed := make(chan State, 1)
	unsubscribe := manager.Subscribe(func(state State) {
		if state.LastError != "" {
			select {
			case failed <- state:
			default:
			}
		}
	})
	t.Cleanup(unsubscribe)

	if err := manager.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := manager.RequestInstall(); err != nil {
		t.Fatalf("RequestInstall: %v", err)
	}

	select {
	case state := <-failed:
		if state.InProgress {
			t.Error("failed update still reports in_progress")
		}
		if !strings.Contains(state.LastError, stageErr.Error()) {
			t.Errorf("last error = %q, want %q", state.LastError, stageErr)
		}
	case <-time.After(time.Second):
		t.Fatal("failed update did not publish its terminal state")
	}

	if len(apply.plans) != 0 {
		t.Errorf("apply was prepared %d times after staging failed", len(apply.plans))
	}
	select {
	case <-restarted:
		t.Fatal("restart was requested after staging failed")
	default:
	}
}
