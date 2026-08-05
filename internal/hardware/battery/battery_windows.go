//go:build windows

// Package battery reports the state and the health of a laptop's battery pack.
//
// Two Windows routes feed it, and they answer different questions. The power
// information calls say how the pack is doing right now — how full, on mains
// or not, charging or draining, how long it will last. The battery device
// itself, reached through SetupAPI and the battery IOCTLs, says what the pack
// is: how large it was when new, how many cycles it has been through, what it
// is made of. Only the second route can say anything about wear, and only the
// first is cheap enough to ask every couple of seconds.
//
// Neither needs elevation, WMI or a third-party driver, and a machine without
// a battery reports nothing at all rather than a row of zeroes.
package battery

import (
	"log/slog"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// deviceInterval is how often the battery's own facts are re-read. They move
// in months, so anything shorter would be asking a device driver a question it
// has already answered. Re-read at all rather than read once, because a pack
// can be swapped and a machine can be docked.
const deviceInterval = 5 * time.Minute

// Source contributes the battery group.
type Source struct {
	log *slog.Logger

	// The device facts, and when they were last fetched. Collect runs on the
	// collector goroutine alone, so no lock is needed here.
	devices []winapi.BatteryInfo
	readAt  time.Time
}

// New creates the battery source.
func New(log *slog.Logger) *Source {
	if log == nil {
		log = slog.Default()
	}
	return &Source{log: log}
}

// Group identifies which switch turns this source off.
func (s *Source) Group() metrics.Group { return metrics.GroupBattery }

// Collect adds whatever the machine's battery has to say.
func (s *Source) Collect(set *metrics.Set) error {
	state, err := winapi.Battery()
	if err != nil {
		return err
	}
	if !state.Present {
		// A desktop, or a laptop with the pack removed. Nothing is reported:
		// an entity claiming nought percent would be a worse answer than no
		// entity, and Home Assistant leaves the old ones alone.
		s.devices, s.readAt = nil, time.Time{}
		return nil
	}

	s.refreshDevices()
	pack := aggregate(s.devices)

	s.addLive(set, state, pack.relative)
	s.addHealth(set, pack)
	return nil
}

// addLive publishes the readings that change from one collection to the next.
//
// relative marks a controller that counts in units of its own rather than in
// milliwatt-hours. The percentage and the flags survive that; every figure
// derived from a capacity does not, and is left out rather than published in
// units nobody can name.
func (s *Source) addLive(set *metrics.Set, state winapi.BatteryState, relative bool) {
	if state.ChargeKnown {
		set.Add(metrics.Gauge(metrics.BatteryCharge, "", state.ChargePercent))
	}
	set.Add(
		metrics.Bool(metrics.BatteryCharging, "", state.Charging),
		metrics.Bool(metrics.BatteryAC, "", state.OnAC),
	)
	if state.RuntimeKnown {
		set.Add(metrics.Gauge(metrics.BatteryRuntime, "", float64(state.RuntimeSeconds)/60))
	}
	if relative {
		return
	}
	if state.RemainingMWh > 0 {
		set.Add(metrics.Gauge(metrics.BatteryRemaining, "", float64(state.RemainingMWh)/1000))
	}
	if state.FullMWh > 0 {
		set.Add(metrics.Gauge(metrics.BatteryCapacityFull, "", float64(state.FullMWh)/1000))
	}
	if state.RateKnown {
		set.Add(metrics.Gauge(metrics.BatteryPower, "", float64(state.RateMW)/1000))
	}
}

// addHealth publishes what the battery controller says about itself.
func (s *Source) addHealth(set *metrics.Set, pack packInfo) {
	// The ratio holds even when the capacities are in the controller's own
	// units, because both sides are in the same ones.
	if pack.designedMWh > 0 && pack.fullMWh > 0 {
		health := float64(pack.fullMWh) / float64(pack.designedMWh) * 100
		set.Add(metrics.Gauge(metrics.BatteryHealth, "", health))
	}
	if pack.cycles > 0 {
		set.Add(metrics.Gauge(metrics.BatteryCycles, "", float64(pack.cycles)))
	}
	if pack.chemistry != "" {
		set.Add(metrics.Text(metrics.BatteryChemistry, "", pack.chemistry))
	}
	if pack.voltageMV > 0 {
		set.Add(metrics.Gauge(metrics.BatteryVoltage, "", float64(pack.voltageMV)/1000))
	}
	if !pack.relative && pack.designedMWh > 0 {
		set.Add(metrics.Gauge(metrics.BatteryCapacityDesign, "", float64(pack.designedMWh)/1000))
	}
}

// refreshDevices re-reads the battery devices when the interval has elapsed.
//
// A failure keeps whatever was read last rather than throwing it away: the
// device can be busy for a moment, and a cycle count that is five minutes old
// is a better answer than none.
func (s *Source) refreshDevices() {
	if time.Since(s.readAt) < deviceInterval {
		return
	}
	devices, err := winapi.BatteryDevices()
	// The attempt is stamped either way. A driver that refuses to answer
	// would otherwise be asked again on every single collection.
	s.readAt = time.Now()
	if err != nil {
		s.log.Debug("battery devices unavailable", "error", err)
		return
	}
	s.devices = devices
}

// packInfo is every battery in the machine taken as one.
type packInfo struct {
	designedMWh uint32
	fullMWh     uint32
	cycles      uint32
	chemistry   string
	voltageMV   uint32
	relative    bool
}

// aggregate folds the individual batteries into the pack a person means when
// they say "the battery".
//
// Capacities add up, because two cells hold what both of them hold. The cycle
// count does not: it is the age of a cell, and the most worn one is the answer
// that matters. Chemistry and voltage come from the first battery that reports
// them, since a machine with two different chemistries does not exist.
func aggregate(devices []winapi.BatteryInfo) packInfo {
	var pack packInfo
	for _, device := range devices {
		pack.designedMWh += device.DesignedMWh
		pack.fullMWh += device.FullMWh
		if device.CycleCount > pack.cycles {
			pack.cycles = device.CycleCount
		}
		if pack.chemistry == "" {
			pack.chemistry = device.Chemistry
		}
		if pack.voltageMV == 0 {
			pack.voltageMV = device.VoltageMV
		}
		if device.Relative {
			pack.relative = true
		}
	}
	return pack
}
