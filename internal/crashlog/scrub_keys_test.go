package crashlog

import (
	"strings"
	"testing"
)

// The three names are not invented: that is what the credential fields are
// called in config.go. A scrubber that does not know this project's own keys
// washes the wrong thing.
//
// The reason it missed them is one character. `\b` wants a change between a
// word character and a non-word one, and the underscore is a word character —
// so between "mqtt_" and "password" there is no boundary at all, and the
// expression could never match there.
func TestTheScrubberCatchesThisProjectsOwnKeyNames(t *testing.T) {
	secrets := []string{"hunter2", "abcdef123", "zzz", "eyJabc"}

	for _, tc := range []struct {
		name string
		line string
	}{
		{"mqtt_password", `mqtt_password=hunter2`},
		{"influx_token", `influx_token=abcdef123`},
		{"data_token", `data_token=zzz`},
		{"mqtt_password as JSON", `{"mqtt_password":"hunter2"}`},
		{"password as JSON", `{"password":"hunter2"}`},
		{"bearer in a header", `Authorization="Bearer eyJabc"`},
		{"bearer without quotes", `Authorization: Bearer eyJabc`},
		{"plain password", `password=hunter2`},
		{"plain token", `token=abcdef123`},
		{"plain api_key", `api_key=zzz`},
		{"slog quoting it", `msg="broker refused" mqtt_password=hunter2`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Scrub(tc.line)

			for _, secret := range secrets {
				if strings.Contains(got, secret) {
					t.Errorf("the secret survived the scrub:\n  in:  %s\n  out: %s", tc.line, got)
				}
			}
			if !strings.Contains(got, "<removed>") {
				t.Errorf("nothing was removed at all:\n  in:  %s\n  out: %s", tc.line, got)
			}
		})
	}
}

// And what stays: the host name, drive labels, addresses. A report with
// everything blacked out is not a report, and the whole point of keeping those
// is that the fault is usually in one of them.
func TestTheScrubberLeavesDiagnosticTextAlone(t *testing.T) {
	for _, line := range []string{
		`broker=tcp://homeassistant.local:1883`,
		`gpu_memory_used=7989`,
		`node_id=corganpc2`,
		// The case the \w* prefix has to be measured against: it may grow
		// backwards, never forwards.
		`passwordless=true`,
		`msg="the token was refused by the broker"`,
	} {
		if got := Scrub(line); got != line {
			t.Errorf("scrubbed something harmless:\n  in:  %s\n  out: %s", line, got)
		}
	}
}

// The key survives the wash legibly. A report that says "mqtt_password" tells a
// maintainer which setting is involved; one that says `"mqtt_password"` with
// its quotes still on, or the whole line collapsed into the key, tells them
// less than it could.
func TestTheScrubbedKeyStaysReadable(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`mqtt_password=hunter2`, `mqtt_password=<removed>`},
		{`{"influx_token":"abcdef123"}`, `{influx_token=<removed>}`},
	} {
		if got := Scrub(tc.line); got != tc.want {
			t.Errorf("Scrub(%s) = %s, want %s", tc.line, got, tc.want)
		}
	}
}
