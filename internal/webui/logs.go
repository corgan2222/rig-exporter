//go:build windows

package webui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/crashlog"
)

// The log files this program writes, and nothing else.
//
// A page that serves a file named in a request is a page that can be asked for
// the wrong file. The defence here is not to sanitise the name but to never
// take one: the handler enumerates what exists, and only serves a name that
// came back from that enumeration. A request for anything else is a 404,
// whatever it is spelled like.
const (
	// logKind marks the running record of what the program is doing.
	logKind = "log"
	// crashKind marks what a session left behind when it did not end on
	// purpose. Shown apart because they mean something different.
	crashKind = "crash"
)

// tailLines is how much of the running log the page shows without being asked.
// Enough to cover a startup and a configuration change, short enough that the
// page still loads as a page rather than as a download.
const tailLines = 200

// logFile is one file on the page.
type logFile struct {
	Name    string
	Size    int64
	ModTime time.Time
	Kind    string
	// Current marks the file being written right now, which is the one whose
	// size will not agree with the directory entry until it is closed.
	Current bool
}

// SizeText is the size in something a person reads.
func (f logFile) SizeText() string {
	switch {
	case f.Size < 1024:
		return fmt.Sprintf("%d B", f.Size)
	case f.Size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(f.Size)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(f.Size)/(1024*1024))
	}
}

// logFilesIn lists what is there, newest first within each kind.
//
// The running log comes first because it is what somebody opening this card is
// usually after; the crash reports follow, newest first, because the one that
// matters is the one that just happened.
func logFilesIn(dir string) []logFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	logName := config.AppName + ".log"
	var logs, crashes []logFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}

		file := logFile{Name: name, Size: info.Size(), ModTime: info.ModTime()}
		switch {
		case name == logName:
			file.Kind, file.Current = logKind, true
			logs = append(logs, file)
		case name == logName+".1":
			file.Kind = logKind
			logs = append(logs, file)
		case name == "crash.log":
			// The armed record of this session. Only worth showing when
			// something is in it beyond the header — normally it holds one
			// line and means nothing has gone wrong.
			file.Kind, file.Current = crashKind, true
			if info.Size() > 0 {
				crashes = append(crashes, file)
			}
		case strings.HasPrefix(name, "crash-") && strings.HasSuffix(name, ".log"):
			file.Kind = crashKind
			crashes = append(crashes, file)
		}
	}

	// The current log first, then its backup; crash reports newest first.
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].Current && !logs[j].Current })
	sort.Slice(crashes, func(i, j int) bool { return crashes[i].Name > crashes[j].Name })
	return append(logs, crashes...)
}

// logFiles is the same for the directory this program actually uses.
func logFiles() []logFile {
	dir, err := config.Dir()
	if err != nil {
		return nil
	}
	return logFilesIn(dir)
}

// runningLogTail is what the card shows without being asked.
func runningLogTail() string {
	path, err := config.LogPath()
	if err != nil {
		return ""
	}
	return applog.Tail(path, tailLines)
}

// logLine is one line of the log and how loudly it was said.
//
// The level is separated out here rather than matched in the browser, so the
// line itself stays a plain string that the template escapes. A log carries
// whatever a device called itself, and a page that built its own markup out of
// that would be taking dictation from a graphics driver.
type logLine struct {
	Level string
	Text  string
}

// levels are the ones slog writes, lowercased for a CSS class.
var levels = map[string]string{
	"level=ERROR": "error",
	"level=WARN":  "warn",
	"level=INFO":  "info",
	"level=DEBUG": "debug",
}

// logLinesOf splits the tail into lines and tags each with its level.
func logLinesOf(tail string) []logLine {
	if tail == "" {
		return nil
	}
	split := strings.Split(tail, "\n")
	out := make([]logLine, 0, len(split))
	for _, text := range split {
		line := logLine{Text: text, Level: "info"}
		for marker, level := range levels {
			if strings.Contains(text, marker) {
				line.Level = level
				break
			}
		}
		// A continuation line — a wrapped message, a stack frame — inherits
		// nothing, so it is not shouted at for having the word ERROR in a file
		// name it happens to mention.
		if !strings.HasPrefix(text, "time=") {
			line.Level = "cont"
		}
		// Red is kept for what earns it, and it wins over everything above.
		// slog has no level beyond ERROR, so the distinction is made on
		// content: a panic or a fatal error is the one thing in a record that
		// means the program stopped.
		if strings.Contains(text, "panic:") || strings.Contains(text, "fatal error:") ||
			strings.Contains(text, "ended without shutting down") {
			line.Level = "critical"
		}
		out = append(out, line)
	}
	return out
}

// handleClearLogs removes the records that are no longer being written.
//
// The running log stays: it is open, Windows will not delete an open file, and
// a button that silently fails is worse than one that says what it does. The
// label says "kept records", which is what this is.
func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	dir, err := config.Dir()
	if err != nil {
		http.Redirect(w, r, "/export#logs", http.StatusSeeOther)
		return
	}

	removed := 0
	for _, file := range logFilesIn(dir) {
		if file.Current {
			continue
		}
		if err := os.Remove(filepath.Join(dir, file.Name)); err == nil {
			removed++
		}
	}
	s.log.Info("kept log records removed", "count", removed)

	// The pending crash banner goes with them: its file is gone, so leaving
	// the banner would point at nothing.
	s.app.DismissCrash()
	http.Redirect(w, r, "/export#logs", http.StatusSeeOther)
}

// handleLogIssue opens the prepared GitHub form for one kept crash report.
//
// Per file rather than only for the pending one: a crash from last week is
// still worth reporting, and the banner that announced it is long gone. The
// report is rebuilt from the file, so what is offered is exactly what is on
// disk — and the redirect is where it ends, because a prepared page is not a
// submitted issue.
func (s *Server) handleLogIssue(w http.ResponseWriter, r *http.Request) {
	wanted := r.PathValue("name")

	var file logFile
	for _, candidate := range logFiles() {
		if candidate.Name == wanted && candidate.Kind == crashKind {
			file = candidate
			break
		}
	}
	if file.Name == "" {
		http.NotFound(w, r)
		return
	}

	dir, err := config.Dir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, file.Name))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	report := crashlog.ReportOf(string(raw), filepath.Join(dir, file.Name))
	if report.Kind != crashlog.KindPanic {
		// A hard kill has no stack and nothing for a maintainer to read.
		// Sending somebody to GitHub with it wastes two people's time.
		http.Redirect(w, r, "/export#logs", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r,
		crashlog.IssueURL(config.ProjectURL, report, machineFor(s.app.Status())),
		http.StatusSeeOther)
}

// handleLog serves one log file as text.
//
// The name is checked against the enumeration rather than cleaned: a name that
// did not come out of the directory listing is not served, so there is no path
// to traverse and no pattern to outwit.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	wanted := r.PathValue("name")

	var found bool
	for _, file := range logFiles() {
		if file.Name == wanted {
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	dir, err := config.Dir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, wanted))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Text, and told not to be sniffed into anything else: this is a record,
	// and a browser deciding it is a document would reflow it.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}
