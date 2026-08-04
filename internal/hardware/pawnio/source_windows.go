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

// Source reads what only a kernel driver can reach.
//
// It fills existing catalogue entries rather than inventing its own, which is
// the whole point: a processor temperature has to look identical whether it
// came from here or from MSI Afterburner. Same identifier, same unit, same
// precision — where the number came from is this program's business.
type Source struct {
	log   *slog.Logger
	store *ModuleStore
	model string

	once     sync.Once
	executor *Executor
	initErr  error

	// The energy counter only counts up, so watts are the difference between
	// two readings over the time between them.
	lastEnergy   uint64
	lastEnergyAt time.Time
	energyUnit   float64
}

// NewSource prepares the source. Nothing is opened until the first collection:
// startup must not wait on a download.
func NewSource(store *ModuleStore, log *slog.Logger) *Source {
	return &Source{store: store, model: processorBrand(), log: log}
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
func (s *Source) Collect(set *metrics.Set) error {
	s.once.Do(s.start)
	if s.initErr != nil {
		return s.initErr
	}

	if celsius, ok := s.temperature(); ok {
		set.Add(metrics.Gauge(metrics.CPUTemperature, "", celsius))
	}
	if watts, ok := s.packageWatts(); ok {
		set.Add(metrics.Gauge(metrics.CPUPower, "", watts))
	}
	return nil
}

// start opens the device and loads the module, once.
func (s *Source) start() {
	if !isAMD(s.model) {
		// Intel needs a different module and a per-microarchitecture table for
		// the temperature target. Reporting nothing is the honest answer until
		// that exists, rather than reading a register meant for another vendor.
		s.initErr = fmt.Errorf("pawnio: only AMD processors are supported so far")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	module, err := s.store.Load(ctx, amdModule)
	if err != nil {
		s.initErr = err
		return
	}

	executor, err := NewExecutor(module)
	if err != nil {
		s.initErr = err
		return
	}
	s.executor = executor
	s.log.Info("pawnio ready", "module", amdModule, "model", s.model)

	// The energy scale never changes, so it is read once.
	if raw, err := executor.ReadMSR(MSRPowerUnit); err == nil {
		if unit, ok := DecodeEnergyUnit(raw); ok {
			s.energyUnit = unit
		}
	}
}

// Close releases the device.
func (s *Source) Close() {
	if s.executor != nil {
		s.executor.Close()
	}
}

func (s *Source) temperature() (float64, bool) {
	raw, err := s.executor.ReadSMN(smnTemperature)
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
func (s *Source) packageWatts() (float64, bool) {
	if s.energyUnit == 0 {
		return 0, false
	}

	raw, err := s.executor.ReadMSR(MSRPackageEnergy)
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
	return PackageWatts(previous, raw, s.energyUnit, now.Sub(previousAt).Seconds())
}

func isAMD(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "amd") || strings.Contains(lower, "ryzen") ||
		strings.Contains(lower, "threadripper") || strings.Contains(lower, "epyc")
}
