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
)

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
func Arm(dir, version, build string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crash log directory: %w", err)
	}
	path := filepath.Join(dir, currentName)

	r := &Recorder{dir: dir}
	r.previous = rotate(path, dir)

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
func rotate(path, dir string) *Report {
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
	kept := filepath.Join(dir, fmt.Sprintf("crash-%s.log", stamp.Format("2006-01-02-150405")))
	if err := os.Rename(path, kept); err != nil {
		// Keeping the report matters more than keeping the file name. If the
		// rename fails the record still gets reported from memory; it will be
		// overwritten by the truncate that follows, which is the lesser loss.
		kept = path
	}
	prune(dir)

	return &Report{
		Kind: kind, At: at, Version: version, Build: build,
		Path: kept, Text: string(raw),
	}
}

// prune keeps the newest reports and deletes the rest.
func prune(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "crash-*.log"))
	if err != nil || len(matches) <= keptReports {
		return
	}
	// The names carry the timestamp, so sorting them sorts by age.
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keptReports] {
		_ = os.Remove(old)
	}
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
