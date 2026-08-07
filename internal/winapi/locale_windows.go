//go:build windows

package winapi

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
	procLCIDToLocaleName         = kernel32.NewProc("LCIDToLocaleName")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

// localeNameMax is LOCALE_NAME_MAX_LENGTH: the longest name Windows will hand
// back, in UTF-16 units.
const localeNameMax = 85

// UILanguage is the display language as a BCP-47 name, such as de-DE.
//
// The display language, not the number format, and the difference matters for
// a crash report. Windows names devices, drives and accounts in the display
// language, and those names are what this program turns into instance
// identifiers: Slug keeps only a-z0-9, so an adapter called in Japanese or
// Cyrillic leaves nothing behind and falls through to a digest. An entity then
// reads net_x1a2b3c4d_ip rather than net_ethernet_3_ip.
//
// That is intended behaviour, but it is also exactly the report that arrives,
// and without the language it cannot be placed. Pointed out by the agent
// building the issue form, who noticed it from the other side.
//
// Empty when Windows will not say, which is not worth an error: a report with
// one field missing is still a report.
func UILanguage() string {
	if err := procGetUserDefaultUILanguage.Find(); err == nil {
		if err := procLCIDToLocaleName.Find(); err == nil {
			langID, _, _ := procGetUserDefaultUILanguage.Call()
			buf := make([]uint16, localeNameMax)
			// A LANGID is a locale identifier with a neutral sort order, which
			// is what LCIDToLocaleName expects here.
			n, _, _ := procLCIDToLocaleName.Call(langID,
				uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
			if n > 1 {
				return windows.UTF16ToString(buf[:n-1])
			}
		}
	}

	// The user's locale rather than the display language. On almost every
	// machine the two agree, and for placing a report it is enough.
	if err := procGetUserDefaultLocaleName.Find(); err != nil {
		return ""
	}
	buf := make([]uint16, localeNameMax)
	n, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n <= 1 {
		return ""
	}
	return windows.UTF16ToString(buf[:n-1])
}
