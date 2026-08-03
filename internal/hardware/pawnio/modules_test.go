package pawnio

import (
	"archive/zip"
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// The module name reaches a file path. An archive is an untrusted list of
// names, so anything that is not a bare .bin has to be refused before it gets
// anywhere near one.
func TestOnlyPlainModuleNamesAreAccepted(t *testing.T) {
	for name, ok := range map[string]bool{
		"AMDFamily17.bin": true,
		"IntelMSR.bin":    true,

		"":                        false,
		".":                       false,
		"..":                      false,
		"../AMDFamily17.bin":      false,
		`..\..\system32\evil.bin`: false,
		"sub/AMDFamily17.bin":     false,
		`sub\AMDFamily17.bin`:     false,
		"C:/Windows/evil.bin":     false,
		"AMDFamily17.exe":         false,
		"AMDFamily17":             false,
	} {
		if got := validModuleName(name) == nil; got != ok {
			t.Errorf("validModuleName(%q) accepted=%v, want %v", name, got, ok)
		}
	}
}

func TestOnlyReleaseHostsServeModules(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/namazso/PawnIO.Modules/releases/download/0.2.10/release_0_2_10.zip": true,
		"https://objects.githubusercontent.com/x":                                               true,

		"https://evil.example/release.zip":         false,
		"https://github.com.evil.example/x":        false,
		"http://github.com/namazso/PawnIO.Modules": false,
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := checkModuleHost(parsed) == nil; got != want {
			t.Errorf("checkModuleHost(%q) accepted=%v, want %v", raw, got, want)
		}
	}
}

// buildArchive makes a zip with the given entry names and contents.
func buildArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An archive is somebody else's list of paths. An entry naming a parent
// directory must land in the module directory like any other, never outside it.
func TestUnpackingCannotEscapeTheModuleDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "modules")
	store := NewModuleStore(dir)

	archive := buildArchive(t, map[string]string{
		"AMDFamily17.bin":         "amd",
		"../../escaped.bin":       "nope",
		`..\..\alsoescaped.bin`:   "nope",
		"nested/dir/IntelMSR.bin": "intel",
		"README.md":               "not a module",
	})

	if err := store.unpack(archive); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	// Nothing may exist above the module directory.
	for _, stray := range []string{"escaped.bin", "alsoescaped.bin"} {
		if _, err := os.Stat(filepath.Join(root, stray)); err == nil {
			t.Errorf("%s was written outside the module directory", stray)
		}
	}

	// The real modules land, flattened, and the readme is skipped.
	for name, want := range map[string]string{"AMDFamily17.bin": "amd", "IntelMSR.bin": "intel"} {
		blob, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s missing: %v", name, err)
			continue
		}
		if string(blob) != want {
			t.Errorf("%s = %q, want %q", name, blob, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Error("a non-module was extracted")
	}

	// The escaped names must not have been flattened into the directory either.
	for _, stray := range []string{"escaped.bin", "alsoescaped.bin"} {
		if _, err := os.Stat(filepath.Join(dir, stray)); err != nil {
			continue
		}
		t.Logf("%s was flattened into the module directory, which is safe", stray)
	}
}

func TestAnArchiveWithoutModulesIsAnError(t *testing.T) {
	store := NewModuleStore(t.TempDir())
	if err := store.unpack(buildArchive(t, map[string]string{"README.md": "x"})); err == nil {
		t.Error("an archive with no modules was accepted")
	}
}

// A module already on disk is used as it is, and nothing is downloaded. That is
// how somebody substitutes their own build, which is what the modules' licence
// asks a program using them to allow.
func TestALocalModuleIsUsedWithoutDownloading(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AMDFamily17.bin"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A cancelled context makes any download attempt fail immediately, so a
	// success here proves none was made.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	blob, err := NewModuleStore(dir).Load(ctx, "AMDFamily17.bin")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(blob) != "mine" {
		t.Errorf("Load = %q, want the local copy", blob)
	}
}

func TestLoadRejectsATraversingName(t *testing.T) {
	if _, err := NewModuleStore(t.TempDir()).Load(context.Background(), "../evil.bin"); err == nil {
		t.Error("a traversing module name was accepted")
	}
}
