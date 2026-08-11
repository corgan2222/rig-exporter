//go:build windows

package net

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wlanapi                = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpenHandle     = wlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle    = wlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces = wlanapi.NewProc("WlanEnumInterfaces")
	procWlanQueryInterface = wlanapi.NewProc("WlanQueryInterface")
	procWlanFreeMemory     = wlanapi.NewProc("WlanFreeMemory")
)

// wlanInterfaceInfo mirrors WLAN_INTERFACE_INFO.
type wlanInterfaceInfo struct {
	GUID        windows.GUID
	Description [256]uint16
	State       uint32
}

// wlanInterfaceInfoList mirrors WLAN_INTERFACE_INFO_LIST.
type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	Interfaces    [1]wlanInterfaceInfo
}

// dot11SSID mirrors DOT11_SSID.
type dot11SSID struct {
	Length uint32
	SSID   [32]byte
}

// wlanAssociationAttributes mirrors WLAN_ASSOCIATION_ATTRIBUTES.
type wlanAssociationAttributes struct {
	SSID          dot11SSID
	BSSType       uint32
	BSSID         [6]byte
	_             [2]byte
	PhyType       uint32
	PhyIndex      uint32
	SignalQuality uint32
	RxRate        uint32
	TxRate        uint32
}

// wlanConnectionAttributes mirrors the head of WLAN_CONNECTION_ATTRIBUTES.
// The security attributes that follow are not read here.
type wlanConnectionAttributes struct {
	State       uint32
	Mode        uint32
	ProfileName [256]uint16
	Association wlanAssociationAttributes
}

const (
	wlanAPIVersion2             = 2
	wlanOpcodeCurrentConnection = 7
	wlanInterfaceStateConnected = 1
)

// wifiSignalPercent returns the signal quality of the connected wireless
// interface, or 0 when there is none.
//
// The adapter name is not matched against the WLAN interface description,
// because Windows reports them differently; on the overwhelmingly common
// single-radio machine the first connected interface is the right one.
func wifiSignalPercent(_ string) float64 {
	// Load answers whether wlanapi.dll is there, not whether these four symbols
	// are, and LazyProc.Call panics on a symbol that is missing. A machine with
	// no wireless at all is the ordinary case here, so a missing symbol is
	// simply "no signal" like every other way this can come up empty.
	for _, p := range []*windows.LazyProc{
		procWlanOpenHandle, procWlanCloseHandle, procWlanEnumInterfaces,
		procWlanQueryInterface, procWlanFreeMemory,
	} {
		if p.Find() != nil {
			return 0
		}
	}

	var handle windows.Handle
	var negotiated uint32
	if ret, _, _ := procWlanOpenHandle.Call(
		wlanAPIVersion2, 0,
		uintptr(unsafe.Pointer(&negotiated)),
		uintptr(unsafe.Pointer(&handle)),
	); ret != 0 {
		return 0
	}
	defer procWlanCloseHandle.Call(uintptr(handle), 0)

	var list *wlanInterfaceInfoList
	if ret, _, _ := procWlanEnumInterfaces.Call(
		uintptr(handle), 0,
		uintptr(unsafe.Pointer(&list)),
	); ret != 0 || list == nil {
		return 0
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(list)))

	interfaces := unsafe.Slice(&list.Interfaces[0], int(list.NumberOfItems))
	for i := range interfaces {
		if interfaces[i].State != wlanInterfaceStateConnected {
			continue
		}
		if quality, ok := connectionQuality(handle, &interfaces[i].GUID); ok {
			return float64(quality)
		}
	}
	return 0
}

func connectionQuality(handle windows.Handle, guid *windows.GUID) (uint32, bool) {
	var size uint32
	var data *wlanConnectionAttributes

	ret, _, _ := procWlanQueryInterface.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(guid)),
		wlanOpcodeCurrentConnection,
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(unsafe.Pointer(&data)),
		0,
	)
	if ret != 0 || data == nil {
		return 0, false
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(data)))

	if size < uint32(unsafe.Sizeof(wlanConnectionAttributes{})) {
		return 0, false
	}
	return data.Association.SignalQuality, true
}
