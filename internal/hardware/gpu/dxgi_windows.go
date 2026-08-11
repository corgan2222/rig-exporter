//go:build windows

package gpu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DXGI is Windows' own graphics-adapter inventory. Unlike Afterburner and
// NVML it is present on every supported Windows installation and therefore
// also sees integrated Intel laptop graphics without an extra program.
var (
	dxgiDLL                = windows.NewLazySystemDLL("dxgi.dll")
	procCreateDXGIFactory1 = dxgiDLL.NewProc("CreateDXGIFactory1")
	iidIDXGIFactory1       = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
	displayClassGUID       = windows.GUID{Data1: 0x4d36e968, Data2: 0xe325, Data3: 0x11ce, Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}
	driverVersionProperty  = windows.DEVPROPKEY{
		FmtID: windows.DEVPROPGUID{Data1: 0xa8b865dd, Data2: 0x2e3d, Data3: 0x4094, Data4: [8]byte{0xad, 0x97, 0xe5, 0x93, 0xa7, 0x0c, 0x75, 0xd6}},
		PID:   3,
	}
)

const (
	// COM vtables include every method inherited from their base interfaces.
	// These are the slots in the Windows SDK's IDXGIFactory1 and
	// IDXGIAdapter1 declarations.
	comReleaseSlot          = 2
	dxgiEnumAdapters1Slot   = 12
	dxgiAdapterGetDesc1Slot = 10

	dxgiAdapterFlagRemote   = 1
	dxgiAdapterFlagSoftware = 2
	dxgiErrorNotFound       = 0x887a0002
)

// dxgiAdapter is the stable, Go-sized subset of DXGI_ADAPTER_DESC1 used by the
// collector. Memory values stay in bytes until they become metric readings so
// no precision is lost at the Windows boundary.
//
// SubSystemID, Revision and DedicatedSystemMemory were carried here and read by
// nobody. One of them is worth a sentence before it goes.
//
// SubSystemID looks like the obvious way to tell mirrored DXGI adapters apart —
// the board a chip is soldered to, so two entries for one card ought to differ.
// Measured, they do not: both RTX 2080 entries carry the identical
// SUBSYS_1E8710B0. It is not the discriminator, and the Plug and Play match is
// what does that job. Written down so the next person does not measure it again.
type dxgiAdapter struct {
	Index                int
	Name                 string
	VendorID             uint32
	DeviceID             uint32
	DriverVersion        string
	DedicatedVideoMemory uint64
	SharedSystemMemory   uint64
	LUID                 windows.LUID
}

// dxgiAdapterDesc1 mirrors DXGI_ADAPTER_DESC1 from dxgi.h. SIZE_T is uintptr,
// which gives the structure the correct layout on the supported windows/amd64
// target without CGO.
type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSystemID           uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	LUID                  windows.LUID
	Flags                 uint32
}

// dxgiAdapters enumerates physical graphics adapters through DXGI 1.1.
// CreateDXGIFactory1 and EnumAdapters1 are available on every Windows version
// this project supports. Software and remote renderers are deliberately left
// out: they are display plumbing, not GPUs a user can monitor.
func dxgiAdapters() ([]dxgiAdapter, error) {
	// LazyProc.Call panics when the symbol does not exist. Find first so an old
	// or damaged Windows installation becomes an ordinary unavailable source.
	if err := procCreateDXGIFactory1.Find(); err != nil {
		return nil, fmt.Errorf("find CreateDXGIFactory1: %w", err)
	}

	var factory uintptr
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(&iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hresultFailed(hr) {
		return nil, hresultError("CreateDXGIFactory1", hr)
	}
	if factory == 0 {
		return nil, fmt.Errorf("CreateDXGIFactory1 returned a nil factory")
	}
	defer comRelease(factory)

	var adapters []dxgiAdapter
	for index := uint32(0); ; index++ {
		var adapter uintptr
		hr = comCall(factory, dxgiEnumAdapters1Slot,
			uintptr(index), uintptr(unsafe.Pointer(&adapter)))
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if hresultFailed(hr) {
			if len(adapters) > 0 {
				break
			}
			return nil, hresultError("IDXGIFactory1.EnumAdapters1", hr)
		}
		if adapter == 0 {
			continue
		}

		var desc dxgiAdapterDesc1
		hr = comCall(adapter, dxgiAdapterGetDesc1Slot,
			uintptr(unsafe.Pointer(&desc)))
		comRelease(adapter)
		if hresultFailed(hr) {
			continue
		}
		if desc.Flags&(dxgiAdapterFlagRemote|dxgiAdapterFlagSoftware) != 0 {
			continue
		}

		name := strings.TrimSpace(windows.UTF16ToString(desc.Description[:]))
		if name == "" {
			continue
		}
		adapters = append(adapters, dxgiAdapter{
			Index:                int(index),
			Name:                 name,
			VendorID:             desc.VendorID,
			DeviceID:             desc.DeviceID,
			DedicatedVideoMemory: uint64(desc.DedicatedVideoMemory),
			SharedSystemMemory:   uint64(desc.SharedSystemMemory),
			LUID:                 desc.LUID,
		})
	}

	if len(adapters) == 0 {
		return nil, fmt.Errorf("DXGI found no physical graphics adapter")
	}
	// A remote desktop or Citrix display driver can mirror a physical adapter
	// into DXGI with the physical card's description and PCI identifiers. The
	// flags still say hardware, so use the present Plug and Play device count to
	// keep one DXGI entry per actual display device. Two genuinely identical
	// cards remain two because Plug and Play contains two devices.
	if devices, err := presentPCIAdapters(); err == nil {
		adapters = limitAdaptersToPlugAndPlay(adapters, devices)
	}
	if len(adapters) == 0 {
		return nil, fmt.Errorf("DXGI adapters did not match a present display device")
	}
	return adapters, nil
}

type pciAdapterID struct {
	VendorID uint32
	DeviceID uint32
}

type plugAndPlayAdapter struct {
	Count         int
	DriverVersion string
}

func presentPCIAdapters() (map[pciAdapterID]plugAndPlayAdapter, error) {
	devices, err := windows.SetupDiGetClassDevsEx(
		&displayClassGUID, "", 0, windows.DIGCF_PRESENT, 0, "")
	if err != nil {
		return nil, fmt.Errorf("enumerate display devices: %w", err)
	}
	defer devices.Close()

	adapters := map[pciAdapterID]plugAndPlayAdapter{}
	for index := 0; ; index++ {
		device, err := devices.EnumDeviceInfo(index)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("enumerate display device %d: %w", index, err)
		}

		value, err := devices.DeviceRegistryProperty(device, windows.SPDRP_HARDWAREID)
		if err != nil {
			continue
		}
		hardwareIDs, ok := value.([]string)
		if !ok {
			continue
		}
		for _, hardwareID := range hardwareIDs {
			id, ok := parsePCIHardwareID(hardwareID)
			if !ok {
				continue
			}
			adapter := adapters[id]
			adapter.Count++
			if adapter.DriverVersion == "" {
				if value, err := windows.SetupDiGetDeviceProperty(
					devices, device, &driverVersionProperty); err == nil {
					adapter.DriverVersion, _ = value.(string)
				}
			}
			adapters[id] = adapter
			break
		}
	}
	if len(adapters) == 0 {
		return nil, fmt.Errorf("plug and play found no PCI display adapters")
	}
	return adapters, nil
}

func parsePCIHardwareID(hardwareID string) (pciAdapterID, bool) {
	upper := strings.ToUpper(strings.TrimSpace(hardwareID))
	if !strings.HasPrefix(upper, `PCI\`) {
		return pciAdapterID{}, false
	}

	vendor, vendorOK := pciHardwareIDField(upper, "VEN_")
	device, deviceOK := pciHardwareIDField(upper, "DEV_")
	if !vendorOK || !deviceOK {
		return pciAdapterID{}, false
	}
	return pciAdapterID{VendorID: vendor, DeviceID: device}, true
}

func pciHardwareIDField(hardwareID, marker string) (uint32, bool) {
	start := strings.Index(hardwareID, marker)
	if start < 0 {
		return 0, false
	}
	value := hardwareID[start+len(marker):]
	if end := strings.IndexAny(value, `&\`); end >= 0 {
		value = value[:end]
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	return uint32(parsed), err == nil
}

// limitAdaptersToPlugAndPlay keeps the DXGI adapters that Plug and Play also
// reports, which is how the mirrored entries DXGI hands out are dropped.
//
// The empty-devices branch is not reachable from production: presentPCIAdapters
// returns an error when it found nothing, and the only caller runs this on
// err == nil. It stays anyway, and deliberately. Without it an empty map makes
// every adapter fail the used[id] >= device.Count test against a zero-valued
// entry, so the function returns nothing — every GPU entity would disappear
// without a word. Two lines against a silent total loss is a trade worth making
// for a precondition that lives in another function.
func limitAdaptersToPlugAndPlay(adapters []dxgiAdapter, devices map[pciAdapterID]plugAndPlayAdapter) []dxgiAdapter {
	if len(devices) == 0 {
		out := append([]dxgiAdapter(nil), adapters...)
		for index := range out {
			out[index].Index = index
		}
		return out
	}

	used := map[pciAdapterID]int{}
	out := make([]dxgiAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		id := pciAdapterID{
			VendorID: adapter.VendorID,
			DeviceID: adapter.DeviceID,
		}
		device := devices[id]
		if used[id] >= device.Count {
			continue
		}
		used[id]++
		adapter.Index = len(out)
		adapter.DriverVersion = device.DriverVersion
		out = append(out, adapter)
	}
	return out
}

func comCall(object uintptr, slot uintptr, args ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(vtable + slot*unsafe.Sizeof(uintptr(0))))
	params := make([]uintptr, 1, len(args)+1)
	params[0] = object
	params = append(params, args...)
	result, _, _ := syscall.SyscallN(method, params...)
	return result
}

func comRelease(object uintptr) {
	if object != 0 {
		comCall(object, comReleaseSlot)
	}
}

func hresultFailed(result uintptr) bool { return int32(uint32(result)) < 0 }

func hresultError(operation string, result uintptr) error {
	return fmt.Errorf("%s failed with HRESULT 0x%08x", operation, uint32(result))
}
