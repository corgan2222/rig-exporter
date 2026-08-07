//go:build windows

package webui

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/config"
)

// stamp gives a file a modification time, so an ordering that must come from
// the clock can be tested without waiting for one.
func stamp(t *testing.T, dir, name string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(filepath.Join(dir, name), at, at); err != nil {
		t.Fatal(err)
	}
}

// writeFiles fills a directory the way the program would.
func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ownLogDir points the program's configuration directory at a temporary one and
// fills it, so a test about serving files does not depend on what happens to be
// in the tester's AppData. os.UserConfigDir reads this variable on Windows.
func ownLogDir(t *testing.T, names ...string) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("AppData", base)

	dir := filepath.Join(base, config.AppName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, names...)
	return dir
}

// The card lists what this program wrote and nothing else. The configuration
// sits in the same directory and holds the broker password.
func TestOnlyTheProgramsOwnRecordsAreListed(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir,
		"rig-exporter.log", "rig-exporter.log.1",
		"rig-exporter_corgan-pc3_crashreport_2026-08-07_15-40-00.log",
		"crash-2026-08-06-101500.log",
		"config.json", "config.json.bak", "probe.txt", "update-signing-key.pem")

	var listed []string
	for _, file := range logFilesIn(dir) {
		listed = append(listed, file.Name)
	}

	// The set is the point here: which files belong to this program and which
	// do not. The order among the crash reports comes from their timestamps and
	// is tested separately.
	want := map[string]bool{
		"rig-exporter.log": true, "rig-exporter.log.1": true,
		"rig-exporter_corgan-pc3_crashreport_2026-08-07_15-40-00.log": true,
		"crash-2026-08-06-101500.log":                                 true,
	}
	if len(listed) != len(want) {
		t.Fatalf("listed %v, want the four records", listed)
	}
	for _, name := range listed {
		if !want[name] {
			t.Errorf("%q is not one of this program's records", name)
		}
	}
	// The running log still comes first, whatever the crashes do.
	if listed[0] != "rig-exporter.log" {
		t.Errorf("the running log is not first: %q", listed[0])
	}
}

// The running log first, then its backup, then the crash reports newest first:
// the one that matters is the one that just happened.
func TestTheNewestCrashIsListedFirst(t *testing.T) {
	dir := t.TempDir()
	newest := "rig-exporter_corgan-pc3_crashreport_2026-08-07_15-40-00.log"
	writeFiles(t, dir, "crash-2026-08-01-100000.log", newest, "rig-exporter.log")

	// The modification times are set against the names on purpose: the newest
	// record is given the oldest one. This is what Windows does by itself for a
	// file that is still open — the directory entry keeps a time from before the
	// last write — so the order has to come from the name, and this is the test
	// that says so. It fails the moment anything sorts by the clock again.
	stamp(t, dir, newest, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stamp(t, dir, "crash-2026-08-01-100000.log", time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))

	files := logFilesIn(dir)
	if files[0].Name != "rig-exporter.log" || !files[0].Current {
		t.Errorf("the running log is not first: %+v", files[0])
	}
	if files[1].Name != newest {
		t.Errorf("the newest crash is not first among the reports: %q", files[1].Name)
	}
	// Both namings sort against each other, which sorting the names as text
	// would not do: every crash- name sorts before every rig-exporter_ one.
	if files[2].Name != "crash-2026-08-01-100000.log" {
		t.Errorf("the older crash is not last: %q", files[2].Name)
	}
	// And the time the page prints is the time the name says, so the column and
	// the file name in the same row cannot contradict each other.
	want := time.Date(2026, 8, 7, 15, 40, 0, 0, time.Local)
	if !files[1].ModTime.Equal(want) {
		t.Errorf("the row says %s, the name says %s", files[1].ModTime, want)
	}
}

// An empty crash.log means the session shut down cleanly. Listing it would put
// a crash on the page every single day.
func TestAnEmptyCurrentCrashRecordIsNotListed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crash.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, file := range logFilesIn(dir) {
		if file.Name == "crash.log" {
			t.Error("an empty crash record was listed as a crash")
		}
	}
}

// The handler serves a name only when that name came out of the listing. This
// is the test that matters: the configuration lives beside the logs and holds
// the broker password and three tokens.
func TestNoFileOutsideTheListingIsServed(t *testing.T) {
	_, ts := newServer(t, nil)

	for _, name := range []string{
		"config.json",
		"config.json.bak",
		"update-signing-key.pem",
		"..%2Fconfig.json",
		"..%5C..%5CWindows%5Cwin.ini",
		"probe.txt",
	} {
		t.Run(name, func(t *testing.T) {
			code, body := get(t, ts.URL+"/logs/"+name)
			if code != http.StatusNotFound {
				t.Errorf("GET /logs/%s = %d, want 404", name, code)
			}
			for _, secret := range []string{"mqtt_password", "influx_token", "PRIVATE KEY"} {
				if strings.Contains(body, secret) {
					t.Errorf("the answer carries %q", secret)
				}
			}
		})
	}
}

// Every file the page lists can also be kept. The issue form asks for the
// record as an attachment, and "open the folder" is the step at which most
// people stop.
func TestEveryListedRecordCanBeDownloaded(t *testing.T) {
	crash := "rig-exporter_corgan-pc3_crashreport_2026-08-07_15-40-00.log"
	ownLogDir(t, "rig-exporter.log", crash)
	_, ts := newServer(t, nil)

	for _, name := range []string{"rig-exporter.log", crash} {
		resp, err := http.Get(ts.URL + "/logs/" + name + "/download")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /logs/%s/download = %d", name, resp.StatusCode)
			continue
		}
		if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, name) {
			t.Errorf("%s: Content-Disposition = %q", name, got)
		}
		if string(body) != "x" {
			t.Errorf("%s: served %q", name, body)
		}
	}
}

// And the download route is no way around the listing. It reads a name out of
// the request just as the other one does, and the configuration with the broker
// password sits in the same directory.
func TestDownloadingAlsoRefusesWhatWasNotListed(t *testing.T) {
	ownLogDir(t, "rig-exporter.log")
	_, ts := newServer(t, nil)

	for _, name := range []string{
		"config.json", "update-signing-key.pem", "..%2Fconfig.json",
	} {
		if code, _ := get(t, ts.URL+"/logs/"+name+"/download"); code != http.StatusNotFound {
			t.Errorf("GET /logs/%s/download = %d, want 404", name, code)
		}
	}
}

// A file name cannot end a header and start another one. Every name that gets
// here was written by this program and would survive unquoted — which is
// exactly the reasoning that stops holding the day somebody adds a caller.
func TestAFileNameCannotBreakOutOfTheHeader(t *testing.T) {
	for _, name := range []string{
		"a\r\nX-Injected: yes.log",
		`a"b.log`,
		"a\\b.log",
		"",
	} {
		got := headerSafe(name)
		if strings.ContainsAny(got, "\r\n\"\\") || got == "" {
			t.Errorf("headerSafe(%q) = %q", name, got)
		}
	}
}

// The card is the last thing on the export page, and shows the running log
// without anybody having to open a folder.
func TestTheExportPageEndsWithTheLogs(t *testing.T) {
	_, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/export")

	logs := strings.Index(body, `id="logs"`)
	if logs < 0 {
		t.Fatal("there is no log card")
	}
	if app := strings.Index(body, `id="app"`); logs < app {
		t.Error("the log card is not below the application card")
	}
	if !strings.Contains(body, `class="logview"`) {
		t.Error("the running log is not shown")
	}
	if !strings.Contains(body, `href="/logs/rig-exporter.log"`) {
		t.Error("the running log cannot be opened in full")
	}
}

// The levels are separated out on the server, so the line itself stays a plain
// string that the template escapes. A log carries whatever a device called
// itself, and building markup out of that would be taking dictation from a
// graphics driver.
func TestEachLineIsTaggedWithItsLevel(t *testing.T) {
	tail := strings.Join([]string{
		`time=2026-08-07T16:00:00+02:00 level=INFO msg=starting`,
		`time=2026-08-07T16:00:01+02:00 level=ERROR msg="render page"`,
		`time=2026-08-07T16:00:02+02:00 level=WARN msg=slow`,
		`time=2026-08-07T16:00:03+02:00 level=DEBUG msg=detail`,
		`    a wrapped continuation with the word ERROR in it`,
	}, "\n")

	got := logLinesOf(tail)
	want := []string{"info", "error", "warn", "debug", "cont"}
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Level != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i].Level, want[i])
		}
	}
}

// Tidying up removes what is finished with and leaves what is still being
// written: Windows will not delete an open file, and a button that half fails
// is worse than one that says what it does.
func TestClearingRemovesTheKeptRecordsOnly(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "rig-exporter.log", "rig-exporter.log.1", "crash-2026-08-07-160000.log")

	var kept []string
	for _, file := range logFilesIn(dir) {
		if !file.Current {
			kept = append(kept, file.Name)
		}
	}

	if len(kept) != 2 {
		t.Fatalf("kept = %v, want the rotated log and the crash report", kept)
	}
	for _, name := range kept {
		if name == "rig-exporter.log" {
			t.Error("the running log is listed as removable")
		}
	}
}
