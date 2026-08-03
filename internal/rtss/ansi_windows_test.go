//go:build windows

package rtss

import "testing"

// Proof that the code page conversion actually runs, rather than the sanitising
// fallback quietly dropping every byte it does not understand. On a Western
// European ANSI code page 0xFC is "ü", and a game under C:\Users\Jürgen has to
// come out readable — dropping the umlaut would be valid UTF-8 and still wrong.
func TestTheCodePageConversionRunsRatherThanTheFallback(t *testing.T) {
	field := make([]byte, 260)
	copy(field, "J\xfcrgen.exe")

	got := cString(field)
	if got == "Jrgen.exe" {
		t.Skip("this machine's ANSI code page is not Western European")
	}
	if got != "Jürgen.exe" {
		t.Errorf("cString = %q, want %q", got, "Jürgen.exe")
	}
}
