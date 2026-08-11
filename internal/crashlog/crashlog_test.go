package crashlog

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

// The machine belongs in the name. These files get attached to issues and
// dropped into chats; once one has been moved, the name is the only thing left
// that says which PC it came from.
func TestAKeptReportIsNamedAfterTheMachineAndTheMoment(t *testing.T) {
	at := time.Date(2026, 8, 7, 16, 38, 51, 0, time.UTC)

	got := ReportName("corgan-pc3", at)
	want := "rig-exporter_corgan-pc3_crashreport_2026-08-07_16-38-51.log"
	if got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if !IsReportName(got) {
		t.Error("the name this package writes is not one it recognises")
	}
}

// A host name can be almost anything; a file name cannot.
func TestAnAwkwardHostNameStillProducesAUsableFileName(t *testing.T) {
	at := time.Date(2026, 8, 7, 16, 38, 51, 0, time.UTC)

	for _, host := range []string{`WORK GROUP\PC`, "büro:pc/1", "田中のPC", ""} {
		name := ReportName(host, at)
		if strings.ContainsAny(name, `\/:*?"<>|`) {
			t.Errorf("host %q produced an unusable name: %q", host, name)
		}
		if !IsReportName(name) {
			t.Errorf("host %q produced a name that is not recognised: %q", host, name)
		}
	}
}

// Reports written before the rename are still reports. Dropping them from the
// list would lose exactly the crashes somebody is trying to look up.
func TestTheNameFromBeforeTheRenameIsStillRecognised(t *testing.T) {
	if !IsReportName("crash-2026-08-07-163851.log") {
		t.Error("a report from the previous naming is no longer recognised")
	}
}

// A record of a session that simply vanished stays that, however often the log
// underneath it mentions the word panic.
//
// This program writes `the previous session ended without shutting down
// kind=panic summary="panic: …"` on every start after a crash, and that line
// travels inside the next record's log tail. Reading the whole file made a hard
// kill look like a panic whose stack was missing, and the prepared issue came
// out titled "unknown crash". Taken from a real file on this machine.
func TestALogMentioningAnOlderPanicDoesNotMakeThisOneAPanic(t *testing.T) {
	report := ReportOf(strings.Join([]string{
		"rig-exporter session 2026-08-07T17:25:37+02:00 version=1.9.2 build=175 pid=36640",
		"",
		logMarker,
		`time=2026-08-07T15:50:29.656+02:00 level=ERROR msg="the previous session ended ` +
			`without shutting down" kind=panic summary="panic: runtime error: index out of range"`,
		`time=2026-08-07T15:50:30.101+02:00 level=INFO msg=starting version=1.9.2`,
	}, "\n"), "")

	if report.Kind != KindUnclean {
		t.Errorf("kind = %q, want %q", report.Kind, KindUnclean)
	}
	if got := report.Summary(); got != "the process ended without shutting down" {
		t.Errorf("summary = %q", got)
	}
}

// And the other way round: a real stack is still found when a log follows it.
func TestARealStackIsStillFoundWithALogUnderneath(t *testing.T) {
	report := ReportOf(strings.Join([]string{
		"rig-exporter session 2026-08-07T17:02:15+02:00 version=1.9.2 build=demo pid=40344",
		"panic: assignment to entry in nil map",
		"",
		"goroutine 8 [running]:",
		logMarker,
		`time=2026-08-07T17:02:14.900+02:00 level=INFO msg=starting version=1.9.2`,
	}, "\n"), "")

	if report.Kind != KindPanic {
		t.Errorf("kind = %q, want %q", report.Kind, KindPanic)
	}
	if got := report.Summary(); got != "panic: assignment to entry in nil map" {
		t.Errorf("summary = %q", got)
	}
}

// The time in the name is the time the list is sorted by, so it has to be
// readable again — out of both namings, because a folder holds both.
func TestTheTimeCanBeReadBackOutOfEitherName(t *testing.T) {
	at := time.Date(2026, 8, 7, 16, 38, 51, 0, time.Local)

	for _, name := range []string{
		ReportName("corgan-pc3", at),
		"crash-2026-08-07-163851.log",
	} {
		got, ok := StampOf(name)
		if !ok {
			t.Errorf("%q carries no readable time", name)
			continue
		}
		if !got.Equal(at) {
			t.Errorf("%q = %s, want %s", name, got, at)
		}
	}
}

// A machine may be called crashreport, and the name still has to be read from
// the right end.
func TestTheHostNameCannotBeMistakenForTheMarker(t *testing.T) {
	at := time.Date(2026, 8, 7, 16, 38, 51, 0, time.Local)

	got, ok := StampOf(ReportName("crashreport", at))
	if !ok || !got.Equal(at) {
		t.Errorf("a machine called crashreport confused the name: %s, %v", got, ok)
	}
}

// Anything else says so rather than answering with the zero time, which would
// sort as the year 1 and quietly become the oldest record in the folder.
func TestANameWithoutATimeSaysSo(t *testing.T) {
	for _, name := range []string{
		"rig-exporter.log", "crash.log", "config.json",
		"rig-exporter_corgan-pc3_crashreport_yesterday.log",
		"crash-not-a-date.log",
	} {
		if _, ok := StampOf(name); ok {
			t.Errorf("%q was read as carrying a time", name)
		}
	}
}

// And nothing else in that directory is.
func TestNothingElseIsTakenForAReport(t *testing.T) {
	for _, name := range []string{
		"config.json", "config.json.bak", "rig-exporter.log", "rig-exporter.log.1",
		"crash.log", "probe.txt", "update-signing-key.pem",
		"rig-exporter_corgan-pc3_crashreport_2026-08-07_16-38-51.txt",
	} {
		if IsReportName(name) {
			t.Errorf("%q was taken for a crash report", name)
		}
	}
}

// The cut has to land between characters, not inside one.
//
// truncate slices by byte against a byte budget, so a multi-byte rune sitting
// across either offset is halved. What fills the log field is the tail of the
// application log, and Windows hands device and drive names to this program in
// the display language — an umlaut in a drive label is the ordinary case on a
// German or Japanese install, not an exotic one.
//
// The broken pair then goes through url.Values.Encode into the prepared issue
// link, and GitHub renders it as a replacement character in the one field whose
// job is to be read literally as evidence. The repository already has the
// correct shape for this: tray.go truncates on runes.
func TestTruncateCutsBetweenCharacters(t *testing.T) {
	body := strings.Repeat("Grafikkarte-Zähler-Überlauf bei 87 °C\n", 60)

	bad := 0
	for limit := 200; limit < 1200; limit++ {
		got := truncate(body, limit)
		if !utf8.ValidString(got) {
			bad++
			continue
		}
		if len(got) > limit {
			t.Fatalf("truncate(limit=%d) returned %d bytes; backing off to a rune boundary may only shrink the result",
				limit, len(got))
		}
	}
	if bad != 0 {
		t.Errorf("%d of 1000 limits produced invalid UTF-8", bad)
	}

	// The budget that actually ships is the one that matters.
	if got := truncate(body, logBudget); !utf8.ValidString(got) {
		t.Errorf("truncate at the shipped logBudget of %d produced invalid UTF-8", logBudget)
	}
	if got := truncate(body, panicBudget); !utf8.ValidString(got) {
		t.Errorf("truncate at the shipped panicBudget of %d produced invalid UTF-8", panicBudget)
	}

	// A text that fits is returned untouched, budgets or not.
	const short = "kurz genug, mit Ümlaut"
	if got := truncate(short, logBudget); got != short {
		t.Errorf("truncate rewrote a text that fits: %q", got)
	}
}

// A machine name that survives nothing must still name a machine.
//
// safeName keeps [A-Za-z0-9-] and trims hyphens off both ends, so a host
// written entirely in Cyrillic, Greek or Japanese trims to nothing. The
// empty-host fallback sits before that trim and never sees the result, so the
// report came out as rig-exporter__crashreport_… — the machine part missing
// from a naming scheme whose only purpose is telling machines apart.
//
// Windows permits non-ASCII computer names, and localised installs are the
// population the report's Locale field exists to serve.
func TestSafeNameAlwaysNamesTheMachine(t *testing.T) {
	for _, host := range []string{"СЕРВЕР", "パソコン", "Σ", "---", "___"} {
		got := safeName(host)
		if got == "" {
			t.Errorf("safeName(%q) = %q; the report would lose the machine", host, got)
		}
		if strings.Trim(got, "-") == "" {
			t.Errorf("safeName(%q) = %q, which is only separators", host, got)
		}
	}

	// Two different machines must not collapse onto one name, or a folder of
	// reports from several PCs is exactly the folder the scheme exists to
	// avoid.
	if a, b := safeName("СЕРВЕР"), safeName("パソコン"); a == b {
		t.Errorf("two different machines both became %q", a)
	}

	// Stable across calls: the name goes into a file name, not a session id.
	if a, b := safeName("СЕРВЕР"), safeName("СЕРВЕР"); a != b {
		t.Errorf("safeName is not stable: %q then %q", a, b)
	}

	// The ordinary case is untouched, and a genuinely empty host keeps the
	// answer it always had.
	if got := safeName("corgan-pc3"); got != "corgan-pc3" {
		t.Errorf("safeName(%q) = %q", "corgan-pc3", got)
	}
	if got := safeName(""); got != "unknown" {
		t.Errorf("safeName(\"\") = %q, want unknown", got)
	}

	// Whatever it produces has to survive the round trip, or the report cannot
	// be recognised as one afterwards.
	for _, host := range []string{"СЕРВЕР", "corgan-pc3", ""} {
		name := ReportName(host, time.Date(2026, 8, 8, 3, 4, 5, 0, time.UTC))
		if !IsReportName(name) {
			t.Errorf("ReportName(%q) produced %q, which IsReportName rejects", host, name)
		}
	}
}
