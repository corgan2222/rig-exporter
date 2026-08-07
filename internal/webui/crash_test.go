//go:build windows

package webui

import (
	"net/http"
	neturl "net/url"
	"strings"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/crashlog"
)

func panicReport() *crashlog.Report {
	return &crashlog.Report{
		Kind:    crashlog.KindPanic,
		At:      time.Date(2026, 8, 7, 15, 40, 0, 0, time.UTC),
		Version: "1.9.2", Build: "167.abc",
		Path: `C:\Users\admin\AppData\Roaming\rig-exporter\crash-2026-08-07-154000.log`,
		Text: "panic: runtime error: index out of range [21] with length 20\n\ngoroutine 42 [running]:\n",
	}
}

// Nothing crashed, nothing to say. A banner that is there every day is a
// banner nobody reads on the day it matters.
func TestNoBannerWhenNothingCrashed(t *testing.T) {
	_, ts := newServer(t, nil)

	if _, body := get(t, ts.URL+"/"); strings.Contains(body, `id="crash-banner"`) {
		t.Error("a clean start showed a crash banner")
	}
}

// The banner has to be the first thing on the page, name the fault, and say
// which build it was — after an update that is not the build showing it.
func TestACrashIsTheFirstThingOnTheDashboard(t *testing.T) {
	server, ts := newServer(t, nil)
	server.app.SetCrash(panicReport())

	_, body := get(t, ts.URL+"/")

	banner := strings.Index(body, `id="crash-banner"`)
	if banner < 0 {
		t.Fatal("the crash is not shown at all")
	}
	if status := strings.Index(body, `id="status"`); banner > status {
		t.Error("the crash banner sits below the status card")
	}
	for _, want := range []string{"index out of range", "1.9.2", "167.abc", `href="/crash"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the banner does not mention %q", want)
		}
	}
}

// A session that simply vanished is offered as a bug report too.
//
// This was the other way round at first, on the argument that a hard kill is
// nobody's bug. That had the case backwards: a program that disappears without
// a message is the failure this whole mechanism exists for, and the record
// still carries the build, the machine and the last two hundred log lines.
// Whether somebody ended the task on purpose is the one thing the program
// cannot know — the banner asks it in words, and the reader is who knows.
func TestAVanishedSessionIsOfferedAsAnIssueToo(t *testing.T) {
	server, ts := newServer(t, nil)
	server.app.SetCrash(&crashlog.Report{
		Kind: crashlog.KindUnclean, Version: "1.9.2",
		At:   time.Now(),
		Text: "rig-exporter session 2026-08-07T15:40:00+02:00 version=1.9.2 build=x pid=1\n",
	})

	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, `id="crash-banner"`) {
		t.Fatal("an unclean exit was not shown at all")
	}
	if !strings.Contains(body, "issues/new") {
		t.Error("a session that vanished cannot be reported")
	}
	if !strings.Contains(body, `href="/crash/download"`) {
		t.Error("the record cannot be downloaded from the banner")
	}
}

// The banner offers all four: read it, keep it, report it, put it away.
func TestTheBannerOffersEveryWayOnWithTheRecord(t *testing.T) {
	server, ts := newServer(t, nil)
	server.app.SetCrash(panicReport())

	_, body := get(t, ts.URL+"/")
	for _, want := range []string{
		`href="/crash"`, `href="/crash/download"`, "issues/new",
		`name="what" value="crash"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the banner is missing %q", want)
		}
	}
}

// The issue form asks for the record as an attachment. Somebody who has to find
// AppData first writes the report from memory instead, or not at all.
func TestTheRecordCanBeDownloadedUnderItsOwnName(t *testing.T) {
	server, ts := newServer(t, nil)
	report := panicReport()
	report.Path = "" // nothing on disk in a test; the held text is the record
	server.app.SetCrash(report)

	resp, err := http.Get(ts.URL + "/crash/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /crash/download = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}
}

// And under the name of the kept file when there is one, because that name says
// which machine and which moment.
func TestTheDownloadIsNamedAfterTheKeptFile(t *testing.T) {
	server, ts := newServer(t, nil)
	server.app.SetCrash(panicReport())

	resp, err := http.Get(ts.URL + "/crash/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "crash-2026-08-07-154000.log") {
		t.Errorf("Content-Disposition = %q, want the kept file's name", got)
	}
}

// The offer is a setting; the record is not.
func TestTheIssueButtonFollowsTheSetting(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) { c.CrashReportOffered = false })
	server.app.SetCrash(panicReport())

	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, `id="crash-banner"`) {
		t.Error("switching the offer off hid the crash itself")
	}
	if strings.Contains(body, "issues/new") {
		t.Error("the button is there although the offer is switched off")
	}
}

// The prepared issue carries the machine's facts on purpose — and must never
// carry a secret. The configuration holds a broker password and three tokens,
// and this text is one click away from a public repository.
func TestThePreparedIssueCarriesNoSecret(t *testing.T) {
	server, ts := newServer(t, func(c *config.Config) {
		c.MQTTPassword = "hunter2-broker-secret"
		c.DataToken = "data-token-secret"
		c.InfluxToken = "influx-token-secret"
	})
	server.app.SetCrash(panicReport())

	_, body := get(t, ts.URL+"/")
	for _, secret := range []string{"hunter2-broker-secret", "data-token-secret", "influx-token-secret"} {
		if strings.Contains(body, secret) {
			t.Errorf("the prepared report carries %q", secret)
		}
		if strings.Contains(neturl.QueryEscape(body), neturl.QueryEscape(secret)) {
			t.Errorf("the prepared report carries %q, escaped", secret)
		}
	}
}

// Dismissing is for this session and does not touch the file: somebody who
// clicked it by accident can still find the report on disk.
func TestDismissingTheCrashHidesItWithoutTouchingTheRecord(t *testing.T) {
	server, ts := newServer(t, nil)
	report := panicReport()
	server.app.SetCrash(report)

	resp := post(t, ts.URL, "/dismiss", neturl.Values{"what": {"crash"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if _, body := get(t, ts.URL+"/"); strings.Contains(body, `id="crash-banner"`) {
		t.Error("the banner came back after it was dismissed")
	}
	if server.app.Status().Crash != nil {
		t.Error("the report is still pending")
	}
}

// The record is evidence. It is served exactly as written, and as text, so a
// browser does not reflow the one thing that has to stay as it is.
func TestTheRecordIsServedVerbatim(t *testing.T) {
	server, ts := newServer(t, nil)
	report := panicReport()
	report.Path = "" // nothing on disk in a test; the held text is the record
	server.app.SetCrash(report)

	code, body := get(t, ts.URL+"/crash")
	if code != http.StatusOK {
		t.Fatalf("GET /crash = %d", code)
	}
	if !strings.Contains(body, "goroutine 42 [running]:") {
		t.Error("the record was not served in full")
	}
}

func TestTheRecordIsNotServedWhenThereIsNone(t *testing.T) {
	_, ts := newServer(t, nil)

	if code, _ := get(t, ts.URL+"/crash"); code != http.StatusNotFound {
		t.Errorf("GET /crash = %d, want 404", code)
	}
}
