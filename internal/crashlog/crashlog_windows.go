//go:build windows

package crashlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/applog"
)

// logTailLines is how much of the application log is folded into a crash
// report. A crash without it says how the program died; with it, it also says
// what the program was doing — and that is usually the half that identifies the
// bug.
const logTailLines = 200

const (
	// currentName is the file the running session writes to. It is empty while
	// nothing has gone wrong and empty again after a clean shutdown, so its
	// having content at startup is the whole detection.
	currentName = "crash.log"
	// keptReports is how many crashes are kept around. Enough to see a pattern,
	// few enough that nobody has to tidy up after it.
	keptReports = 10
)

// Recorder owns the redirected standard error for the lifetime of a session.
type Recorder struct {
	file *os.File
	dir  string
	// previous is what the last session left behind, or nil when it ended
	// cleanly. Read before this session overwrites the file.
	previous *Report
}

// Arm gives the process a standard error again and hands back whatever the
// previous session left behind.
//
// Call it once, as early as a run can manage, and before anything that might
// fault. It must not run in a second instance that is about to exit: that would
// rotate the crash record of the instance still running.
func Arm(dir, version, build, logPath string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crash log directory: %w", err)
	}
	path := filepath.Join(dir, currentName)

	r := &Recorder{dir: dir}
	r.previous = rotate(path, dir, logPath)

	// Truncated: what is in here from now on belongs to this session alone.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("crash log: %w", err)
	}
	if _, err := file.WriteString(header(version, build, os.Getpid(), time.Now())); err != nil {
		file.Close()
		return nil, fmt.Errorf("crash log header: %w", err)
	}

	// Two different things, and both are needed. SetStdHandle is what the Go
	// runtime asks for when it prints a panic — it calls GetStdHandle at the
	// moment of writing, so this takes effect for every later fault. os.Stderr
	// is what ordinary Go code writes to, and it would otherwise still point at
	// the handle that goes nowhere.
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(file.Fd())); err != nil {
		file.Close()
		return nil, fmt.Errorf("redirect standard error: %w", err)
	}
	os.Stderr = file

	r.file = file
	return r, nil
}

// Previous is the report from the session before this one, or nil.
func (r *Recorder) Previous() *Report {
	if r == nil {
		return nil
	}
	return r.previous
}

// Disarm records that this session ended on purpose.
//
// Emptying the file is the signal: a session that ends cleanly leaves nothing
// behind, so anything found at the next start was left by a session that did
// not get this far.
func (r *Recorder) Disarm() {
	if r == nil || r.file == nil {
		return
	}
	// Truncate rather than delete: the handle is still the process's standard
	// error, and a fault between here and exit should still have somewhere to
	// land.
	if err := r.file.Truncate(0); err != nil {
		return
	}
	_, _ = r.file.Seek(0, 0)
}

// Close releases the file. Standard error is left pointing at it: taking it
// away again would restore the silence for whatever happens during shutdown.
func (r *Recorder) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Sync()
}

// rotate moves a leftover record aside and reads it. A file that is missing or
// holds nothing but whitespace means the last session shut down cleanly.
func rotate(path, dir, logPath string) *Report {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	kind, crashed := classify(string(raw))
	if !crashed {
		return nil
	}

	at, version, build := parseHeader(string(raw))
	stamp := at
	if stamp.IsZero() {
		stamp = time.Now()
	}

	// One file per crash, and each one complete on its own: what the runtime
	// printed, and underneath it what the program had been doing. Whoever picks
	// this file up should not have to find a second one to make sense of it —
	// and by the time anybody looks, the running log has moved on.
	// Scrubbed here, where the file is written, and not only where a link is
	// built. The issue form asks the sender to attach this very file, so this
	// is the artefact the promise has to hold for.
	text := Scrub(string(raw) + logSection(logPath))

	// The machine goes in the name: these files get attached to issues and
	// dropped into chats, and once moved the name is all that is left to say
	// which PC they came from.
	host, _ := os.Hostname()
	kept := filepath.Join(dir, ReportName(host, stamp))
	if err := os.WriteFile(kept, []byte(text), 0o644); err != nil {
		// Keeping the report matters more than keeping the file name. The
		// record is still reported from memory; it is the truncate below that
		// takes the original away, which is the lesser loss.
		kept = ""
	}
	prune(dir)

	return &Report{
		Kind: kind, At: at, Version: version, Build: build,
		Path: kept, Text: text,
	}
}

// logSection is the tail of the application log, marked off so nobody mistakes
// it for part of the stack.
func logSection(logPath string) string {
	if logPath == "" {
		return ""
	}
	tail := applog.Tail(logPath, logTailLines)
	if tail == "" {
		return ""
	}
	// Through the constant rather than as a literal. Split looks for logMarker,
	// and two copies of the same string in two files do not hold each other
	// together — the day one of them changes, Split stops finding the divider,
	// returns the whole record as the stack, and the issue link puts the trace
	// and the application log into one field, cut to the stack's budget.
	return "\n\n" + logMarker + "\n" + tail + "\n"
}

// prune keeps the newest reports and deletes the rest.
func prune(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() && IsReportName(entry.Name()) {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) <= keptReports {
		return
	}
	// Oldest first, by the time in the name. Sorting the names as text would do
	// for one naming and stopped doing for two: every crash- name sorts before
	// every rig-exporter_ name, so the older scheme would be deleted first
	// however recent it was.
	sort.Slice(matches, func(i, j int) bool { return olderReport(matches[i], matches[j]) })
	for _, old := range matches[:len(matches)-keptReports] {
		_ = os.Remove(filepath.Join(dir, old))
	}
}

// olderReport orders two report names by the time each one carries. A name
// without a readable time sorts oldest, so an unrecognisable file is the first
// to go rather than the last.
func olderReport(a, b string) bool {
	atA, okA := StampOf(a)
	atB, okB := StampOf(b)
	if okA != okB {
		return !okA
	}
	if !okA || atA.Equal(atB) {
		return a < b
	}
	return atA.Before(atB)
}

// Elevated reports whether this process runs with administrator rights, which
// changes which sensors can be read and is therefore worth a line in a report.
func Elevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(&windows.SECURITY_NT_AUTHORITY, 2,
		windows.SECURITY_BUILTIN_DOMAIN_RID, windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0, &sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	member, err := windows.Token(0).IsMember(sid)
	return err == nil && member
}

// ReadReport loads a kept report back from disk, for showing all of it.
func ReadReport(path string) (string, error) {
	if filepath.Ext(path) != ".log" {
		return "", errors.New("not a crash report")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
