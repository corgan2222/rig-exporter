//go:build windows

package webui

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/crashlog"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// machineFor is the handful of facts that make a crash report worth reading.
//
// Read out of the last snapshot rather than gathered again: it is already
// there, it is already the truth about this machine, and a crash report is not
// worth a second pass over the hardware.
//
// What it deliberately does not read is the configuration. That holds a broker
// password and three API tokens, and this text is about to be pasted into a
// public issue.
func machineFor(st app.Status) crashlog.Machine {
	return crashlog.Machine{
		OS:         st.Snapshot.Str(metrics.OSVersion.ID),
		Hypervisor: st.Snapshot.Str(metrics.Hypervisor.ID),
		CPU:        st.Snapshot.Str(metrics.CPUModel.ID),
		// Not Str: the card's name carries an instance, because a machine can
		// have two, and Str only finds the readings that have none. A crash
		// report from a graphics tool that does not say which graphics card is
		// missing the first thing anybody would ask.
		GPU:      strings.Join(textsOf(st, metrics.GPUName.ID), " + "),
		Elevated: crashlog.Elevated(),
		Locale:   winapi.UILanguage(),
		// What actually answered. A crash with a vendor library loaded is a
		// different bug from one without, and those are reached through
		// function-pointer tables — where this kind of fault comes from.
		Sources: st.Snapshot.Str(metrics.GPUSource.ID),
	}
}

// textsOf collects every instance of one text measurement, in the order the
// collector produced them.
func textsOf(st app.Status, id string) []string {
	var out []string
	for _, r := range st.Snapshot.Readings {
		if r.Def.ID == id && r.Text != "" {
			out = append(out, r.Text)
		}
	}
	return out
}

// crashIssueURL is the prepared GitHub page for the pending report, or empty
// when there is nothing to report or the user does not want the offer.
//
// Offered for a session that simply vanished as well, not only for one that
// left a stack. That was the other way round at first, on the argument that a
// hard kill is nobody's bug — and it had the case backwards. A program that
// disappears without a message is the failure this whole mechanism was built
// for, and the record still carries the build, the machine, what was answering
// and the last two hundred lines of the log. What it cannot say is whether
// somebody ended the task on purpose; the banner asks that in words, and the
// person reading it is the one who knows.
func crashIssueURL(st app.Status) string {
	if st.Crash == nil || !st.Config.CrashReportOffered {
		return ""
	}
	return crashlog.IssueURL(config.ProjectURL, *st.Crash, machineFor(st))
}

// handleCrash serves the full record as plain text, to read in the browser.
//
// Plain text on purpose: this is evidence, and a browser that renders it as a
// document would reflow the one thing that has to stay exactly as written.
func (s *Server) handleCrash(w http.ResponseWriter, r *http.Request) {
	s.serveCrash(w, r, false)
}

// handleCrashDownload serves the same record as a file to keep. The issue form
// asks for it as an attachment, and asking somebody to find it in AppData first
// is where a bug report stops being written.
func (s *Server) handleCrashDownload(w http.ResponseWriter, r *http.Request) {
	s.serveCrash(w, r, true)
}

func (s *Server) serveCrash(w http.ResponseWriter, r *http.Request, download bool) {
	report := s.app.Status().Crash
	if report == nil {
		http.NotFound(w, r)
		return
	}

	text := report.Text
	// Prefer the file: it is the whole record, where what is held in memory is
	// only what was there when this session started.
	name := "crash.log"
	if report.Path != "" {
		name = filepath.Base(report.Path)
		if full, err := crashlog.ReadReport(report.Path); err == nil && full != "" {
			text = full
		}
	}

	serveRecord(w, name, []byte(text), download)
}
