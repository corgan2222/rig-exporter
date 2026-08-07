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
}

// issueBodyLimit keeps the prepared report inside what a browser will carry in
// a URL. GitHub takes a long query string but not an unbounded one, and a
// goroutine dump from a busy process runs to tens of kilobytes. The full record
// stays on disk; the button is for the part that fits.
const issueBodyLimit = 4000

// IssueBody renders the report the way a maintainer wants to read it.
func IssueBody(r Report, m Machine) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**What happened:** %s\n\n", r.Summary())

	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Version | %s", r.Version)
	if r.Build != "" {
		fmt.Fprintf(&b, " (build %s)", r.Build)
	}
	b.WriteString(" |\n")
	if !r.At.IsZero() {
		fmt.Fprintf(&b, "| Session started | %s |\n", r.At.Format(time.RFC3339))
	}
	if m.OS != "" {
		fmt.Fprintf(&b, "| Windows | %s |\n", m.OS)
	}
	if m.Hypervisor != "" {
		fmt.Fprintf(&b, "| Virtual machine | %s |\n", m.Hypervisor)
	}
	if m.CPU != "" {
		fmt.Fprintf(&b, "| Processor | %s |\n", m.CPU)
	}
	if m.GPU != "" {
		fmt.Fprintf(&b, "| Graphics | %s |\n", m.GPU)
	}
	fmt.Fprintf(&b, "| Elevated | %t |\n", m.Elevated)

	b.WriteString("\n<details><summary>Crash record</summary>\n\n```\n")
	b.WriteString(truncate(r.Text, issueBodyLimit))
	b.WriteString("\n```\n</details>\n")
	return b.String()
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

// IssueURL is a GitHub "new issue" page with the report already filled in.
//
// A prepared page rather than a posted issue, and that is the whole point: the
// report carries the machine's name, its hardware and its Windows build, and
// publishing that is the user's decision to make on a page where they can read
// every word first. Nothing here needs a token, so nothing here can leak one.
func IssueURL(projectURL string, r Report, m Machine) string {
	title := fmt.Sprintf("Crash in %s: %s", r.Version, r.Summary())
	if len(title) > 160 {
		title = title[:157] + "…"
	}
	query := neturl.Values{}
	query.Set("title", title)
	query.Set("body", IssueBody(r, m))
	query.Set("labels", "crash")
	return strings.TrimSuffix(projectURL, "/") + "/issues/new?" + query.Encode()
}
