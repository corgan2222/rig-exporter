//go:build windows

package crashlog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Ten reports are kept, and it has to be the ten newest.
//
// Sorting the names as text did that for as long as they all began the same
// way, and stopped the moment a second naming existed: every crash- name sorts
// before every rig-exporter_ one, so the older scheme would be thrown away first
// however recent it was. The two namings are interleaved here on purpose — text
// order and age order disagree, and only one of them is right.
func TestTheOldestReportsGoWhicheverWayTheyAreNamed(t *testing.T) {
	dir := t.TempDir()

	var written []string
	for day := 1; day <= keptReports+2; day++ {
		at := time.Date(2026, 8, day, 12, 0, 0, 0, time.Local)
		name := ReportName("corgan-pc3", at)
		if day%2 == 0 {
			name = fmt.Sprintf("crash-%s.log", at.Format(legacyStamp))
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		written = append(written, name)
	}
	// The directory also holds what this program must not touch.
	for _, other := range []string{"config.json", "rig-exporter.log", "crash.log"} {
		if err := os.WriteFile(filepath.Join(dir, other), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prune(dir)

	for i, name := range written {
		_, err := os.Stat(filepath.Join(dir, name))
		gone := os.IsNotExist(err)
		// The first two days are the two oldest, whatever they are called.
		if want := i < 2; gone != want {
			t.Errorf("%q gone = %v, want %v", name, gone, want)
		}
	}
	for _, other := range []string{"config.json", "rig-exporter.log", "crash.log"} {
		if _, err := os.Stat(filepath.Join(dir, other)); err != nil {
			t.Errorf("%s was removed: %v", other, err)
		}
	}
}
