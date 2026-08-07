//go:build windows

package webui

import (
	"net/http"
	"strings"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/crashlog"
	"github.com/corgan2222/rig-exporter/internal/metrics"
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
func crashIssueURL(st app.Status) string {
	if st.Crash == nil || !st.Config.CrashReportOffered {
		return ""
	}
	// An unclean exit is not a bug to report. Somebody ended the task, or the
	// power went, and there is no stack for a maintainer to read.
	if st.Crash.Kind != crashlog.KindPanic {
		return ""
	}
	return crashlog.IssueURL(config.ProjectURL, *st.Crash, machineFor(st))
}

// handleCrash serves the full record as plain text.
//
// Plain text on purpose: this is evidence, and a browser that renders it as a
// document would reflow the one thing that has to stay exactly as written.
func (s *Server) handleCrash(w http.ResponseWriter, r *http.Request) {
	report := s.app.Status().Crash
	if report == nil {
		http.NotFound(w, r)
		return
	}

	text := report.Text
	// Prefer the file: it is the whole record, where what is held in memory is
	// only what was there when this session started.
	if report.Path != "" {
		if full, err := crashlog.ReadReport(report.Path); err == nil && full != "" {
			text = full
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(text))
}
