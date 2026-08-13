//go:build windows

// Package net collects the active network adapters and, optionally, a latency
// probe.
//
// Adapter facts come from GetAdaptersAddresses and the throughput from
// GetIfTable2, differenced between collections. The ping runs on its own
// schedule in a background goroutine, because an ICMP round trip takes far too
// long to sit inside the collection loop.
package net

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// Source collects the network group.
type Source struct {
	// allAdapters reports every connected interface instead of only the one
	// carrying the default route.
	allAdapters bool

	mu     sync.Mutex
	last   map[uint64]counters
	lastAt time.Time

	// lastPrimary is the interface that carried the default route when there
	// last was one, kept so a moment without a route does not widen the list to
	// every virtual adapter on the machine. Zero until one has been seen.
	lastPrimary uint64

	// defaultRoute is the lookup, behind a variable so a test can make it fail
	// without unplugging anything.
	defaultRoute func() (uint64, error)

	pingers []*Pinger
}

// counters are the cumulative interface statistics at one instant.
type counters struct {
	inOctets    uint64
	outOctets   uint64
	inErrors    uint64
	outErrors   uint64
	inDiscards  uint64
	outDiscards uint64
}

// New builds the network source. pingers may be empty, which leaves out the
// latency readings.
//
// allAdapters turns off the filter that normally reduces the list to the
// interface actually carrying traffic to the internet. A machine with Hyper-V,
// WSL, Tailscale and a capture driver installed easily has a dozen interfaces,
// and entities for all of them would bury the one that matters.
func New(pingers []*Pinger, allAdapters bool) *Source {
	return &Source{last: map[uint64]counters{}, pingers: pingers, allAdapters: allAdapters}
}

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupNet }

// Collect appends one reading set per connected adapter, plus the latency
// probe's most recent result.
func (s *Source) Collect(set *metrics.Set) error {
	if len(s.pingers) > 0 {
		s.addPing(set)
	}

	adapters, err := s.activeAdapters()
	if err != nil {
		return err
	}

	stats, err := interfaceStats()
	if err != nil {
		// Adapter facts are still worth reporting without throughput.
		stats = map[uint64]counters{}
	}

	now := time.Now()
	s.mu.Lock()
	elapsed := now.Sub(s.lastAt).Seconds()
	previous := s.last
	s.last = stats
	s.lastAt = now
	s.mu.Unlock()

	for _, adapter := range adapters {
		instance := adapter.Name
		set.Add(metrics.Text(metrics.NetType, instance, adapter.Description()))
		if adapter.IP != "" {
			set.Add(metrics.Text(metrics.NetIP, instance, adapter.IP))
		}

		if adapter.LinkMbit > 0 {
			set.Add(metrics.Gauge(metrics.NetLinkSpeed, instance, adapter.LinkMbit))
		}
		if adapter.WifiSignal > 0 {
			set.Add(metrics.Gauge(metrics.NetWifiSignal, instance, adapter.WifiSignal))
		}

		current, ok := stats[adapter.LUID]
		if !ok {
			continue
		}

		// The totals come first because they need no interval: they are the
		// counters themselves, so the very first collection already has them
		// while the rates below still have nothing to divide by.
		//
		// 1024-based, like every other GB in this catalogue and like Windows
		// Explorer. Two meanings of the same unit inside one device page would
		// be worse than either convention.
		const bytesPerGigabyte = 1024 * 1024 * 1024
		set.Add(
			metrics.Gauge(metrics.NetRxTotal, instance, float64(current.inOctets)/bytesPerGigabyte),
			metrics.Gauge(metrics.NetTxTotal, instance, float64(current.outOctets)/bytesPerGigabyte),
		)

		before, seen := previous[adapter.LUID]
		if !seen || elapsed <= 0 {
			continue // the first collection has no interval to divide by
		}

		// Octets are bytes; a megabit is 125000 of them.
		const bytesPerMegabit = 125_000
		set.Add(
			metrics.Gauge(metrics.NetRx, instance,
				float64(delta(current.inOctets, before.inOctets))/bytesPerMegabit/elapsed),
			metrics.Gauge(metrics.NetTx, instance,
				float64(delta(current.outOctets, before.outOctets))/bytesPerMegabit/elapsed),
		)

		// Deltas are taken per counter and only then added: some drivers park
		// a nonsense value in one of them, and summing first would overflow.
		errors := float64(delta(current.inErrors, before.inErrors)+
			delta(current.outErrors, before.outErrors)) / elapsed
		discards := float64(delta(current.inDiscards, before.inDiscards)+
			delta(current.outDiscards, before.outDiscards)) / elapsed

		limit := packetRateLimit(adapter.LinkMbit)
		if errors <= limit {
			set.Add(metrics.Gauge(metrics.NetErrors, instance, errors))
		}
		if discards <= limit {
			set.Add(metrics.Gauge(metrics.NetDiscards, instance, discards))
		}
	}

	if len(adapters) == 0 {
		return fmt.Errorf("no connected adapter found")
	}
	return nil
}

func (s *Source) addPing(set *metrics.Set) {
	// An instance per target, but only once there is more than one of them. A
	// lone probe keeps the plain ping_rtt it has always had: instancing it too
	// would rename the entity on every machine that never asked for a second
	// target, and a renamed entity is an orphaned dashboard.
	instanced := len(s.pingers) > 1

	for _, pinger := range s.pingers {
		result, ok := pinger.Result()
		if !ok {
			continue
		}

		// The configured target, not the resolved one. They differ only for the
		// gateway, which cannot occur here — a gateway probe is the unconfigured
		// single target and never instanced — but the configured string is the
		// one that stays put while a network changes underneath it, and an
		// instance that moves is an entity that moves.
		instance := ""
		if instanced {
			instance = pinger.Target()
		}

		set.Add(metrics.Text(metrics.PingTarget, instance, result.Target))

		// A round that never got off the ground says nothing about the network,
		// so it must not be reported as zero loss.
		if result.Sent == 0 {
			continue
		}
		set.Add(metrics.Gauge(metrics.PingLoss, instance, result.LossPercent))
		if result.Received > 0 {
			set.Add(metrics.Gauge(metrics.PingRTT, instance, result.AverageMs))
		}
	}
}

// linkMbit converts a reported link speed into megabits per second.
//
// Written by hand for one value. Windows uses all-ones in ReceiveLinkSpeed to
// mean "speed unknown", and dividing that produces 18446744073709.55 Mbit/s —
// which slips past everything downstream because it is not too small, it is
// enormous. The measurement then claims a speed nobody reported, and
// packetRateLimit builds a ceiling of 3.6e16 packets a second on it, which
// switches off the very filter that keeps a broken driver's counters out.
//
// Zero here is not the same sin as a published zero. This is a field of an
// internal struct rather than a reading, and both readers already understand
// zero as "unknown": Collect leaves the measurement out, and packetRateLimit
// falls back to its assumed ten gigabit.
func linkMbit(raw uint64) float64 {
	if raw == math.MaxUint64 {
		return 0
	}
	return float64(raw) / 1_000_000
}

// packetRateLimit is the most packets per second a link could physically
// carry, using the smallest Ethernet frame.
//
// Error and discard counters are the ones drivers most often leave broken:
// a Realtek adapter here reports 267 trillion received discards, climbing by
// two billion a second. A rate no cable could deliver is a broken counter, and
// omitting the reading is more honest than publishing the number.
func packetRateLimit(linkMbit float64) float64 {
	if linkMbit <= 0 {
		linkMbit = 10_000 // assume 10 Gbit when the link speed is unknown
	}
	const smallestFrameBits = 64 * 8
	return linkMbit * 1e6 / smallestFrameBits
}

// delta is the increase of a cumulative counter between two samples.
//
// Windows resets these when an adapter is reconfigured, and a naive
// subtraction on unsigned counters would then report roughly four billion
// events per second. A counter that went backwards means the series restarted,
// so the honest answer for that interval is zero.
func delta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

// Adapter is one connected network interface.
type Adapter struct {
	// Name is the connection name Windows shows, e.g. "Ethernet" or "WLAN".
	Name string
	// Kind is "Ethernet", "WLAN" or "Sonstige".
	Kind     string
	IP       string
	LinkMbit float64
	LUID     uint64
	// WifiSignal is the signal quality in percent, 0 for wired adapters.
	WifiSignal float64
}

// Description is what the diagnostic entity shows.
// Description used to glue the kind and the address together as
// "Ethernet · 192.168.2.30". Two facts in one string, and neither could be
// filtered, compared or used in an automation — the same mistake the drive
// label made with its file system. They are reported separately now.
func (a Adapter) Description() string { return a.Kind }

const (
	ifTypeEthernet = 6
	ifTypeWifi     = 71

	ifOperStatusUp = 1
)

// activeAdapters lists the interfaces that are up and have an address.
//
// Unless all is set, the list is reduced to the interface carrying the default
// route. That is "the active NIC" in the sense anyone actually means it: the
// one the machine reaches the network through.
func (s *Source) activeAdapters() ([]Adapter, error) {
	adapters, err := allActiveAdapters()
	if err != nil || s.allAdapters {
		return adapters, err
	}
	return s.chooseAdapters(adapters)
}

// chooseAdapters narrows the list to the one carrying the default route.
//
// When the route cannot be found it reports the interface that carried it last,
// rather than falling back to all of them. The old comment here said there was
// "nothing better to pick"; there is, and picking everything is expensive in a
// way that is not obvious from this function.
//
// Every FriendlyName becomes an instance, and every instance gets a *retained*
// discovery message. Measured on one development machine: ten interfaces pass
// the "up, not loopback, has IPv4" filter — one physical card, six Hyper-V
// switches, Tailscale, ZeroTier and an Npcap loopback — none of which go down
// when the cable does. At ten catalogued readings each, five seconds without a
// cable published ninety entities that outlive the outage, outlive Home
// Assistant forgetting them, and come back on every restart.
//
// This does not stop orphans in general: any newly seen instance produces them,
// which is deliberate — see announceNew, where a drive unplugged for an
// afternoon must not cost anybody their history. It stops the one path that
// produces them by the dozen.
func (s *Source) chooseAdapters(adapters []Adapter) ([]Adapter, error) {
	lookup := s.defaultRoute
	if lookup == nil {
		lookup = defaultRouteLUID
	}

	primary, err := lookup()
	if err == nil {
		s.lastPrimary = primary
	} else if s.lastPrimary == 0 {
		// Nothing was ever chosen, so there is nothing to hold on to. Reporting
		// every virtual adapter here would create exactly the entities this
		// avoids, so it reports none and lets the source error say why.
		return nil, fmt.Errorf("no default route, and no adapter seen carrying one yet: %w", err)
	}

	for _, adapter := range adapters {
		if adapter.LUID == s.lastPrimary {
			return []Adapter{adapter}, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("no default route, and the last known adapter is gone: %w", err)
	}
	// The route points at an interface this list does not hold — a tunnel that
	// is up without an address of the kind collected here, for instance. The
	// full list is the honest answer then: a route exists, it just is not one of
	// these.
	return adapters, nil
}

func allActiveAdapters() ([]Adapter, error) {
	const flags = windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER

	size := uint32(16 * 1024)
	var buf []byte
	var first *windows.IpAdapterAddresses

	// The required size is only known after an attempt, and can grow between
	// attempts if an interface appears, so this retries rather than assuming.
	for attempt := 0; attempt < 4; attempt++ {
		buf = make([]byte, size)
		first = (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))

		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
		first = nil
	}
	if first == nil {
		return nil, fmt.Errorf("GetAdaptersAddresses: buffer kept growing")
	}

	var adapters []Adapter
	for entry := first; entry != nil; entry = entry.Next {
		if entry.OperStatus != ifOperStatusUp || entry.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}

		adapter := Adapter{
			Name:     strings.TrimSpace(windows.UTF16PtrToString(entry.FriendlyName)),
			Kind:     adapterKind(entry.IfType),
			IP:       firstUnicastAddress(entry),
			LinkMbit: linkMbit(entry.ReceiveLinkSpeed),
			LUID:     luidValue(entry),
		}
		if adapter.Name == "" || adapter.IP == "" {
			continue // virtual adapters without an address are noise
		}
		if entry.IfType == ifTypeWifi {
			adapter.WifiSignal = wifiSignalPercent(adapter.Name)
		}
		adapters = append(adapters, adapter)
	}
	return adapters, nil
}

// adapterKind names the link technology.
//
// These strings are published as a sensor value, so they are machine-facing and
// stay English like every other value in the export. Translating them would
// change what a Home Assistant automation compares against whenever the user
// switches language.
func adapterKind(ifType uint32) string {
	switch ifType {
	case ifTypeEthernet:
		return "Ethernet"
	case ifTypeWifi:
		return "Wi-Fi"
	default:
		return "Other"
	}
}

// firstUnicastAddress returns the adapter's first IPv4 address, which is what
// identifies it to a human reading the entity in Home Assistant.
func firstUnicastAddress(entry *windows.IpAdapterAddresses) string {
	for address := entry.FirstUnicastAddress; address != nil; address = address.Next {
		ip := address.Address.IP()
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

// luidValue reads the interface LUID, which is what joins an adapter to its
// row in the interface table.
func luidValue(entry *windows.IpAdapterAddresses) uint64 {
	return *(*uint64)(unsafe.Pointer(&entry.Luid))
}

var (
	iphlpapi          = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIfTable2   = iphlpapi.NewProc("GetIfTable2")
	procFreeMibTable  = iphlpapi.NewProc("FreeMibTable")
	procGetBestRoute2 = iphlpapi.NewProc("GetBestRoute2")
)

// mibIfRow2 mirrors MIB_IF_ROW2.
//
// Every field matters even though only the counters are read: the rows are
// walked as an array, so a single wrong field size shifts every row after the
// first and turns the statistics into noise. The size assertion below is what
// catches that.
type mibIfRow2 struct {
	InterfaceLUID               uint64
	InterfaceIndex              uint32
	InterfaceGUID               windows.GUID
	Alias                       [257]uint16
	Description                 [257]uint16
	PhysAddrLength              uint32
	PhysAddr                    [32]uint8
	PermPhysAddr                [32]uint8
	MTU                         uint32
	Type                        uint32
	TunnelType                  uint32
	MediaType                   uint32
	PhysMediumType              uint32
	AccessType                  uint32
	DirectionType               uint32
	InterfaceAndOperStatusFlags uint8
	_                           [3]uint8
	OperStatus                  uint32
	AdminStatus                 uint32
	MediaConnectState           uint32
	NetworkGUID                 windows.GUID
	ConnectionType              uint32
	_                           [4]byte // padding before the 64-bit fields
	TransmitLinkSpeed           uint64
	ReceiveLinkSpeed            uint64
	InOctets                    uint64
	InUcastPkts                 uint64
	InNUcastPkts                uint64
	InDiscards                  uint64
	InErrors                    uint64
	InUnknownProtos             uint64
	InUcastOctets               uint64
	InMulticastOctets           uint64
	InBroadcastOctets           uint64
	OutOctets                   uint64
	OutUcastPkts                uint64
	OutNUcastPkts               uint64
	OutDiscards                 uint64
	OutErrors                   uint64
	OutUcastOctets              uint64
	OutMulticastOctets          uint64
	OutBroadcastOctets          uint64
	OutQLen                     uint64
}

// MIB_IF_ROW2 is 1352 bytes on 64-bit Windows. This fails to compile if the
// Go declaration above ever drifts from that, which is the only cheap way to
// notice a field that was added, removed or mistyped.
var _ = [1]struct{}{}[unsafe.Sizeof(mibIfRow2{})-1352]

// mibIfTable2 mirrors MIB_IF_TABLE2's header.
type mibIfTable2 struct {
	NumEntries uint32
	_          [4]byte
	Table      [1]mibIfRow2
}

// interfaceStats reads the cumulative counters of every interface, keyed by
// LUID.
func interfaceStats() (map[uint64]counters, error) {
	var table *mibIfTable2

	// AF_UNSPEC asks for every address family. CallStatus because GetIfTable2
	// answers with a Win32 error code, where zero is success — the same value
	// a missing symbol would produce.
	if err := winapi.CallStatus(procGetIfTable2, uintptr(unsafe.Pointer(&table))); err != nil {
		return nil, fmt.Errorf("GetIfTable2: %w", err)
	}
	if table == nil {
		return nil, fmt.Errorf("GetIfTable2 reported success without a table")
	}
	defer func() { _, _ = winapi.Call(procFreeMibTable, uintptr(unsafe.Pointer(table))) }()

	rows := unsafe.Slice(&table.Table[0], int(table.NumEntries))
	out := make(map[uint64]counters, len(rows))
	for i := range rows {
		row := &rows[i]
		out[row.InterfaceLUID] = counters{
			inOctets:    row.InOctets,
			outOctets:   row.OutOctets,
			inErrors:    row.InErrors,
			outErrors:   row.OutErrors,
			inDiscards:  row.InDiscards,
			outDiscards: row.OutDiscards,
		}
	}
	return out, nil
}
