package crashlog

import (
	"strings"
	"testing"
	"time"
)

// A session that shut down cleanly leaves an empty file. Reading that as a
// crash would put a banner in front of somebody every single start.
func TestAnEmptyRecordIsNotACrash(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\n"} {
		if _, crashed := classify(text); crashed {
			t.Errorf("%q was read as a crash", text)
		}
	}
}

// A panic and a kill are different news. One is a bug with a stack, the other
// is somebody closing the task manager, and telling a user to report the
// second as an issue wastes everybody's time.
func TestAPanicAndAKillAreToldApart(t *testing.T) {
	head := header("1.9.2", "170.abc", 4242, time.Now())

	tests := []struct {
		name string
		text string
		want Kind
	}{
		{"killed", head, KindUnclean},
		{"panic", head + "panic: assignment to entry in nil map\n\ngoroutine 1 [running]:\n", KindPanic},
		{"fatal", head + "fatal error: out of memory\n", KindPanic},
		{"fault", head + "runtime error: invalid memory address\n", KindPanic},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, crashed := classify(tc.text)
			if !crashed {
				t.Fatal("a session with a record was read as clean")
			}
			if kind != tc.want {
				t.Errorf("kind = %q, want %q", kind, tc.want)
			}
		})
	}
}

// The header has to survive the round trip, or a report cannot say which build
// crashed — and after an update that is not the build reading the report.
func TestTheHeaderSaysWhichBuildCrashed(t *testing.T) {
	when := time.Now().Truncate(time.Second)
	text := header("1.9.2", "170.abcdef", 1234, when)

	at, version, build := parseHeader(text)
	if !at.Equal(when) {
		t.Errorf("time = %v, want %v", at, when)
	}
	if version != "1.9.2" {
		t.Errorf("version = %q", version)
	}
	if build != "170.abcdef" {
		t.Errorf("build = %q", build)
	}
}

func TestTheSummaryIsTheLineThatNamesTheFault(t *testing.T) {
	r := Report{Kind: KindPanic, Text: header("1.9.2", "", 1, time.Now()) +
		"panic: assignment to entry in nil map\n\ngoroutine 1 [running]:\nmain.main()\n"}

	if got := r.Summary(); got != "panic: assignment to entry in nil map" {
		t.Errorf("summary = %q", got)
	}
}

// Cutting the end off a goroutine dump throws away the goroutines that were
// waiting, which is often where the deadlock is. The middle goes instead.
func TestTruncationKeepsBothEnds(t *testing.T) {
	text := strings.Repeat("A", 500) + strings.Repeat("Z", 500)

	got := truncate(text, 300)
	if len(got) > 300 {
		t.Errorf("length = %d, want at most 300", len(got))
	}
	if !strings.HasPrefix(got, "AAA") {
		t.Error("the beginning was cut, so the fault line is gone")
	}
	if !strings.HasSuffix(got, "ZZZ") {
		t.Error("the end was cut, so the waiting goroutines are gone")
	}
}

func TestShortTextIsNotTouched(t *testing.T) {
	if got := truncate("short", 300); got != "short" {
		t.Errorf("truncate rewrote a short text: %q", got)
	}
}

// A trailing slash on the project URL must not produce a double one.
func TestTheIssueURLSurvivesATrailingSlash(t *testing.T) {
	raw := IssueURL("https://github.com/corgan2222/rig-exporter/", Report{Version: "1.9.2"}, Machine{})
	if strings.Contains(raw, "rig-exporter//issues") {
		t.Errorf("doubled slash: %s", raw)
	}
}

// However long the dump, the prepared link has to stay something a browser
// will actually open.
func TestALongDumpStillFitsInALink(t *testing.T) {
	r := Report{Kind: KindPanic, Version: "1.9.2", Text: strings.Repeat("goroutine 99 [running]:\n", 4000)}

	raw := IssueURL("https://github.com/corgan2222/rig-exporter", r, Machine{})
	if len(raw) > 16000 {
		t.Errorf("the prepared link is %d characters, too long for a browser", len(raw))
	}
}

// The report is published by a person, and the one thing in it that names that
// person is the Windows account in every path. It is of no diagnostic use and
// on many machines it is a real name.
func TestTheUsersOwnNameIsTakenOutOfTheReport(t *testing.T) {
	text := `panic: open C:\Users\Stefan Knaak\AppData\Roaming\rig-exporter\config.json: denied
	D:/Coding/rig-exporter/main.go:99
config=C:/Users/stefan/AppData/Local/Temp/x.json`

	got := Scrub(text)
	for _, name := range []string{"Stefan", "stefan"} {
		if strings.Contains(got, name) {
			t.Errorf("the account name %q survived: %s", name, got)
		}
	}
	// The path itself has to stay readable, or the report says nothing.
	if !strings.Contains(got, `AppData\Roaming\rig-exporter\config.json`) {
		t.Errorf("the path was destroyed rather than anonymised: %s", got)
	}
	if !strings.Contains(got, "D:/Coding/rig-exporter/main.go:99") {
		t.Error("a path that names nobody was scrubbed anyway")
	}
}

// Nothing writes a credential to the log today. This is the backstop for the
// log line somebody adds in two years without thinking about where it ends up.
func TestAnythingShapedLikeACredentialIsRemoved(t *testing.T) {
	text := "connecting host=broker.local username=stefan password=hunter2 token: abc123 secret=s3cr3t"

	got := Scrub(text)
	for _, leaked := range []string{"hunter2", "abc123", "s3cr3t"} {
		if strings.Contains(got, leaked) {
			t.Errorf("%q survived: %s", leaked, got)
		}
	}
	// The host is where the fault often is, and stays.
	if !strings.Contains(got, "host=broker.local") {
		t.Errorf("the host was removed as well: %s", got)
	}
}

// A path that has been through the structured log comes back with its
// backslashes doubled, because slog quotes the value. The account name is just
// as exposed in that copy, and it is the copy that reaches a crash report.
func TestADoubledBackslashPathIsScrubbedToo(t *testing.T) {
	logged := `level=ERROR msg="the previous session ended" summary="panic: open C:\Users\admin\AppData\Roaming\file"`

	got := Scrub(logged)
	if strings.Contains(got, "admin") {
		t.Errorf("the account name survived the doubled separators: %s", got)
	}
	if !strings.Contains(got, "%USER%") {
		t.Errorf("nothing was replaced at all: %s", got)
	}
}
