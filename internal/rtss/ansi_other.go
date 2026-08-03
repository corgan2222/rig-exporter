//go:build !windows

package rtss

import "strings"

// decodeANSI has no code page to consult off Windows, where this package only
// exists so the parser stays testable. Invalid bytes are dropped rather than
// passed on, which is the same guarantee the Windows implementation gives.
func decodeANSI(b []byte) string {
	return strings.ToValidUTF8(string(b), "")
}
