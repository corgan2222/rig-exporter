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
