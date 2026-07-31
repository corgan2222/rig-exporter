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
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan/rig-exporter/internal/metrics"
)

// Source collects the network group.
type Source struct {
	// allAdapters reports every connected interface instead of only the one
	// carrying the default route.
	allAdapters bool

	mu     sync.Mutex
	last   map[uint64]counters
	lastAt time.Time

	pinger *Pinger
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

// New builds the network source. pinger may be nil, which leaves out the
// latency readings.
//
// allAdapters turns off the filter that normally reduces the list to the
// interface actually carrying traffic to the internet. A machine with Hyper-V,
// WSL, Tailscale and a capture driver installed easily has a dozen interfaces,
// and entities for all of them would bury the one that matters.
func New(pinger *Pinger, allAdapters bool) *Source {
	return &Source{last: map[uint64]counters{}, pinger: pinger, allAdapters: allAdapters}
}

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupNet }

// Collect appends one reading set per connected adapter, plus the latency
// probe's most recent result.
func (s *Source) Collect(set *metrics.Set) error {
	if s.pinger != nil {
		s.addPing(set)
	}

	adapters, err := activeAdapters(s.allAdapters)
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
	result, ok := s.pinger.Result()
	if !ok {
		return
	}
	set.Add(metrics.Text(metrics.PingTarget, "", result.Target))

	// A round that never got off the ground says nothing about the network,
	// so it must not be reported as zero loss.
	if result.Sent == 0 {
		return
	}
	set.Add(metrics.Gauge(metrics.PingLoss, "", result.LossPercent))
	if result.Received > 0 {
		set.Add(metrics.Gauge(metrics.PingRTT, "", result.AverageMs))
	}
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
func (a Adapter) Description() string {
	if a.IP == "" {
		return a.Kind
	}
	return a.Kind + " · " + a.IP
}

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
func activeAdapters(all bool) ([]Adapter, error) {
	adapters, err := allActiveAdapters()
	if err != nil || all {
		return adapters, err
	}

	primary, err := defaultRouteLUID()
	if err != nil {
		// Without a default route there is nothing better to pick, so fall
		// back to reporting everything rather than nothing.
		return adapters, nil
	}
	for _, adapter := range adapters {
		if adapter.LUID == primary {
			return []Adapter{adapter}, nil
		}
	}
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
			LinkMbit: float64(entry.ReceiveLinkSpeed) / 1_000_000,
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

func adapterKind(ifType uint32) string {
	switch ifType {
	case ifTypeEthernet:
		return "Ethernet"
	case ifTypeWifi:
		return "WLAN"
	default:
		return "Sonstige"
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

	// AF_UNSPEC asks for every address family.
	status, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&table)))
	if status != 0 || table == nil {
		return nil, fmt.Errorf("GetIfTable2 returned %d", status)
	}
	defer procFreeMibTable.Call(uintptr(unsafe.Pointer(table)))

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
