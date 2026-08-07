// Package crashlog makes a crash impossible to miss.
//
// This binary is linked with -H windowsgui, which buys a start without a
// console flashing up and costs the process its standard error. Go writes a
// panic — the message and the stack of every goroutine — straight to that
// stream, so on a GUI build the most valuable thing the runtime ever prints
// goes to a handle that discards it. The program vanishes, the log file says
// nothing, and Windows records nothing either: the runtime handles the fault
// itself, so there is not even an entry in the event log.
//
// The fix is to give the process a standard error again, pointed at a file.
// Verified rather than assumed, on a binary linked exactly like the shipped
// one: a panic in main, a panic inside a goroutine and a runtime fault all
// land in the file with a full stack.
//
// That covers more than a recover ever could. A recover in main cannot see a
// goroutine's panic, and nothing in Go can recover from a fatal runtime error;
// both reach this file.
package crashlog

import (
	"fmt"
	neturl "net/url"
	"regexp"
	"strings"
	"time"
)

// Kind says what the previous run's record means.
type Kind string

const (
	// KindPanic is a stack: the program died of a bug and said where.
	KindPanic Kind = "panic"
	// KindUnclean is a session that started and never ended — killed from the
	// task manager, cut off by a power failure, or stopped by a debugger.
	// Worth telling apart from a panic: there is nothing to fix in the code.
	KindUnclean Kind = "unclean"
)

// Report is what the previous run left behind.
type Report struct {
	Kind Kind
	// At is when the crashed session started, not when it died. The moment of
	// death is not knowable after the fact, and pretending otherwise would put
	// a wrong timestamp on a bug report.
	At time.Time
	// Version and Build identify the binary that crashed, which need not be the
	// one reading this: an update can land between the crash and the next start.
	Version string
	Build   string
	// Path is where the full record was kept, for somebody who wants all of it.
	Path string
	// Text is the record itself: the session header and whatever the runtime
	// managed to write.
	Text string
}

// Summary is the first meaningful line of a panic, for a heading.
func (r Report) Summary() string {
	for _, line := range strings.Split(r.Text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "panic:") || strings.HasPrefix(line, "fatal error:") {
			return line
		}
	}
	if r.Kind == KindUnclean {
		return "the process ended without shutting down"
	}
	return "unknown crash"
}

// sessionPrefix opens the file so a later start can tell a crashed session from
// a clean one, and so the record says which binary wrote it.
const sessionPrefix = "rig-exporter session"

// header is the line every session writes when it arms itself.
func header(version, build string, pid int, now time.Time) string {
	return fmt.Sprintf("%s %s version=%s build=%s pid=%d\n",
		sessionPrefix, now.Format(time.RFC3339), version, build, pid)
}

// classify reads a leftover file. An empty one means the last session shut down
// cleanly and wiped its own record.
func classify(text string) (Kind, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	if strings.Contains(text, "panic:") || strings.Contains(text, "fatal error:") ||
		strings.Contains(text, "runtime error:") {
		return KindPanic, true
	}
	return KindUnclean, true
}

// parseHeader pulls the session details back out of the first line.
func parseHeader(text string) (at time.Time, version, build string) {
	line, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(line, sessionPrefix) {
		return time.Time{}, "", ""
	}
	for i, field := range strings.Fields(line) {
		switch {
		case i == 2:
			at, _ = time.Parse(time.RFC3339, field)
		case strings.HasPrefix(field, "version="):
			version = strings.TrimPrefix(field, "version=")
		case strings.HasPrefix(field, "build="):
			build = strings.TrimPrefix(field, "build=")
		}
	}
	return at, version, build
}

// Machine is the handful of facts about a PC that make a crash report worth
// reading. Deliberately a fixed list rather than the configuration: the
// configuration holds a broker password and three API tokens, and a crash
// report is something a person is about to publish.
type Machine struct {
	OS         string
	Hypervisor string
	CPU        string
	GPU        string
	Elevated   bool
	// Sources names what actually answered — Afterburner, NVML, ADLX, PawnIO.
	// A crash with a vendor library loaded is a different bug from one without,
	// and those libraries are reached through function-pointer tables, which is
	// where this kind of fault tends to come from.
	Sources string
}

// logMarker separates the runtime's output from the application log inside one
// record. The two halves go into different fields of the report, so the split
// has to be findable again.
const logMarker = "--- the application log up to this point ---"

// Split returns the runtime's output and the application log separately.
func (r Report) Split() (crash, log string) {
	before, after, found := strings.Cut(r.Text, logMarker)
	if !found {
		return r.Text, ""
	}
	return strings.TrimRight(before, "\n"), strings.TrimLeft(after, "\n")
}

// secretish matches a key=value whose key names something that must never be
// published, whatever the value turns out to be.
//
// Nothing writes a secret to the log today — checked, not assumed. This is here
// because a log line added in two years' time will not remember that its output
// can end up in a public issue, and the cost of being wrong once is somebody
// else's broker.
var secretish = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|apikey|api_key|bearer)\s*[=:]\s*\S+`)

// urlUserinfo matches the credentials in a URL: scheme://user:pass@host.
//
// A key=value filter cannot see these, and they are not hypothetical. The
// broker address is a free text field; somebody who types
// stefan:hunter2@broker.local as the host gets tcp://stefan:hunter2@broker.local
// written to the log twice on every connect, and from there into a report.
// Found by the agent building the issue form, not by this package's own tests —
// which is the argument for having somebody else read it.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@`)

// homePath matches a Windows user profile directory, whichever way the slashes
// happen to lean and however many of them there are.
//
// The repetition is not decoration. A path that has been through the structured
// log comes back with its backslashes doubled, because slog quotes the value —
// so the text to match is C:\\Users\name, not C:\Users\name. A pattern that
// insisted on exactly one separator missed precisely the copy that ends up in a
// crash report, which is how this was found: by reading the prepared report
// rather than by trusting the pattern.
var homePath = regexp.MustCompile(`(?i)([A-Z]:[\\/]+Users[\\/]+)([^\\/\s"']+)`)

// Scrub takes out of a record what a public issue must not carry.
//
// Two things, and only two. The user's own name, which appears in every path on
// a machine where somebody used it as their Windows account and is of no
// diagnostic use at all. And anything shaped like a credential, as a backstop.
//
// What deliberately stays: the host name, drive labels and addresses. Those are
// often exactly where the fault is, and the interface tells the sender they are
// in there before the button is pressed. Taking them out would leave a report
// that cannot be acted on, which is a different way of being useless.
//
// Applied to the kept file as well as to the prepared link, and that correction
// came from outside: the issue form asks the sender to attach
// crash-<timestamp>.log, so a guarantee that held only for the URL would have
// been a guarantee about the wrong artefact. Scrubbing the file costs nothing —
// on the machine that wrote it, C:\Users\%USER%\… says everything the real
// account name would.
func Scrub(text string) string {
	text = homePath.ReplaceAllString(text, "${1}%USER%")
	text = urlUserinfo.ReplaceAllString(text, "${1}<removed>@")
	return secretish.ReplaceAllStringFunc(text, func(match string) string {
		key, _, _ := strings.Cut(match, "=")
		if !strings.Contains(match, "=") {
			key, _, _ = strings.Cut(match, ":")
		}
		return strings.TrimSpace(key) + "=<removed>"
	})
}

// truncate cuts the middle out rather than the end. The first lines name the
// fault and the last ones hold the goroutines that were waiting, and a report
// that keeps only the beginning throws away half the evidence.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const note = "\n\n… trimmed for the report, the full record is in the file named above …\n\n"
	keep := limit - len(note)
	head := keep * 2 / 3
	tail := keep - head
	return text[:head] + note + text[len(text)-tail:]
}

// The issue form this fills in, and the budget for the two long fields.
//
// The file name is part of the contract: rename the form and this link lands on
// a 404, silently. It carries no number prefix precisely so it never has to be
// renamed when another form is added.
const (
	issueTemplate = "crash-report.yml"
	// The stack names the fault, the log says what led to it. 3000 and 1000
	// was measured against the real form: 4172 characters of raw text became a
	// 5604-character URL, well inside what a browser will open.
	panicBudget = 3000
	logBudget   = 1000
)

// IssueURL is a GitHub "new issue" page with the form already filled in.
//
// A prepared page rather than a posted issue, and that is the whole point: the
// report carries the machine's name, its hardware and its Windows build, and
// publishing that is the user's decision to make on a page where they can read
// every word first. Nothing here needs a token, so nothing here can leak one.
//
// The field names are the contract with the form; they are ids, not labels, and
// a renamed one arrives empty without an error. No labels are set here either:
// the form declares its own, and a label named in a URL that does not exist in
// the repository is discarded by GitHub without a word.
func IssueURL(projectURL string, r Report, m Machine) string {
	crash, log := r.Split()

	query := neturl.Values{}
	query.Set("template", issueTemplate)
	query.Set("title", title(r))
	// No fences around either of these: both fields declare render: text, so
	// GitHub puts them in a code block itself and ours would show up as
	// literal backticks inside it.
	query.Set("panic", truncate(Scrub(crash), panicBudget))
	query.Set("log", truncate(Scrub(log), logBudget))

	query.Set("build", buildText(r))
	query.Set("windows", m.OS)
	query.Set("platform", platformText(m))
	query.Set("hardware", hardwareText(m))
	query.Set("elevated", yesNo(m.Elevated))
	if m.Sources != "" {
		query.Set("sources", m.Sources)
	}
	// steps and full are left alone. One is the question only a person can
	// answer, the other is where they attach the file.

	return strings.TrimSuffix(projectURL, "/") + "/issues/new?" + query.Encode()
}

func title(r Report) string {
	text := Scrub(fmt.Sprintf("Crash in %s: %s", r.Version, r.Summary()))
	if len(text) > 160 {
		text = text[:157] + "…"
	}
	return text
}

func buildText(r Report) string {
	if r.Build == "" {
		return r.Version
	}
	return r.Version + "+" + r.Build
}

// platformText says whether this ran on metal, and under what if not.
func platformText(m Machine) string {
	if m.Hypervisor == "" {
		return "real"
	}
	return "virtual (" + m.Hypervisor + ")"
}

func hardwareText(m Machine) string {
	parts := make([]string, 0, 2)
	for _, part := range []string{m.CPU, m.GPU} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " · ")
}

func yesNo(on bool) string {
	if on {
		return "yes"
	}
	return "no"
}
