//go:build windows

package rtss

import (
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

// cpACP selects the system ANSI code page. x/sys/windows does not export the
// constant, and it has been zero since Win32 existed.
const cpACP = 0

// decodeANSI converts bytes in the system ANSI code page to UTF-8.
//
// Everything RTSS hands over is char, not WCHAR, so on a German machine "ü" is
// the single byte 0xFC and on a Japanese one a Shift-JIS pair — neither is
// valid UTF-8, and Go's string() would carry the raw bytes straight through to
// the JSON document, the Prometheus exposition and the InfluxDB line.
//
// CP_ACP is the right code page here rather than CP_UTF8: it is the one the
// ANSI Win32 functions RTSS itself uses encode with.
func decodeANSI(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	// Pure ASCII is the overwhelmingly common case and is already valid UTF-8.
	if isASCII(b) {
		return string(b)
	}

	n, err := windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		// Conversion failed; still never emit invalid UTF-8.
		return sanitize(b)
	}

	buf := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(
		cpACP, 0, &b[0], int32(len(b)), &buf[0], n,
	); err != nil {
		return sanitize(b)
	}
	return windows.UTF16ToString(buf)
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// sanitize is the last resort: drop what cannot be decoded rather than let it
// reach an exporter.
func sanitize(b []byte) string {
	out := make([]rune, 0, len(b))
	for _, c := range b {
		if c < utf8.RuneSelf {
			out = append(out, rune(c))
		}
	}
	return string(out)
}
