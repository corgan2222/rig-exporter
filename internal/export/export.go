// Package export defines the contract every output target implements.
//
// A target is anything that can receive a snapshot: the MQTT publisher, the
// HTTP data server Home Assistant or Prometheus pull from, and the InfluxDB
// writer. The app treats them uniformly, so switching one on or off is a
// configuration change rather than a code path.
package export

import (
	"sync/atomic"

	"github.com/corgan2222/rig-exporter/internal/collector"
)

// Status is what the tray and the settings page show for one target.
type Status struct {
	// Name identifies the target, e.g. "mqtt".
	Name string
	// Label is the human-readable name.
	Label string
	// Healthy is false when the target is configured but not working.
	Healthy bool
	// Detail explains the state: an endpoint URL when healthy, the error
	// otherwise.
	Detail string
	// Delivered counts successful exports since Start.
	Delivered uint64
}

// Target receives snapshots.
//
// Export must not block for long: it is called once per collection interval,
// and targets that talk to the network are expected to hand the work to a
// background goroutine.
type Target interface {
	Status() Status
	Start() error
	Export(collector.Snapshot) error
	Stop()
}

// Counter is a small helper targets embed to track successful deliveries.
type Counter struct {
	n atomic.Uint64
}

// Inc records one successful export.
func (c *Counter) Inc() { c.n.Add(1) }

// Count returns the number of successful exports.
func (c *Counter) Count() uint64 { return c.n.Load() }
