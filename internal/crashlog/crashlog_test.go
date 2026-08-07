package crashlog

import (
	neturl "net/url"
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

// The report is about to be published by somebody. It carries the machine's
// name and hardware on purpose — and must never carry a secret, which is why
// it is built from a fixed list of facts rather than from the configuration.
func TestTheReportCarriesTheFactsAndNothingElse(t *testing.T) {
	r := Report{
		Kind: KindPanic, Version: "1.9.2", Build: "170.abc",
		At:   time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC),
		Text: "panic: boom\n\ngoroutine 1 [running]:\n",
	}
	m := Machine{OS: "Windows 10 Pro 22H2", CPU: "AMD Ryzen 9 5950X", GPU: "NVIDIA GeForce RTX 5070 Ti", Elevated: false}

	body := IssueBody(r, m)
	for _, want := range []string{"1.9.2", "170.abc", "Windows 10 Pro 22H2", "AMD Ryzen 9 5950X", "panic: boom"} {
		if !strings.Contains(body, want) {
			t.Errorf("the report does not mention %q", want)
		}
	}
	// A hypervisor line only appears on a machine that has one.
	if strings.Contains(body, "Virtual machine") {
		t.Error("a bare-metal machine was reported as virtual")
	}
}

// The button opens a page with the report filled in. It must be a real URL
// pointing at this project, and it must survive a report full of characters
// that mean something in a query string.
func TestTheIssueURLIsUsable(t *testing.T) {
	r := Report{Kind: KindPanic, Version: "1.9.2",
		Text: "panic: bad & ugly ?query #fragment\n"}

	raw := IssueURL("https://github.com/corgan2222/rig-exporter", r, Machine{})

	parsed, err := neturl.Parse(raw)
	if err != nil {
		t.Fatalf("the prepared link is not a URL: %v", err)
	}
	if parsed.Path != "/corgan2222/rig-exporter/issues/new" {
		t.Errorf("path = %q", parsed.Path)
	}
	body := parsed.Query().Get("body")
	if !strings.Contains(body, "bad & ugly ?query #fragment") {
		t.Error("the characters that mean something in a URL did not survive")
	}
	if parsed.Query().Get("labels") != "crash" {
		t.Error("the issue is not labelled")
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
