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
}

type limitedSource struct {
	selfupdate.Source
	maxBytes int64
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func (s limitedSource) DownloadReleaseAsset(ctx context.Context, release *selfupdate.Release,
	assetID int64) (io.ReadCloser, error) {
	reader, err := s.Source.DownloadReleaseAsset(ctx, release, assetID)
	if err != nil {
		return nil, err
	}
	// One byte beyond the bound makes an oversized payload fail its signed
	// checksum while still placing a hard ceiling on go-selfupdate's ReadAll.
	return limitedReadCloser{
		Reader: io.LimitReader(reader, s.maxBytes+1),
		Closer: reader,
	}, nil
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
	source = limitedSource{Source: source, maxBytes: maxReleaseSize}
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

func (s *githubReleaseSource) Stage(ctx context.Context, release Release, target string) error {
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
