//go:build windows

package pawnio

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"golang.org/x/sys/windows/registry"
)

// amdModule is the register primitive for every Zen generation. Despite the
// name it covers families 17h through 1Ah — AMD kept the interface.
const amdModule = "AMDFamily17.bin"

// How long to leave a failed load alone before trying again. Nobody is waiting
// on it — the measurement runs on without these readings — so the interval can
// be generous rather than eager.
const (
	initialBackoff = time.Minute
	maxBackoff     = time.Hour
)

// moduleLoader is the one method Source needs from the module store.
//
// *ModuleStore satisfies it unchanged. The seam exists so the waiting can be
// tested: the real path downloads, and a test that reaches the network is no
// test of when this source blocks.
type moduleLoader interface {
	Load(ctx context.Context, name string) ([]byte, error)
}

// Source reads what only a kernel driver can reach.
//
// It fills existing catalogue entries rather than inventing its own, which is
// the whole point: a processor temperature has to look identical whether it
// came from here or from MSI Afterburner. Same identifier, same unit, same
// precision — where the number came from is this program's business.
type Source struct {
	log   *slog.Logger
	store moduleLoader
	model string

	// The load runs on its own goroutine, so everything it writes and Collect
	// reads is behind this.
	// ctx lives as long as the source does. Close ends it, which is what takes
	// a download that is under way down with it.
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	loading   bool
	closed    bool
	executor  *Executor
	initErr   error
	permanent bool
	retryAt   time.Time
	backoff   time.Duration

	// energyUnit is written by the load and read by the tick, so it sits under
	// the lock with the executor it belongs to.
	energyUnit float64

	// The energy counter only counts up, so watts are the difference between
	// two readings over the time between them. Only the tick touches these.
	lastEnergy   uint64
	lastEnergyAt time.Time
}

// NewSource prepares the source. Nothing is opened until the first collection:
// startup must not wait on a download.
func NewSource(store *ModuleStore, log *slog.Logger) *Source {
	return newSourceWithLoader(store, log)
}

// newSourceWithLoader is the constructor behind NewSource, taking the loader as
// the interface rather than the concrete store so a test can supply its own.
func newSourceWithLoader(store moduleLoader, log *slog.Logger) *Source {
	ctx, cancel := context.WithCancel(context.Background())
	return &Source{
		store: store, model: processorBrand(), log: log,
		ctx: ctx, cancel: cancel, backoff: initialBackoff,
	}
}

// processorBrand is the model name, which is what decides both the module and
// whether an offset has to be undone.
func processorBrand() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	name, _, err := key.GetStringValue("ProcessorNameString")
	if err != nil {
		return ""
	}
	// The registry pads the model name out to a fixed width.
	return strings.Join(strings.Fields(name), " ")
}

// Group identifies this source. It reports processor readings, so it belongs
// with the processor group and is switched off with it.
func (s *Source) Group() metrics.Group { return metrics.GroupCPU }

// OriginName is what the interface shows as the supplier of these readings.
func (s *Source) OriginName() string { return "PawnIO" }

// Collect appends what PawnIO can supply.
//
// The load runs alongside rather than here: it takes up to two minutes when the
// module is not on disk yet, and this tick drives every other source with it.
// Until something is loaded this source reports nothing, which is what every
// other optional source does before it knows a value — a missing reading is
// left out, not zeroed.
func (s *Source) Collect(set *metrics.Set) error {
	executor, unit, err := s.ready()
	if executor == nil {
		return err
	}

	if celsius, ok := s.temperature(executor); ok {
		set.Add(metrics.Gauge(metrics.CPUTemperature, "", celsius))
	}
	if watts, ok := s.packageWatts(executor, unit); ok {
		set.Add(metrics.Gauge(metrics.CPUPower, "", watts))
	}
	return nil
}

// ready hands out the loaded device, starting the load if it is due.
//
// A nil executor and a nil error is the ordinary answer while the load is still
// running: there is nothing to report and nothing has gone wrong.
func (s *Source) ready() (*Executor, float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.executor != nil || s.permanent || s.closed {
		return s.executor, s.energyUnit, s.initErr
	}
	if !s.loading && !time.Now().Before(s.retryAt) {
		s.loading = true
		go s.start()
	}
	return nil, 0, s.initErr
}

// start opens the device and loads the module. It runs on its own goroutine.
func (s *Source) start() {
	if !isAMD(s.model) {
		// Intel needs a different module and a per-microarchitecture table for
		// the temperature target. Reporting nothing is the honest answer until
		// that exists, rather than reading a register meant for another vendor.
		// This is the one verdict that cannot change while the program runs.
		s.fail(fmt.Errorf("pawnio: only AMD processors are supported so far"), true)
		return
	}

	// The source's own context, so releasing the source ends the download.
	// The two minutes stay on top of it as the upper bound.
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()

	module, err := s.store.Load(ctx, amdModule)
	if err != nil {
		s.fail(err, false)
		return
	}

	executor, err := NewExecutor(module)
	if err != nil {
		s.fail(err, false)
		return
	}
	s.log.Info("pawnio ready", "module", amdModule, "model", s.model)

	// The energy scale never changes, so it is read once.
	unit := 0.0
	if raw, err := executor.ReadMSR(MSRPowerUnit); err == nil {
		if decoded, ok := DecodeEnergyUnit(raw); ok {
			unit = decoded
		}
	}
	s.succeed(executor, unit)
}

// succeed records a loaded device.
//
// A device that finished loading after the source was released is closed right
// here rather than kept: Close has already run and would never see it.
func (s *Source) succeed(executor *Executor, unit float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loading, s.initErr = false, nil
	if s.closed {
		executor.Close()
		return
	}
	s.executor, s.energyUnit = executor, unit
}

// fail records a failed attempt, and whether trying again could ever help.
//
// A network error is temporary — a captive portal, a VPN that is not up yet —
// and making it permanent means this source stays silent until the program is
// restarted, long after the network came back. Only what cannot change is
// remembered; everything else is tried again after the backoff.
func (s *Source) fail(err error, permanent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loading, s.initErr, s.permanent = false, err, permanent
	if permanent {
		return
	}
	s.retryAt = time.Now().Add(s.backoff)
	s.backoff = min(2*s.backoff, maxBackoff)
}

// Close releases the device and takes a download that is still running with it.
//
// Both callers need the second half: quitting waits on the measurement loop and
// then releases the sources, and a configuration change throws the whole set
// away. Either one used to leave a two-minute download running behind it.
func (s *Source) Close() {
	s.cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.executor != nil {
		s.executor.Close()
		s.executor = nil
	}
}

func (s *Source) temperature(executor *Executor) (float64, bool) {
	raw, err := executor.ReadSMN(smnTemperature)
	if err != nil {
		s.log.Debug("pawnio temperature read failed", "error", err)
		return 0, false
	}
	return DecodeZenTemperature(raw, s.model)
}

// packageWatts turns two readings of the energy counter into a rate.
//
// The first collection establishes the baseline and reports nothing, the same
// way every other rate in this program behaves.
func (s *Source) packageWatts(executor *Executor, unit float64) (float64, bool) {
	if unit == 0 {
		return 0, false
	}

	raw, err := executor.ReadMSR(MSRPackageEnergy)
	if err != nil {
		s.log.Debug("pawnio energy read failed", "error", err)
		return 0, false
	}
	now := time.Now()

	previous, previousAt := s.lastEnergy, s.lastEnergyAt
	s.lastEnergy, s.lastEnergyAt = raw, now

	if previousAt.IsZero() {
		return 0, false
	}
	return PackageWatts(previous, raw, unit, now.Sub(previousAt).Seconds())
}

func isAMD(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "amd") || strings.Contains(lower, "ryzen") ||
		strings.Contains(lower, "threadripper") || strings.Contains(lower, "epyc")
}
