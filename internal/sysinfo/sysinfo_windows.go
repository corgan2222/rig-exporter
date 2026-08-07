//go:build windows

// Package sysinfo reports the machine-level metrics that go alongside the FPS
// reading: CPU load, memory load and the current display mode.
package sysinfo

import (
	"sync"

	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// Provider implements the system half of collector.Sources. It keeps the CPU
// counters between calls, so it must be reused rather than recreated.
type Provider struct {
	mu       sync.Mutex
	haveLast bool
	lastIdle uint64
	lastKern uint64
	lastUser uint64

	// The self counters keep a baseline of their own rather than sharing the
	// one above. SelfUsage is only read when its group is on, and a baseline
	// that exists only sometimes would make CPUPercent depend on a setting that
	// has nothing to do with it.
	haveLastSelf bool
	lastSelfCPU  uint64
	lastTotalCPU uint64

	// The firmware identity cannot change under a running process, so it is
	// read once rather than on every collection.
	hypervisorOnce sync.Once
	hypervisor     string
}

// New returns a Provider with no CPU baseline yet.
func New() *Provider { return &Provider{} }

// CPUPercent is the system-wide CPU utilisation since the previous call.
//
// The very first call has nothing to compare against and reports 0. That is
// deliberate: an absolute-since-boot number would be misleading, and the
// collector polls again a couple of seconds later anyway.
func (p *Provider) CPUPercent() (float64, error) {
	idle, kernel, user, err := winapi.SystemTimes()
	if err != nil {
		return 0, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.haveLast {
		p.haveLast, p.lastIdle, p.lastKern, p.lastUser = true, idle, kernel, user
		return 0, nil
	}

	dIdle := idle - p.lastIdle
	dKernel := kernel - p.lastKern
	dUser := user - p.lastUser
	p.lastIdle, p.lastKern, p.lastUser = idle, kernel, user

	// Kernel time already contains idle time, so total is kernel+user.
	total := dKernel + dUser
	if total == 0 {
		return 0, nil
	}
	busy := float64(total-dIdle) / float64(total) * 100
	return clamp(busy, 0, 100), nil
}

// SelfUsage is what this process costs the machine since the previous call.
//
// The share is taken against the machine's own CPU time over the same window
// rather than against wall clock times a core count. Both give the same answer,
// but this one needs neither the number of cores nor a clock, and it is the
// same denominator Windows uses for the figure in Task Manager.
//
// Like CPUPercent, the first call has nothing to compare against and reports
// zero for the processor. Memory is an instantaneous figure and is right away.
func (p *Provider) SelfUsage() (SelfUsage, error) {
	self, err := winapi.ProcessCPUTime()
	if err != nil {
		return SelfUsage{}, err
	}
	_, kernel, user, err := winapi.SystemTimes()
	if err != nil {
		return SelfUsage{}, err
	}
	bytes, err := winapi.ProcessWorkingSetBytes()
	if err != nil {
		return SelfUsage{}, err
	}

	const mb = 1024 * 1024
	usage := SelfUsage{MemoryMB: float64(bytes) / mb}

	p.mu.Lock()
	defer p.mu.Unlock()

	total := kernel + user
	if !p.haveLastSelf {
		p.haveLastSelf, p.lastSelfCPU, p.lastTotalCPU = true, self, total
		return usage, nil
	}

	dSelf := self - p.lastSelfCPU
	dTotal := total - p.lastTotalCPU
	p.lastSelfCPU, p.lastTotalCPU = self, total

	if dTotal == 0 {
		return usage, nil
	}
	usage.CPUPercent = clamp(float64(dSelf)/float64(dTotal)*100, 0, 100)
	return usage, nil
}

// Memory reports physical memory usage.
func (p *Provider) Memory() (Memory, error) {
	load, totalBytes, availBytes, err := winapi.MemoryStatus()
	if err != nil {
		return Memory{}, err
	}
	const mb = 1024 * 1024
	return Memory{
		UsedPercent: float64(load),
		TotalMB:     totalBytes / mb,
		AvailableMB: availBytes / mb,
		UsedMB:      (totalBytes - availBytes) / mb,
	}, nil
}

// Display reports the primary monitor's active mode.
func (p *Provider) Display() (Display, error) {
	w, h, hz, err := winapi.DisplayMode()
	if err != nil {
		return Display{}, err
	}
	return Display{Width: w, Height: h, RefreshHz: hz}, nil
}

// ForegroundPID is the process id behind the focused window.
func (p *Provider) ForegroundPID() uint32 { return winapi.ForegroundPID() }

// TickCount is the millisecond tick RTSS timestamps are measured against.
func (p *Provider) TickCount() uint32 { return winapi.TickCount() }

// IdleSeconds is how long the user has not touched keyboard or mouse.
func (p *Provider) IdleSeconds() float64 { return winapi.IdleSeconds() }

// UptimeHours is how long the machine has been running.
func (p *Provider) UptimeHours() float64 { return winapi.UptimeHours() }

// WindowsVersion describes the operating system, e.g. "Windows 10 Pro 22H2
// (19045.7548)". Empty when it could not be determined.
func (p *Provider) WindowsVersion() string { return winapi.WindowsVersion() }

// ProcessCount is how many processes are running.
func (p *Provider) ProcessCount() (int, error) { return winapi.ProcessCount() }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
