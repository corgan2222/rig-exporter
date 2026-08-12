//go:build windows

package cooling

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

const (
	// reportWait is how long one read waits for a controller to say something.
	//
	// It no longer sits on the measuring loop — the reader has its own
	// goroutine — which is why it can afford to be generous rather than short.
	// The old comment claimed a wait this short "almost always finds a report
	// already there"; it did not. At 250 ms and three attempts a quiet cooler
	// used the whole per-source deadline, and the group alternated between a
	// reading and "no answer within 250ms" on every tick.
	reportWait = 500 * time.Millisecond
	// pollInterval is how often the reader asks. These devices report about
	// once a second on their own, so asking faster only produces more reads
	// that find nothing.
	pollInterval = time.Second
	// maxAge is how long the last report stays worth publishing. Beyond it the
	// readings are left out rather than repeated — a stale pump speed looks
	// exactly like a running pump.
	maxAge = 15 * time.Second
	// rescanInterval is how often the USB bus is searched again while nothing
	// has been found. Enumerating every HID device on the machine is not free,
	// and a cooler is not plugged in twice an hour.
	rescanInterval = 60 * time.Second
	// reportBuffer is one byte more than the longest report any decoder here
	// reads, so a device that sends more is truncated rather than refused.
	reportBuffer = 65
)

// controller is one open device and what it last said.
type controller struct {
	product uint16
	path    string
	reader  *winapi.HIDReader

	last   Reading
	lastAt time.Time
}

// Source contributes the cooling group.
//
// Collect runs on the collector goroutine alone, so nothing in here is
// guarded: the fields below are touched from that one goroutine and from
// Close, which the same goroutine's owner calls after it has stopped.
type Source struct {
	log *slog.Logger

	// mu guards everything below, because the reader runs on its own goroutine
	// and the tick reads what it leaves behind.
	mu          sync.Mutex
	controllers []*controller
	scannedAt   time.Time
	scanErr     error

	polling bool
	stop    chan struct{}
	done    sync.WaitGroup
}

// New creates the cooling source.
func New(log *slog.Logger) *Source {
	if log == nil {
		log = slog.Default()
	}
	return &Source{log: log, stop: make(chan struct{})}
}

// startPolling launches the reader once there is something to read.
//
// Not in New: with no cooler attached there is nothing to poll, and the scan
// that finds one runs on the tick.
func (s *Source) startPolling() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.polling || len(s.controllers) == 0 {
		return
	}
	s.polling = true
	s.done.Add(1)
	go s.poll()
}

// poll asks each controller in turn, for as long as the source lives.
//
// Paced by pollInterval rather than run flat out: a status report arrives every
// second or so, and asking faster only means more reads that time out.
func (s *Source) poll() {
	defer s.done.Done()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}

		s.mu.Lock()
		controllers := append([]*controller(nil), s.controllers...)
		s.mu.Unlock()

		for _, c := range controllers {
			// Outside the lock: this is the part that waits, and holding the
			// lock across it would hand the tick exactly the stall this change
			// exists to remove.
			reading, ok := s.read(c)

			s.mu.Lock()
			if ok {
				c.last, c.lastAt = reading, time.Now()
			}
			s.mu.Unlock()
		}
	}
}

// Group identifies which switch turns this source off.
func (s *Source) Group() metrics.Group { return metrics.GroupCooling }

// OriginName credits the controllers on the status page, so somebody can see
// which device the liquid temperature came from rather than guess.
func (s *Source) OriginName() string {
	names := make([]string, 0, len(s.controllers))
	seen := map[string]bool{}
	for _, c := range s.controllers {
		name := krakenV3Products[c.product]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, " + ")
}

// Collect reads every controller that answers.
// Collect reports the last thing each controller said. It never waits for one.
//
// The device is not asked here, because asking means waiting: a Kraken sends
// status reports when it feels like it, and a read that finds none costs the
// full reportWait of 250 ms — up to three times over. That is the whole
// per-source deadline at a 500 ms poll interval, so the cooling group spent its
// life alternating between a reading and "no answer within 250ms", and the
// dashboard tile flickered between the two.
//
// The cache was already here — last, lastAt and maxAge — it was simply never
// reached, because overrunning the deadline discards everything the source
// would have contributed. Now a background goroutine does the waiting and this
// only reads what it left behind.
func (s *Source) Collect(set *metrics.Set) error {
	s.rescan()
	s.startPolling()

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, c := range s.controllers {
		if c.lastAt.IsZero() || now.Sub(c.lastAt) > maxAge {
			// It answered once and has gone quiet. Say nothing rather than
			// repeat what it said a minute ago.
			continue
		}
		add(set, c.last)
	}

	// A scan that could not run at all is worth reporting; a machine with no
	// such cooler is not. The settings page tells the two apart by this error.
	if len(s.controllers) == 0 {
		return s.scanErr
	}
	return nil
}

// read asks one controller for its current report.
//
// A device can send several kinds of report, and only one of them is a status
// report; a few are read before giving up, because the alternative is to wait
// a whole collection for the right one to come round again.
func (s *Source) read(c *controller) (Reading, bool) {
	if c.reader == nil {
		reader, err := winapi.OpenHID(c.path)
		if err != nil {
			return Reading{}, false
		}
		c.reader = reader
	}

	buf := make([]byte, reportBuffer)
	for attempt := 0; attempt < 3; attempt++ {
		n, err := c.reader.Read(buf, reportWait)
		if err != nil {
			// A timeout is ordinary — the cooler had nothing to say. Anything
			// else means the handle is no good, usually because the device was
			// unplugged, and the next scan will find it again or not.
			if err != winapi.ErrHIDTimeout {
				s.log.Debug("cooling device stopped answering", "device", c.path, "error", err)
				c.reader.Close()
				c.reader = nil
				c.path = ""
			}
			return Reading{}, false
		}
		if reading, ok := decodeKrakenV3(c.product, buf[:n]); ok {
			return reading, true
		}
	}
	return Reading{}, false
}

// rescan looks for controllers, at first call and then only while there are
// none or one has dropped out.
func (s *Source) rescan() {
	// Under the lock throughout: the reader goroutine walks this same slice and
	// writes c.reader and c.path when a device stops answering.
	s.mu.Lock()
	defer s.mu.Unlock()

	live := s.controllers[:0]
	for _, c := range s.controllers {
		if c.path != "" {
			live = append(live, c)
		}
	}
	s.controllers = live

	if len(s.controllers) > 0 {
		return
	}
	if !s.scannedAt.IsZero() && time.Since(s.scannedAt) < rescanInterval {
		return
	}
	s.scannedAt = time.Now()

	devices, err := winapi.HIDDevices()
	s.scanErr = err
	if err != nil {
		s.log.Debug("cannot enumerate HID devices", "error", err)
		return
	}
	for _, d := range devices {
		if d.VendorID != vendorNZXT {
			continue
		}
		if _, known := krakenV3Products[d.ProductID]; !known {
			continue
		}
		s.log.Info("cooling controller found",
			"device", krakenV3Products[d.ProductID], "product", d.ProductID)
		s.controllers = append(s.controllers, &controller{product: d.ProductID, path: d.Path})
	}
}

// Close releases every open device.
func (s *Source) Close() {
	// The reader is stopped and waited for before a single handle is closed.
	// It spends most of its time inside a blocking read, holding no lock, so
	// closing underneath it would pull the handle out of a call in progress.
	// Guarded against a second Close, which a configuration change produces.
	s.mu.Lock()
	stopping := s.polling
	s.polling = false
	s.mu.Unlock()

	if stopping {
		close(s.stop)
		s.done.Wait()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.controllers {
		if c.reader != nil {
			c.reader.Close()
			c.reader = nil
		}
	}
	s.controllers = nil
}

// add turns one reading into measurements. Each figure is only added when the
// device supplied it.
func add(set *metrics.Set, r Reading) {
	if r.HasLiquid {
		set.Add(metrics.Gauge(metrics.CoolingLiquidTemperature, r.Instance, r.LiquidTemperature))
	}
	if r.HasPumpRPM {
		set.Add(metrics.Gauge(metrics.CoolingPumpSpeed, r.Instance, float64(r.PumpRPM)))
	}
	if r.HasPumpDuty {
		set.Add(metrics.Gauge(metrics.CoolingPumpDuty, r.Instance, float64(r.PumpDuty)))
	}
	if r.HasFanRPM {
		set.Add(metrics.Gauge(metrics.CoolingFanSpeed, r.Instance, float64(r.FanRPM)))
	}
	if r.HasFanDuty {
		set.Add(metrics.Gauge(metrics.CoolingFanDuty, r.Instance, float64(r.FanDuty)))
	}
	if r.Device != "" {
		set.Add(metrics.Text(metrics.CoolingDevice, r.Instance, r.Device))
	}
}
