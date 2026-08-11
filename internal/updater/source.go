package updater

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

const (
	releaseOwner         = "corgan2222"
	releaseRepository    = "rig-exporter"
	releaseAssetFilter   = `^rig-exporter_(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)_windows_amd64\.exe\.zip$`
	releaseChecksumsFile = "checksums.txt"
)

// signingCertificatePEM is the trust anchor for official release artifacts.
// The matching private key exists only as the UPDATE_SIGNING_KEY repository
// secret and is never distributed with the application.
//
//go:embed update-signing-cert.pem
var signingCertificatePEM []byte

type githubReleaseSource struct {
	updater       *selfupdate.Updater
	repository    selfupdate.Repository
	currentBinary string
	// progress is the same reporter the download wrapper reads, which is the
	// only way back to a reader go-selfupdate owns.
	progress *progressReporter
}

type limitedSource struct {
	selfupdate.Source
	maxBytes int64
	// progress is shared by every copy of this value, because the value itself
	// is handed to go-selfupdate once at construction while the callback
	// changes per staging run.
	progress *progressReporter
}

// progressReporter carries the callback for the staging run currently under
// way. go-selfupdate reads the asset on its own goroutine, so both ends are
// behind the lock.
type progressReporter struct {
	mu sync.Mutex
	fn func(float64)
}

func (p *progressReporter) set(fn func(float64)) {
	p.mu.Lock()
	p.fn = fn
	p.mu.Unlock()
}

func (p *progressReporter) report(percent float64) {
	p.mu.Lock()
	fn := p.fn
	p.mu.Unlock()

	if fn != nil {
		fn(percent)
	}
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

// countingReader turns bytes read into a percentage of the whole.
//
// The library offers no progress hook of its own, so the count is taken here —
// this package already wraps the download to bound its size, and one wrapper
// doing both is cheaper than a second one around the same reader.
type countingReader struct {
	inner    io.Reader
	total    int64
	read     int64
	reported float64
	report   func(float64)
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		r.read += int64(n)
		// A whole percentage point at a time. The asset is megabytes and the
		// reads are kilobytes, so reporting every read would push a hundred
		// messages per percent onto the broker for a bar with a hundred steps.
		if percent := min(float64(r.read)/float64(r.total)*100, 100); percent >= r.reported+1 {
			r.reported = percent
			r.report(percent)
		}
	}
	return n, err
}

func (s limitedSource) DownloadReleaseAsset(ctx context.Context, release *selfupdate.Release,
	assetID int64) (io.ReadCloser, error) {
	reader, err := s.Source.DownloadReleaseAsset(ctx, release, assetID)
	if err != nil {
		return nil, err
	}

	// One byte beyond the bound makes an oversized payload fail its signed
	// checksum while still placing a hard ceiling on go-selfupdate's ReadAll.
	var bounded io.Reader = io.LimitReader(reader, s.maxBytes+1)

	// Counted only for the release asset itself. The checksum file and its
	// signature come through this same call and are a few hundred bytes against
	// an asset of megabytes; measuring them against the asset size would run the
	// bar up to some meaningless number and back down again.
	if s.progress != nil && release != nil && assetID == release.AssetID && release.AssetByteSize > 0 {
		bounded = &countingReader{
			inner:  bounded,
			total:  int64(release.AssetByteSize),
			report: s.progress.report,
		}
	}

	return limitedReadCloser{Reader: bounded, Closer: reader}, nil
}

// newGitHubReleaseSource builds the provider adapter. source is injected in
// tests; production passes the anonymous GitHub source for the fixed public
// repository.
func newGitHubReleaseSource(source selfupdate.Source, certificate []byte, currentBinary string) (*githubReleaseSource, error) {
	if err := validateSigningCertificate(certificate); err != nil {
		return nil, err
	}
	if source == nil {
		var err error
		source, err = selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
		if err != nil {
			return nil, fmt.Errorf("create GitHub release source: %w", err)
		}
	}
	// Built before the value is handed over, because go-selfupdate keeps its own
	// copy from here on and only the shared pointer can still be reached.
	progress := &progressReporter{}
	source = limitedSource{Source: source, maxBytes: maxReleaseSize, progress: progress}
	up, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:     source,
		Validator:  selfupdate.NewChecksumWithECDSAValidator(releaseChecksumsFile, certificate),
		Filters:    []string{releaseAssetFilter},
		OS:         "windows",
		Arch:       "amd64",
		Draft:      false,
		Prerelease: false,
	})
	if err != nil {
		return nil, fmt.Errorf("configure release updater: %w", err)
	}
	return &githubReleaseSource{
		updater:       up,
		repository:    selfupdate.NewRepositorySlug(releaseOwner, releaseRepository),
		currentBinary: currentBinary,
		progress:      progress,
	}, nil
}

func validateSigningCertificate(raw []byte) error {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return invalidSigningCertificate("not one PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return invalidSigningCertificate(err.Error())
	}
	if _, ok := certificate.PublicKey.(*ecdsa.PublicKey); !ok {
		return invalidSigningCertificate("public key is not ECDSA")
	}
	if certificate.IsCA || certificate.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return invalidSigningCertificate("certificate is not limited to digital signatures")
	}
	return nil
}

func invalidSigningCertificate(detail string) error {
	return fmt.Errorf("invalid update signing certificate: %s", detail)
}

func (s *githubReleaseSource) Latest(ctx context.Context) (Release, bool, error) {
	release, found, err := s.updater.DetectLatest(ctx, s.repository)
	if err != nil {
		return Release{}, false, err
	}
	if !found {
		return Release{}, false, nil
	}
	version := release.Version()
	if release.AssetName != releaseAssetName(version) {
		return Release{}, false, invalidRelease("unexpected asset " + release.AssetName)
	}
	return Release{
		Version: version,
		Notes:   release.ReleaseNotes,
		URL:     release.URL,
		Size:    release.AssetByteSize,
		native:  release,
	}, true, nil
}

func (s *githubReleaseSource) Stage(ctx context.Context, release Release, target string, report func(float64)) error {
	native, ok := release.native.(*selfupdate.Release)
	if !ok || native == nil {
		return invalidRelease("provider metadata is missing")
	}
	if native.Version() != release.Version {
		return invalidRelease("provider version changed")
	}
	if native.AssetName != releaseAssetName(release.Version) {
		return invalidRelease("unexpected asset " + native.AssetName)
	}
	if native.AssetByteSize <= 0 || native.AssetByteSize > maxReleaseSize {
		return invalidRelease(fmt.Sprintf("asset size %d is outside the allowed range", native.AssetByteSize))
	}

	if err := copyExecutable(s.currentBinary, target); err != nil {
		return fmt.Errorf("prepare staging target: %w", err)
	}

	// Cleared again afterwards: the reporter outlives this call, and a download
	// started by anything else must not reach a caller that has already
	// returned.
	s.progress.set(report)
	defer s.progress.set(nil)

	if err := s.updater.UpdateTo(ctx, native, target); err != nil {
		return err
	}
	return nil
}

func releaseAssetName(version string) string {
	return "rig-exporter_" + version + "_windows_amd64.exe.zip"
}

func invalidRelease(detail string) error {
	return fmt.Errorf("invalid update release: %s", detail)
}

func copyExecutable(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = out.Close()
		if remove {
			_ = os.Remove(target)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
