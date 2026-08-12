//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Human interface devices are how every USB-attached cooling controller worth
// reading talks: the pump, the fan hub and the flow meter all announce
// themselves as HID and push a status report on their own, about once a
// second. Reading one needs no elevation, no vendor driver and no library —
// SetupAPI to find it, CreateFile to open it, ReadFile to receive it.
//
// Only reading is offered here. Writing to a cooling controller is how a pump
// curve gets changed, and this program has no business doing that.

var (
	hid = windows.NewLazySystemDLL("hid.dll")

	procHidDGetHidGUID    = hid.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes = hid.NewProc("HidD_GetAttributes")
)

// hidAttributes is HIDD_ATTRIBUTES.
type hidAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

// HIDDevice is one HID interface that is present right now.
type HIDDevice struct {
	// Path is what CreateFile wants. It stays valid until the device is
	// unplugged, so it is worth keeping rather than enumerating again.
	Path      string
	VendorID  uint16
	ProductID uint16
}

// ErrHIDUnavailable is returned when hid.dll does not offer what we need. It
// is separated out because it means "not on this Windows", not "no device".
var ErrHIDUnavailable = errors.New("hid.dll does not offer the required entry points")

// HIDDevices lists the HID interfaces present, without opening any of them.
//
// A device that is there but busy still shows up here; whether it can be read
// is the question OpenHID answers.
func HIDDevices() ([]HIDDevice, error) {
	// Every entry point is checked before it is called. LazyProc.Call panics
	// on a missing symbol — there is no error return — and with -H windowsgui
	// that kills the program without a word.
	for _, proc := range []*windows.LazyProc{procHidDGetHidGUID, procHidDGetAttributes} {
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrHIDUnavailable, err)
		}
	}
	if err := procSetupDiEnumDeviceInterfaces.Find(); err != nil {
		return nil, fmt.Errorf("SetupDiEnumDeviceInterfaces unavailable: %w", err)
	}
	if err := procSetupDiGetInterfaceDetail.Find(); err != nil {
		return nil, fmt.Errorf("SetupDiGetDeviceInterfaceDetailW unavailable: %w", err)
	}

	var guid windows.GUID
	procHidDGetHidGUID.Call(uintptr(unsafe.Pointer(&guid)))

	set, err := windows.SetupDiGetClassDevsEx(&guid, "", 0,
		windows.DIGCF_PRESENT|windows.DIGCF_DEVICEINTERFACE, 0, "")
	if err != nil {
		return nil, fmt.Errorf("SetupDiGetClassDevs(hid): %w", err)
	}
	defer set.Close()

	var out []HIDDevice
	for index := 0; ; index++ {
		path, ok := interfacePath(set, &guid, index)
		if !ok {
			break
		}
		device, err := hidAttributesOf(path)
		if err != nil {
			// A keyboard held open by something else is not a reason to stop
			// looking for the pump.
			continue
		}
		out = append(out, device)
	}
	return out, nil
}

// hidAttributesOf opens one interface just long enough to ask what it is.
func hidAttributesOf(path string) (HIDDevice, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return HIDDevice{}, err
	}
	// No access rights at all: that is enough for the attributes, and it is
	// the one open that a device already held exclusively will still allow.
	handle, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return HIDDevice{}, err
	}
	defer windows.CloseHandle(handle)

	attrs := hidAttributes{}
	attrs.Size = uint32(unsafe.Sizeof(attrs))
	ret, _, callErr := procHidDGetAttributes.Call(uintptr(handle), uintptr(unsafe.Pointer(&attrs)))
	if ret == 0 {
		return HIDDevice{}, fmt.Errorf("HidD_GetAttributes: %w", callErr)
	}
	return HIDDevice{Path: path, VendorID: attrs.VendorID, ProductID: attrs.ProductID}, nil
}

// HIDReader is an open HID interface that reports can be read from.
type HIDReader struct {
	handle windows.Handle
	event  windows.Handle
}

// OpenHID opens a device for reading.
//
// Shared, and read-only: the vendor's own software is usually running and must
// keep working. Overlapped, because a read that no report answers has to be
// abandoned rather than waited out — a status report is worth 200 ms, not a
// stalled measuring loop.
func OpenHID(path string) (*HIDReader, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("open hid: %w", err)
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("event: %w", err)
	}
	return &HIDReader{handle: handle, event: event}, nil
}

// ErrHIDTimeout says no report arrived in time. Not a failure of the device —
// these controllers report when they feel like it.
var ErrHIDTimeout = errors.New("no HID report within the timeout")

// Read waits for one input report and returns how much of buf it filled.
func (r *HIDReader) Read(buf []byte, timeout time.Duration) (int, error) {
	if r == nil || r.handle == windows.InvalidHandle {
		return 0, errors.New("hid reader is closed")
	}

	var read uint32
	overlapped := windows.Overlapped{HEvent: r.event}
	windows.ResetEvent(r.event)

	err := windows.ReadFile(r.handle, buf, &read, &overlapped)
	if err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
		return 0, err
	}
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		wait, err := windows.WaitForSingleObject(r.event, uint32(timeout.Milliseconds()))
		if err != nil {
			r.abort(&overlapped, &read)
			return 0, err
		}
		if wait != windows.WAIT_OBJECT_0 {
			r.abort(&overlapped, &read)
			return 0, ErrHIDTimeout
		}
		if err := windows.GetOverlappedResult(r.handle, &overlapped, &read, false); err != nil {
			return 0, err
		}
	}
	return int(read), nil
}

// abort ends a read that is still running, and does not return until the driver
// has actually let go of it.
//
// The waiting is the whole point, and leaving it out cost a fortnight of
// crashes. CancelIoEx only *requests* cancellation: it returns immediately, and
// the driver may still complete the read afterwards — writing into buf and into
// overlapped. Both are Go memory. overlapped is a local that dies with the
// return, and the caller reuses buf for the next read, so the kernel then
// writes into memory the runtime has handed to somebody else. That is heap
// corruption, and it surfaces far away from here as "fatal error: selectgo:
// bad wakeup" or "fatal error: fault" — in this program, in a select statement
// in the cooling poll loop that has nothing to do with any of it.
//
// GetOverlappedResult with wait=true is what makes the operation over. It
// returns ERROR_OPERATION_ABORTED for a read that was cancelled, which is the
// expected outcome here and not worth reporting.
//
// CancelIoEx rather than CancelIo for a second reason: CancelIo only cancels
// what the calling thread started, and a goroutine does not stay on one thread.
// CancelIo could therefore cancel nothing at all and still return success.
func (r *HIDReader) abort(overlapped *windows.Overlapped, read *uint32) {
	if err := windows.CancelIoEx(r.handle, overlapped); err != nil &&
		!errors.Is(err, windows.ERROR_NOT_FOUND) {
		// ERROR_NOT_FOUND means it finished on its own between the timeout and
		// the cancel. Anything else leaves the read running, and returning now
		// would hand its buffer back to the allocator — so wait regardless.
		_ = err
	}
	_ = windows.GetOverlappedResult(r.handle, overlapped, read, true)
}

// Close releases the device.
func (r *HIDReader) Close() error {
	if r == nil {
		return nil
	}
	if r.event != 0 {
		windows.CloseHandle(r.event)
		r.event = 0
	}
	if r.handle != 0 && r.handle != windows.InvalidHandle {
		err := windows.CloseHandle(r.handle)
		r.handle = windows.InvalidHandle
		return err
	}
	return nil
}
