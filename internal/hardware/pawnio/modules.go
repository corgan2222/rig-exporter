package pawnio

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// PawnIO's driver is a sandbox; the code that actually touches hardware lives
// in separate modules, and its installer ships none of them. They are published
// as one small archive of signed binaries.
//
// They are fetched rather than shipped. Two reasons, and the second is the one
// that matters. They are LGPL-2.1, so distributing them inside this program
// would drag obligations onto a project that only wants to read a temperature.
// And the driver verifies every module's signature before it will load it, so a
// module that arrived over a tampered connection does not run — it is refused.
// That makes fetching them safer than the usual download, not riskier.
const (
	// ModulesURL is the published archive of signed modules.
	ModulesURL = "https://github.com/namazso/PawnIO.Modules/releases/latest/download/release_0_2_10.zip"

	// maxArchiveBytes caps the download. The archive is about 62 KB.
	maxArchiveBytes = 8 << 20
	// maxModuleBytes caps one extracted module, so a crafted archive cannot
	// expand into something enormous.
	maxModuleBytes = 1 << 20
)

// moduleHosts are the hosts the archive download may end on. The published URL
// redirects, and whatever the chain ends at is what gets written to disk.
var moduleHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// ModuleStore keeps the fetched modules on disk so the archive is downloaded
// once rather than at every start.
type ModuleStore struct {
	dir string
}

// NewModuleStore keeps modules under dir, which is created on demand.
func NewModuleStore(dir string) *ModuleStore { return &ModuleStore{dir: dir} }

// Load returns one module's bytes, e.g. "AMDFamily17.bin".
//
// A copy already on disk wins, and that is deliberate rather than merely a
// cache: it is how someone supplies their own module, which is what the
// modules' licence asks of a program that uses them. Nothing is downloaded
// while a local copy exists.
func (s *ModuleStore) Load(ctx context.Context, name string) ([]byte, error) {
	if err := validModuleName(name); err != nil {
		return nil, err
	}

	local := filepath.Join(s.dir, name)
	if blob, err := os.ReadFile(local); err == nil && len(blob) > 0 {
		return blob, nil
	}

	if err := s.fetchArchive(ctx); err != nil {
		return nil, fmt.Errorf("fetch the PawnIO modules: %w", err)
	}

	blob, err := os.ReadFile(local)
	if err != nil {
		return nil, fmt.Errorf("%s is not in the module archive: %w", name, err)
	}
	return blob, nil
}

// validModuleName rejects anything that is not a bare file name.
//
// The name reaches a file path, and a name carrying a separator or a parent
// reference would let a caller — or an archive entry — write outside the
// directory. Checked here so every path below is known to be safe.
func validModuleName(name string) error {
	if name == "" || name != path.Base(name) || name != filepath.Base(name) {
		return fmt.Errorf("%q is not a plain module name", name)
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("%q is not a plain module name", name)
	}
	if !strings.HasSuffix(name, ".bin") {
		return fmt.Errorf("%q is not a module", name)
	}
	return nil
}

// fetchArchive downloads the archive and unpacks its modules into the store.
func (s *ModuleStore) fetchArchive(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ModulesURL, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the download returned %s", response.Status)
	}
	if err := checkModuleHost(response.Request.URL); err != nil {
		return err
	}

	archive, err := io.ReadAll(io.LimitReader(response.Body, maxArchiveBytes))
	if err != nil {
		return err
	}
	if len(archive) == 0 {
		return fmt.Errorf("the archive was empty")
	}
	if len(archive) == maxArchiveBytes {
		return fmt.Errorf("the archive exceeded %d bytes", maxArchiveBytes)
	}
	return s.unpack(archive)
}

func checkModuleHost(final *url.URL) error {
	if final == nil {
		return fmt.Errorf("the download had no final address")
	}
	if final.Scheme != "https" {
		return fmt.Errorf("the download ended on %s, which is not https", final.Scheme)
	}
	if !moduleHosts[final.Hostname()] {
		return fmt.Errorf("the download ended on %s, which is not a release host",
			final.Hostname())
	}
	return nil
}

// unpack writes the archive's modules into the store.
//
// Entry names are reduced to their base name rather than trusted: an archive
// is an untrusted list of paths, and one containing "..\..\system32\x.bin"
// would otherwise be an invitation. Anything that is not a plain .bin is
// skipped instead of refused, so an archive that grows a readme keeps working.
func (s *ModuleStore) unpack(archive []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}

	written := 0
	for _, entry := range reader.File {
		if !safeArchivePath(entry.Name) {
			continue
		}
		name := filepath.Base(entry.Name)
		if entry.FileInfo().IsDir() || validModuleName(name) != nil {
			continue
		}
		if entry.UncompressedSize64 > maxModuleBytes {
			continue
		}
		if err := s.extract(entry, filepath.Join(s.dir, name)); err != nil {
			return err
		}
		written++
	}
	if written == 0 {
		return fmt.Errorf("the archive held no modules")
	}
	return nil
}

func safeArchivePath(name string) bool {
	normalized := strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean(normalized)

	if strings.HasPrefix(clean, "/") {
		return false
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	return true
}

func (s *ModuleStore) extract(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()

	blob, err := io.ReadAll(io.LimitReader(source, maxModuleBytes+1))
	if err != nil {
		return err
	}
	if len(blob) > maxModuleBytes {
		return fmt.Errorf("%s is larger than %d bytes", entry.Name, maxModuleBytes)
	}
	return os.WriteFile(target, blob, 0o644)
}
