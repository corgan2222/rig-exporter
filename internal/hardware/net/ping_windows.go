//go:build windows

package net

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PingResult is one completed round of echoes.
type PingResult struct {
	Target      string
	Sent        int
	Received    int
	AverageMs   float64
	MinMs       float64
	MaxMs       float64
	LossPercent float64
	At          time.Time
	// Err explains why nothing came back, when nothing did.
	Err string
}

// Pinger measures latency and packet loss on its own schedule.
//
// It has to be separate from the collection loop: a round of three echoes
// against an unreachable host takes three seconds, which would stall a
// two-second collection interval every time the network hiccups.
type Pinger struct {
	target   string
	count    int
	interval time.Duration
	log      *slog.Logger

	mu     sync.RWMutex
	result PingResult
	have   bool

	stop chan struct{}
	done chan struct{}
	once sync.Once
	// started tells Stop whether there is a goroutine to wait for. A pinger
	// that was built but never started — a configuration change before the
	// app is running does exactly that — would otherwise wait forever on a
	// channel nobody is going to close.
	started atomic.Bool
}

// NewPinger builds a pinger. An empty target means the default gateway, which
// is resolved fresh on every round so a changed network is picked up.
func NewPinger(target string, count int, interval time.Duration, log *slog.Logger) *Pinger {
	if count <= 0 {
		count = 3
	}
	if interval < time.Second {
		interval = 15 * time.Second
	}
	return &Pinger{
		target:   target,
		count:    count,
		interval: interval,
		log:      log,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start begins probing. Calling it twice is harmless.
func (p *Pinger) Start() {
	if p.started.CompareAndSwap(false, true) {
		go p.run()
	}
}

// Stop ends probing, and returns immediately if it never began.
func (p *Pinger) Stop() {
	p.once.Do(func() {
		close(p.stop)
		if p.started.Load() {
			<-p.done
		}
	})
}

// Result is the most recent round, and whether one has completed.
func (p *Pinger) Result() (PingResult, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.result, p.have
}

func (p *Pinger) run() {
	defer close(p.done)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.probe()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.probe()
		}
	}
}

func (p *Pinger) probe() {
	target := p.target
	if target == "" {
		gateway, err := defaultGateway()
		if err != nil {
			p.record(PingResult{Target: "gateway", At: time.Now(), Err: err.Error()})
			return
		}
		target = gateway
	}

	address, err := resolve(target)
	if err != nil {
		p.record(PingResult{Target: target, At: time.Now(), Err: err.Error()})
		return
	}

	result := PingResult{Target: target, Sent: p.count, At: time.Now()}
	for i := 0; i < p.count; i++ {
		select {
		case <-p.stop:
			return
		default:
		}

		rtt, err := echo(address)
		if err != nil {
			result.Err = err.Error()
			continue
		}
		result.Received++
		result.AverageMs += rtt
		if result.MinMs == 0 || rtt < result.MinMs {
			result.MinMs = rtt
		}
		if rtt > result.MaxMs {
			result.MaxMs = rtt
		}
	}

	if result.Received > 0 {
		result.AverageMs /= float64(result.Received)
		result.Err = ""
	}
	result.LossPercent = float64(result.Sent-result.Received) / float64(result.Sent) * 100
	p.record(result)
}

func (p *Pinger) record(result PingResult) {
	p.mu.Lock()
	p.result = result
	p.have = true
	p.mu.Unlock()

	if result.Err != "" {
		p.log.Debug("ping failed", "target", result.Target, "error", result.Err)
	}
}

// resolve turns a host name or literal into an IPv4 address. ICMP here is
// IPv4 only, which is what a gateway or a public resolver answers on.
func resolve(target string) (uint32, error) {
	ips, err := net.LookupIP(target)
	if err != nil {
		return 0, fmt.Errorf("resolve %s: %w", target, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return uint32(v4[0]) | uint32(v4[1])<<8 | uint32(v4[2])<<16 | uint32(v4[3])<<24, nil
		}
	}
	return 0, fmt.Errorf("%s has no IPv4 address", target)
}

var (
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = iphlpapi.NewProc("IcmpSendEcho")
)

// icmpEchoReply mirrors ICMP_ECHO_REPLY.
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       icmpOptions
}

// icmpOptions mirrors IP_OPTION_INFORMATION.
type icmpOptions struct {
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	_           [4]byte
	OptionsData uintptr
}

const (
	echoPayloadSize = 32
	echoTimeoutMs   = 1000
)

// echo sends one ICMP echo and returns the round trip time in milliseconds.
//
// IcmpSendEcho needs no elevation and no raw socket, which is why it is used
// rather than assembling ICMP packets by hand.
func echo(address uint32) (float64, error) {
	handle, _, err := procIcmpCreateFile.Call()
	if handle == uintptr(windows.InvalidHandle) {
		return 0, fmt.Errorf("IcmpCreateFile: %w", err)
	}
	defer procIcmpCloseHandle.Call(handle)

	payload := make([]byte, echoPayloadSize)
	// The reply buffer holds the reply structure plus the echoed payload, and
	// Windows wants room for an error message on top.
	replyBuf := make([]byte, unsafe.Sizeof(icmpEchoReply{})+echoPayloadSize+8)

	replies, _, callErr := procIcmpSendEcho.Call(
		handle,
		uintptr(address),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(echoPayloadSize),
		0,
		uintptr(unsafe.Pointer(&replyBuf[0])),
		uintptr(len(replyBuf)),
		echoTimeoutMs,
	)
	if replies == 0 {
		return 0, fmt.Errorf("no reply: %w", callErr)
	}

	reply := (*icmpEchoReply)(unsafe.Pointer(&replyBuf[0]))
	if reply.Status != 0 {
		return 0, fmt.Errorf("icmp status %d", reply.Status)
	}
	return float64(reply.RoundTripTime), nil
}

// mibIPForwardRow2 mirrors the head of MIB_IPFORWARD_ROW2, which is all that
// is needed to read the next hop of the best route.
type sockaddrInet struct {
	Family uint16
	Port   uint16
	Addr   [4]byte
	Zero   [20]byte
}

// The field order and padding here have to match MIB_IPFORWARD_ROW2 exactly
// as far as NextHop, or the gateway is read out of the wrong bytes. There is
// deliberately no padding after InterfaceIndex: IP_ADDRESS_PREFIX aligns to
// four, so DestinationPrefix begins immediately at offset 12.
type mibIPForwardRow2 struct {
	InterfaceLUID     uint64
	InterfaceIndex    uint32
	DestinationPrefix ipAddressPrefix
	NextHop           sockaddrInet
	// The rest of the structure is not read here, but the buffer must still
	// be large enough for the whole thing.
	_ [128]byte
}

// ipAddressPrefix mirrors IP_ADDRESS_PREFIX: a socket address plus the prefix
// length, padded out to 32 bytes.
type ipAddressPrefix struct {
	Prefix       sockaddrInet
	PrefixLength uint8
	_            [3]byte
}

// bestRoute asks the routing table which interface and next hop the machine
// would use to reach a public address.
func bestRoute() (mibIPForwardRow2, error) {
	destination := sockaddrInet{Family: windows.AF_INET, Addr: [4]byte{1, 1, 1, 1}}
	var source sockaddrInet
	var route mibIPForwardRow2

	status, _, _ := procGetBestRoute2.Call(
		0, 0,
		0,
		uintptr(unsafe.Pointer(&destination)),
		0,
		uintptr(unsafe.Pointer(&route)),
		uintptr(unsafe.Pointer(&source)),
	)
	if status != 0 {
		return mibIPForwardRow2{}, fmt.Errorf("GetBestRoute2 returned %d", status)
	}
	return route, nil
}

// defaultGateway is the next hop toward the internet, which is the gateway
// currently in use.
func defaultGateway() (string, error) {
	route, err := bestRoute()
	if err != nil {
		return "", err
	}

	hop := route.NextHop.Addr
	if hop == [4]byte{} {
		return "", fmt.Errorf("no default gateway")
	}
	return fmt.Sprintf("%d.%d.%d.%d", hop[0], hop[1], hop[2], hop[3]), nil
}

// defaultRouteLUID identifies the interface the default route goes out of.
func defaultRouteLUID() (uint64, error) {
	route, err := bestRoute()
	if err != nil {
		return 0, err
	}
	if route.InterfaceLUID == 0 {
		return 0, fmt.Errorf("the default route names no interface")
	}
	return route.InterfaceLUID, nil
}
