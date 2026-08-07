//go:build windows

package webui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles fills a directory the way the program would.
func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The card lists what this program wrote and nothing else. The configuration
// sits in the same directory and holds the broker password.
func TestOnlyTheProgramsOwnRecordsAreListed(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir,
		"rig-exporter.log", "rig-exporter.log.1",
		"crash-2026-08-07-154000.log", "crash-2026-08-06-101500.log",
		"config.json", "config.json.bak", "probe.txt", "update-signing-key.pem")

	var listed []string
	for _, file := range logFilesIn(dir) {
		listed = append(listed, file.Name)
	}

	want := []string{
		"rig-exporter.log", "rig-exporter.log.1",
		"crash-2026-08-07-154000.log", "crash-2026-08-06-101500.log",
	}
	if len(listed) != len(want) {
		t.Fatalf("listed %v, want %v", listed, want)
	}
	for i := range want {
		if listed[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, listed[i], want[i])
		}
	}
}

// The running log first, then its backup, then the crash reports newest first:
// the one that matters is the one that just happened.
func TestTheNewestCrashIsListedFirst(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "crash-2026-08-01-100000.log", "crash-2026-08-07-154000.log", "rig-exporter.log")

	files := logFilesIn(dir)
	if files[0].Name != "rig-exporter.log" || !files[0].Current {
		t.Errorf("the running log is not first: %+v", files[0])
	}
	if files[1].Name != "crash-2026-08-07-154000.log" {
		t.Errorf("the newest crash is not first among the reports: %q", files[1].Name)
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
