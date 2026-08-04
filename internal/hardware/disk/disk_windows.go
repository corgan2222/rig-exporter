//go:build windows

// Package disk collects one reading set per fixed volume: what kind of drive
// it is, how full it is, and how much it is being read and written.
//
// Throughput comes from IOCTL_DISK_PERFORMANCE on the volume handle. The
// handle is opened with no access rights at all, which is what lets an
// unelevated process ask a volume for its counters.
package disk

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// gb converts the byte counts Windows reports into the unit the readings use.
const gb = 1024 * 1024 * 1024

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

	// Summed here rather than in the collector, because this is the one place
	// that knows which volumes were actually reported: an excluded drive, or
	// one that could not be read, must not count towards the total either.
	found := 0
	var totalAll, freeAll uint64
	for _, letter := range letters {
		if !s.wants(letter) {
			continue
		}
		total, free, ok := s.collectVolume(set, letter, previous, current, elapsed)
		if !ok {
			continue
		}
		found++
		totalAll += total
		freeAll += free
	}

	s.mu.Lock()
	s.last = current
	s.mu.Unlock()

	if found == 0 {
		return fmt.Errorf("no volume could be read")
	}
	addOverall(set, totalAll, freeAll)
	return nil
}

// addOverall reports every reported volume together. Nothing is added for a
// machine whose volumes all read zero capacity — a total of zero would divide
// by zero below and would say something false besides.
func addOverall(set *metrics.Set, total, free uint64) {
	if total == 0 {
		return
	}

	used := total - free
	set.Add(
		metrics.Gauge(metrics.DiskOverallCapacity, "", float64(total)/gb),
		metrics.Gauge(metrics.DiskOverallUsed, "", float64(used)/gb),
		metrics.Gauge(metrics.DiskOverallFree, "", float64(free)/gb),
		metrics.Gauge(metrics.DiskOverallUsage, "", float64(used)/float64(total)*100),
		metrics.Gauge(metrics.DiskOverallFreePercent, "", float64(free)/float64(total)*100),
	)
}

// collectVolume appends the readings of one drive letter. It returns the
// volume's capacity and free space so the caller can sum them, and false when
// nothing could be read.
func (s *Source) collectVolume(set *metrics.Set, letter string, previous, current map[string]sample, elapsed float64) (uint64, uint64, bool) {
	instance := letter + ":"

	total, free, err := spaceOf(letter)
	if err != nil || total == 0 {
		return 0, 0, false
	}

	used := total - free
	set.Add(
		metrics.Gauge(metrics.DiskTotal, instance, float64(total)/gb),
		metrics.Gauge(metrics.DiskUsed, instance, float64(used)/gb),
		metrics.Gauge(metrics.DiskFree, instance, float64(free)/gb),
		metrics.Gauge(metrics.DiskUsedPercent, instance, float64(used)/float64(total)*100),
		metrics.Gauge(metrics.DiskFreePercent, instance, float64(free)/float64(total)*100),
	)

	// Label and file system are separate facts and are reported separately. They
	// used to be glued together as "Windows (NTFS)", which read well and could
	// not be filtered, compared or graphed.
	if label, filesystem, err := volumeInfo(letter); err == nil {
		if label != "" {
			set.Add(metrics.Text(metrics.DiskLabel, instance, label))
		}
		if filesystem != "" {
			set.Add(metrics.Text(metrics.DiskFilesystem, instance, filesystem))
		}
	}
	if media := mediaKind(letter); media != "" {
		set.Add(metrics.Text(metrics.DiskMedia, instance, media))
	}
	if vendor := driveVendor(letter); vendor != "" {
		set.Add(metrics.Text(metrics.DiskVendor, instance, vendor))
	}

	now, err := performanceOf(letter)
	if err != nil {
		return total, free, true // space readings are still worth having
	}
	current[letter] = now

	before, seen := previous[letter]
	if !seen || elapsed <= 0 {
		return total, free, true // the first collection has no interval to divide by
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
	return total, free, true
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
// skipping network shares, optical drives, removable media and anything hanging
// off a USB port.
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
		if windows.GetDriveType(root) != windows.DRIVE_FIXED {
			continue
		}
		if attachedByUSB(letter) {
			continue
		}
		letters = append(letters, letter)
	}
	if len(letters) == 0 {
		return nil, fmt.Errorf("no fixed drives found")
	}
	return letters, nil
}

// attachedByUSB reports whether a volume hangs off a USB port.
//
// GetDriveType is not enough. A USB memory stick answers DRIVE_REMOVABLE and is
// filtered above, but an external USB SSD or hard disk almost always answers
// DRIVE_FIXED — indistinguishable from an internal drive without asking the
// storage stack which bus it is on. Such a drive is somebody's backup disk that
// happens to be plugged in today; counting it towards "how full is this
// machine" would make the total jump around for reasons that have nothing to do
// with the machine.
//
// A drive that cannot be asked is kept. Not being able to tell is not a reason
// to drop a drive somebody is watching.
func attachedByUSB(letter string) bool {
	handle, err := openVolume(letter)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	bus, ok := busType(handle)
	return ok && bus == busTypeUSB
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

// driveVendor reports who made the drive, as far as the drive will say.
//
// The device descriptor carries a vendor and a product string, and which one
// holds the manufacturer depends on the bus. SATA drives fill in the vendor;
// NVMe drives very often leave it empty and put everything into the product
// string, where the manufacturer is the first word — "Samsung SSD 980 PRO 1TB".
// Taking that first word is a guess, but a well-founded one, and an empty
// result is returned rather than a wrong one whenever nothing usable is there.
func driveVendor(letter string) string {
	handle, err := openVolume(letter)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	query := storagePropertyQuery{PropertyID: storageDeviceProperty, QueryType: propertyStandardQuery}
	buf := make([]byte, 1024)
	var returned uint32

	err = windows.DeviceIoControl(handle, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		&buf[0], uint32(len(buf)),
		&returned, nil)
	if err != nil || returned < uint32(unsafe.Sizeof(storageDeviceDescriptor{})) {
		return ""
	}

	descriptor := (*storageDeviceDescriptor)(unsafe.Pointer(&buf[0]))
	if vendor := descriptorString(buf[:returned], descriptor.VendorIDOffset); vendor != "" {
		return vendor
	}
	return brandOf(descriptorString(buf[:returned], descriptor.ProductIDOffset))
}

// brandOf picks the manufacturer out of a product string, or gives up.
//
// "Samsung SSD 980 PRO 1TB" starts with its maker, so the first word is right.
// "WDS200T1X0E-00AFY0" is a part number and starts with nothing of the sort —
// publishing it as the manufacturer would be a wrong answer where no answer was
// available. A leading token carrying digits is a part number, not a brand, and
// no brand needs digits to be recognised.
func brandOf(product string) string {
	word := firstWord(product)
	if word == "" || strings.ContainsAny(word, "0123456789") {
		return ""
	}
	return word
}

// descriptorString reads one of the NUL-terminated strings the descriptor
// points at by offset. The offsets come from the driver, so they are checked
// against the buffer rather than trusted.
func descriptorString(buf []byte, offset uint32) string {
	if offset == 0 || int(offset) >= len(buf) {
		return ""
	}
	rest := buf[offset:]
	if end := bytes.IndexByte(rest, 0); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(string(rest))
}

func firstWord(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
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
