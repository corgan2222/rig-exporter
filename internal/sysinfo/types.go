package sysinfo

import "strconv"

// Memory is the physical memory picture at one instant.
type Memory struct {
	// UsedPercent is what Windows itself reports as the memory load.
	UsedPercent float64
	TotalMB     uint64
	UsedMB      uint64
	AvailableMB uint64
}

// SelfUsage is what this process itself costs the machine.
//
// Reported only with debug logging switched on: it answers "is the exporter the
// thing making the numbers move", which is a question about the tool and not
// about the PC it measures.
type SelfUsage struct {
	// CPUPercent is measured against every core together, the same denominator
	// Task Manager uses — 100 % means every core saturated, not one.
	CPUPercent float64
	MemoryMB   float64
}

// Display is the current mode of the primary monitor.
type Display struct {
	Width     int
	Height    int
	RefreshHz int
}

// String renders the mode as "2560x1440", or "unknown" before the first
// successful query.
func (d Display) String() string {
	if d.Width <= 0 || d.Height <= 0 {
		return "unknown"
	}
	return strconv.Itoa(d.Width) + "x" + strconv.Itoa(d.Height)
}
