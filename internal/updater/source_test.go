package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

type fakeProviderAsset struct {
	id   int64
	name string
	data []byte
}

func (a fakeProviderAsset) GetID() int64                  { return a.id }
func (a fakeProviderAsset) GetName() string               { return a.name }
func (a fakeProviderAsset) GetSize() int                  { return len(a.data) }
func (a fakeProviderAsset) GetBrowserDownloadURL() string { return "https://example.invalid/" + a.name }

type fakeProviderRelease struct {
	assets []selfupdate.SourceAsset
}

func (r fakeProviderRelease) GetID() int64                        { return 42 }
func (r fakeProviderRelease) GetTagName() string                  { return "v1.6.4" }
func (r fakeProviderRelease) GetDraft() bool                      { return false }
func (r fakeProviderRelease) GetPrerelease() bool                 { return false }
func (r fakeProviderRelease) GetPublishedAt() time.Time           { return time.Unix(1_700_000_000, 0) }
func (r fakeProviderRelease) GetReleaseNotes() string             { return "A useful changelog" }
func (r fakeProviderRelease) GetName() string                     { return "rig-exporter 1.6.4" }
func (r fakeProviderRelease) GetURL() string                      { return "https://example.invalid/releases/v1.6.4" }
func (r fakeProviderRelease) GetAssets() []selfupdate.SourceAsset { return r.assets }

type fakeProvider struct {
	release fakeProviderRelease
	data    map[int64][]byte
}

func (p *fakeProvider) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return []selfupdate.SourceRelease{p.release}, nil
}

func (p *fakeProvider) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(p.data[id])), nil
}

func TestReleaseDownloadsHaveAHardReadLimit(t *testing.T) {
	provider := &fakeProvider{data: map[int64][]byte{1: []byte("123456789")}}
	source := limitedSource{Source: provider, maxBytes: 4}
	reader, err := source.DownloadReleaseAsset(context.Background(), nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "12345" {
		t.Errorf("limited download = %q, want one byte beyond the four-byte limit", got)
	}
}

func TestReleaseAssetFilterRequiresAVersionBoundWindowsAsset(t *testing.T) {
	filter := regexp.MustCompile(releaseAssetFilter)
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "rig-exporter_1.6.4_windows_amd64.exe.zip", want: true},
		{name: "rig-exporter_0.0.0_windows_amd64.exe.zip", want: true},
		{name: "rig-exporter_windows_amd64.exe.zip"},
		{name: "rig-exporter_01.6.4_windows_amd64.exe.zip"},
		{name: "rig-exporter_1.6.4-rc.1_windows_amd64.exe.zip"},
		{name: "rig-exporter_1.6.4_windows_arm64.exe.zip"},
	} {
		if got := filter.MatchString(test.name); got != test.want {
			t.Errorf("release asset filter match for %q = %v, want %v", test.name, got, test.want)
		}
	}
}

func signedProvider(t *testing.T) (*fakeProvider, []byte, []byte) {
	t.Helper()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, err := zw.Create("rig-exporter.exe")
	if err != nil {
		t.Fatalf("create archive entry: %v", err)
	}
	newExecutable := []byte("signed new executable")
	if _, err := w.Write(newExecutable); err != nil {
		t.Fatalf("write archive entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	archiveHash := sha256.Sum256(archive.Bytes())
	assetName := releaseAssetName("1.6.4")
	checksums := []byte(strings.ToUpper(hex.EncodeToString(archiveHash[:])) + "  " + assetName + "\n")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rig-exporter update test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	checksumHash := sha256.Sum256(checksums)
	signature, err := ecdsa.SignASN1(rand.Reader, key, checksumHash[:])
	if err != nil {
		t.Fatalf("sign checksums: %v", err)
	}

	assets := []fakeProviderAsset{
		{id: 1, name: assetName, data: archive.Bytes()},
		{id: 2, name: "checksums.txt", data: checksums},
		{id: 3, name: "checksums.txt.sig", data: signature},
	}
	provider := &fakeProvider{data: map[int64][]byte{}}
	for _, asset := range assets {
		provider.release.assets = append(provider.release.assets, asset)
		provider.data[asset.id] = asset.data
	}
	return provider, certificate, newExecutable
}

func TestReleaseSourceStagesOnlyAnAuthenticRelease(t *testing.T) {
	provider, certificate, wantExecutable := signedProvider(t)
	current := filepath.Join(t.TempDir(), "current.exe")
	if err := os.WriteFile(current, []byte("old executable"), 0o755); err != nil {
		t.Fatalf("write current executable: %v", err)
	}
	source, err := newGitHubReleaseSource(provider, certificate, current)
	if err != nil {
		t.Fatalf("newGitHubReleaseSource: %v", err)
	}

	release, found, err := source.Latest(context.Background())
	if err != nil || !found {
		t.Fatalf("Latest = found:%v err:%v", found, err)
	}
	if release.Version != "1.6.4" || release.Notes != "A useful changelog" {
		t.Fatalf("release = %#v", release)
	}

	target := filepath.Join(t.TempDir(), "rig-exporter.exe")
	if err := source.Stage(context.Background(), release, target); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read staged executable: %v", err)
	}
	if !bytes.Equal(got, wantExecutable) {
		t.Errorf("staged executable = %q, want %q", got, wantExecutable)
	}
}

func TestReleaseSourceRejectsAnAssetForAnotherVersion(t *testing.T) {
	provider, certificate, _ := signedProvider(t)
	provider.release.assets[0] = fakeProviderAsset{
		id:   1,
		name: releaseAssetName("1.6.5"),
		data: provider.data[1],
	}
	current := filepath.Join(t.TempDir(), "current.exe")
	if err := os.WriteFile(current, []byte("old executable"), 0o755); err != nil {
		t.Fatalf("write current executable: %v", err)
	}
	source, err := newGitHubReleaseSource(provider, certificate, current)
	if err != nil {
		t.Fatalf("newGitHubReleaseSource: %v", err)
	}

	if _, _, err := source.Latest(context.Background()); err == nil {
		t.Fatal("Latest accepted an asset whose version differs from its release")
	}
}

func TestReleaseSourceRejectsATamperedArchive(t *testing.T) {
	provider, certificate, _ := signedProvider(t)
	current := filepath.Join(t.TempDir(), "current.exe")
	if err := os.WriteFile(current, []byte("old executable"), 0o755); err != nil {
		t.Fatalf("write current executable: %v", err)
	}
	source, err := newGitHubReleaseSource(provider, certificate, current)
	if err != nil {
		t.Fatalf("newGitHubReleaseSource: %v", err)
	}
	release, found, err := source.Latest(context.Background())
	if err != nil || !found {
		t.Fatalf("Latest = found:%v err:%v", found, err)
	}
	provider.data[1] = append(provider.data[1], byte('x'))

	err = source.Stage(context.Background(), release, filepath.Join(t.TempDir(), "rig-exporter.exe"))
	if err == nil {
		t.Fatal("Stage accepted an archive whose checksum no longer matches")
	}
}

func TestReleaseSourceRejectsAnInvalidChecksumSignature(t *testing.T) {
	provider, certificate, _ := signedProvider(t)
	provider.data[3] = append([]byte(nil), provider.data[3]...)
	provider.data[3][0] ^= 0xff

	current := filepath.Join(t.TempDir(), "current.exe")
	if err := os.WriteFile(current, []byte("old executable"), 0o755); err != nil {
		t.Fatalf("write current executable: %v", err)
	}
	source, err := newGitHubReleaseSource(provider, certificate, current)
	if err != nil {
		t.Fatalf("newGitHubReleaseSource: %v", err)
	}
	release, found, err := source.Latest(context.Background())
	if err != nil || !found {
		t.Fatalf("Latest = found:%v err:%v", found, err)
	}
	if err := source.Stage(context.Background(), release, filepath.Join(t.TempDir(), "rig-exporter.exe")); err == nil {
		t.Fatal("Stage accepted checksums with an invalid ECDSA signature")
	}
}
