//go:build windows

// Package disk collects one reading set per fixed volume: what kind of drive
// it is, how full it is, and how much it is being read and written.
//
// Throughput comes from IOCTL_DISK_PERFORMANCE on the volume handle. The
// handle is opened with no access rights at all, which is what lets an
// unelevated process ask a volume for its counters.
package disk

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan/rig-exporter/internal/metrics"
)

// Source collects the disk group.
type Source struct {
	// wants decides which drive letters to report; nil means all fixed ones.
	wants func(letter string) bool

	mu     sync.Mutex
	last   map[string]sample
	lastAt time.Time
}

// sample is one reading of a volume's cumulative IO counters.
type sample struct {
	bytesRead    int64
	bytesWritten int64
	idleTime     int64
}

// New builds the disk source. wants may be nil, which reports every fixed
// drive.
func New(wants func(letter string) bool) *Source {
	if wants == nil {
		wants = func(string) bool { return true }
	}
	return &Source{wants: wants, last: map[string]sample{}}
}

// Group identifies this source.
func (s *Source) Group() metrics.Group { return metrics.GroupDisk }

// Collect appends one reading set per volume.
func (s *Source) Collect(set *metrics.Set) error {
	letters, err := fixedDrives()
	if err != nil {
		return err
	}

	now := time.Now()
	s.mu.Lock()
	elapsed := now.Sub(s.lastAt).Seconds()
	previous := s.last
	current := make(map[string]sample, len(letters))
	s.lastAt = now
	s.mu.Unlock()

	found := 0
	for _, letter := range letters {
		if !s.wants(letter) {
			continue
		}
		if s.collectVolume(set, letter, previous, current, elapsed) {
			found++
		}
	}

	s.mu.Lock()
	s.last = current
	s.mu.Unlock()

	if found == 0 {
		return fmt.Errorf("no volume could be read")
	}
	return nil
}

// collectVolume appends the readings of one drive letter and reports whether
// anything was collected.
func (s *Source) collectVolume(set *metrics.Set, letter string, previous, current map[string]sample, elapsed float64) bool {
	instance := letter + ":"

	total, free, err := spaceOf(letter)
	if err != nil || total == 0 {
		return false
	}

	const gb = 1024 * 1024 * 1024
	used := total - free
	set.Add(
		metrics.Gauge(metrics.DiskTotal, instance, float64(total)/gb),
		metrics.Gauge(metrics.DiskUsed, instance, float64(used)/gb),
		metrics.Gauge(metrics.DiskFree, instance, float64(free)/gb),
		metrics.Gauge(metrics.DiskUsedPercent, instance, float64(used)/float64(total)*100),
		metrics.Gauge(metrics.DiskFreePercent, instance, float64(free)/float64(total)*100),
	)

	if label, filesystem, err := volumeInfo(letter); err == nil {
		description := filesystem
		if label != "" {
			description = label + " (" + filesystem + ")"
		}
		set.Add(metrics.Text(metrics.DiskLabel, instance, description))
	}
	if media := mediaKind(letter); media != "" {
		set.Add(metrics.Text(metrics.DiskMedia, instance, media))
	}

	now, err := performanceOf(letter)
	if err != nil {
		return true // space readings are still worth having
	}
	current[letter] = now

	before, seen := previous[letter]
	if !seen || elapsed <= 0 {
		return true // the first collection has no interval to divide by
	}

	const mb = 1024 * 1024
	set.Add(
		metrics.Gauge(metrics.DiskRead, instance, float64(delta(now.bytesRead, before.bytesRead))/mb/elapsed),
		metrics.Gauge(metrics.DiskWrite, instance, float64(delta(now.bytesWritten, before.bytesWritten))/mb/elapsed),
	)

	// IdleTime is in 100 ns units, the same as the interval once converted.
	interval := elapsed * 1e7
	if idle := float64(delta(now.idleTime, before.idleTime)); interval > 0 {
		busy := (1 - idle/interval) * 100
		if busy < 0 {
			busy = 0
		}
		if busy > 100 {
			busy = 100
		}
		set.Add(metrics.Gauge(metrics.DiskBusy, instance, busy))
	}
	return true
}

// delta is the increase of a cumulative counter between two samples. Windows
// resets these when a volume is remounted, and a counter that went backwards
// means the series restarted rather than that the disk read minus ten
// gigabytes.
func delta(current, previous int64) int64 {
	if current < previous {
		return 0
	}
	return current - previous
}

// fixedDrives lists the letters of the volumes that live in the machine,
// skipping network shares, optical drives and removable media.
func fixedDrives() ([]string, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, fmt.Errorf("GetLogicalDrives: %w", err)
	}

	var letters []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A' + i))

		root, err := windows.UTF16PtrFromString(letter + `:\`)
		if err != nil {
			continue
		}
		if windows.GetDriveType(root) == windows.DRIVE_FIXED {
			letters = append(letters, letter)
		}
	}
	if len(letters) == 0 {
		return nil, fmt.Errorf("no fixed drives found")
	}
	return letters, nil
}

func spaceOf(letter string) (total, free uint64, err error) {
	root, err := windows.UTF16PtrFromString(letter + `:\`)
	if err != nil {
		return 0, 0, err
	}
	var freeToCaller uint64
	if err := windows.GetDiskFreeSpaceEx(root, &freeToCaller, &total, &free); err != nil {
		return 0, 0, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", letter, err)
	}
	// The space available to this user is the honest "free", since a quota
	// can make it smaller than the volume-wide figure.
	return total, freeToCaller, nil
}

func volumeInfo(letter string) (label, filesystem string, err error) {
	root, err := windows.UTF16PtrFromString(letter + `:\`)
	if err != nil {
		return "", "", err
	}

	labelBuf := make([]uint16, 261)
	fsBuf := make([]uint16, 261)
	err = windows.GetVolumeInformation(root,
		&labelBuf[0], uint32(len(labelBuf)),
		nil, nil, nil,
		&fsBuf[0], uint32(len(fsBuf)))
	if err != nil {
		return "", "", fmt.Errorf("GetVolumeInformation %s: %w", letter, err)
	}
	return strings.TrimSpace(windows.UTF16ToString(labelBuf)),
		strings.TrimSpace(windows.UTF16ToString(fsBuf)), nil
}

// openVolume opens \\.\X: with no access rights, which is enough for the
// informational IOCTLs and does not require elevation.
func openVolume(letter string) (windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(`\\.\` + letter + `:`)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(path,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
}

// diskPerformance mirrors DISK_PERFORMANCE, truncated after the fields used
// here. The full structure ends with a storage manager name.
type diskPerformance struct {
	BytesRead      int64
	BytesWritten   int64
	ReadTime       int64
	WriteTime      int64
	IdleTime       int64
	ReadCount      uint32
	WriteCount     uint32
	QueueDepth     uint32
	SplitCount     uint32
	QueryTime      int64
	StorageDevNum  uint32
	StorageMgrName [8]uint16
}

// ioctlDiskPerformance is IOCTL_DISK_PERFORMANCE.
const ioctlDiskPerformance = 0x00070020

func performanceOf(letter string) (sample, error) {
	handle, err := openVolume(letter)
	if err != nil {
		return sample{}, fmt.Errorf("open volume %s: %w", letter, err)
	}
	defer windows.CloseHandle(handle)

	var perf diskPerformance
	var returned uint32
	err = windows.DeviceIoControl(handle, ioctlDiskPerformance,
		nil, 0,
		(*byte)(unsafe.Pointer(&perf)), uint32(unsafe.Sizeof(perf)),
		&returned, nil)
	if err != nil {
		return sample{}, fmt.Errorf("IOCTL_DISK_PERFORMANCE %s: %w", letter, err)
	}

	return sample{
		bytesRead:    perf.BytesRead,
		bytesWritten: perf.BytesWritten,
		idleTime:     perf.IdleTime,
	}, nil
}

// storagePropertyQuery mirrors STORAGE_PROPERTY_QUERY.
type storagePropertyQuery struct {
	PropertyID uint32
	QueryType  uint32
	Additional [1]byte
	_          [3]byte
}

// deviceSeekPenaltyDescriptor mirrors DEVICE_SEEK_PENALTY_DESCRIPTOR.
type deviceSeekPenaltyDescriptor struct {
	Version           uint32
	Size              uint32
	IncursSeekPenalty uint8
	_                 [3]byte
}

// storageDeviceDescriptor mirrors the fixed part of STORAGE_DEVICE_DESCRIPTOR.
type storageDeviceDescriptor struct {
	Version               uint32
	Size                  uint32
	DeviceType            uint8
	DeviceTypeModifier    uint8
	RemovableMedia        uint8
	CommandQueueing       uint8
	VendorIDOffset        uint32
	ProductIDOffset       uint32
	ProductRevisionOffset uint32
	SerialNumberOffset    uint32
	BusType               uint32
	RawPropertiesLength   uint32
}

const (
	ioctlStorageQueryProperty = 0x002D1400

	storageDeviceProperty    = 0
	storageDeviceSeekPenalty = 7
	propertyStandardQuery    = 0

	busTypeNVMe = 0x11
	busTypeUSB  = 0x07
)

// mediaKind reports whether the volume lives on an SSD, an NVMe drive or a
// spinning disk. An empty result means the drive did not say.
func mediaKind(letter string) string {
	handle, err := openVolume(letter)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	if bus, ok := busType(handle); ok && bus == busTypeNVMe {
		return "NVMe"
	}

	query := storagePropertyQuery{PropertyID: storageDeviceSeekPenalty, QueryType: propertyStandardQuery}
	var descriptor deviceSeekPenaltyDescriptor
	var returned uint32

	err = windows.DeviceIoControl(handle, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		(*byte)(unsafe.Pointer(&descriptor)), uint32(unsafe.Sizeof(descriptor)),
		&returned, nil)
	if err != nil {
		return ""
	}

	// A drive that pays no penalty for seeking has no platters to move.
	if descriptor.IncursSeekPenalty == 0 {
		return "SSD"
	}
	return "HDD"
}

func busType(handle windows.Handle) (uint32, bool) {
	query := storagePropertyQuery{PropertyID: storageDeviceProperty, QueryType: propertyStandardQuery}
	buf := make([]byte, 1024)
	var returned uint32

	err := windows.DeviceIoControl(handle, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		&buf[0], uint32(len(buf)),
		&returned, nil)
	if err != nil || returned < uint32(unsafe.Sizeof(storageDeviceDescriptor{})) {
		return 0, false
	}

	descriptor := (*storageDeviceDescriptor)(unsafe.Pointer(&buf[0]))
	return descriptor.BusType, true
}
