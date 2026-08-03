package rtss

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// RTSS hands over char[MAX_PATH], not WCHAR, so anything outside ASCII arrives
// in the system code page. Passing those bytes on as if they were UTF-8 put
// invalid sequences into the Prometheus exposition and the InfluxDB line, and
// Prometheus rejects an entire scrape over one bad byte.
func TestNamesAreAlwaysValidUTF8(t *testing.T) {
	for _, name := range []string{
		"Cyberpunk2077.exe",
		"J\xfcrgens Spiel.exe",   // "Jürgens Spiel.exe" in CP1252
		"\x83Q\x81[\x83\x80.exe", // Shift-JIS
		"\xff\xfe\xfd",           // nothing decodable at all
		"",
	} {
		padded := make([]byte, 260)
		copy(padded, name)

		got := cString(padded)
		if !utf8.ValidString(got) {
			t.Errorf("cString(%q) = %q, which is not valid UTF-8", name, got)
		}
		if strings.ContainsRune(got, 0) {
			t.Errorf("cString(%q) kept a NUL", name)
		}
	}
}

// The field is fixed width and NUL-terminated; nothing past the terminator
// belongs to the name.
func TestNameStopsAtTheTerminator(t *testing.T) {
	field := make([]byte, 260)
	copy(field, "game.exe\x00leftovers from an earlier title")

	if got := cString(field); got != "game.exe" {
		t.Errorf("cString = %q, want %q", got, "game.exe")
	}
}

func TestPlainASCIISurvivesUnchanged(t *testing.T) {
	const path = `C:\Games\Cyberpunk 2077\bin\x64\Cyberpunk2077.exe`

	field := make([]byte, 260)
	copy(field, path)

	if got := cString(field); got != path {
		t.Errorf("cString = %q, want %q", got, path)
	}
}
