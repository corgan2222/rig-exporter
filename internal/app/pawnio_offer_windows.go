//go:build windows

package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// PawnIOSetupURL is the installer, from the project's own release page.
const PawnIOSetupURL = "https://github.com/namazso/PawnIO.Setup/releases/latest/download/PawnIO_setup.exe"

// pawnioSetupHosts are the hosts a download is allowed to end on.
//
// The URL above is a redirect: GitHub sends "latest" on to a versioned asset on
// a different host. Following that blindly means whatever the redirect chain
// ends up pointing at gets written to disk and offered to the user as an
// installer, so the destination is checked rather than assumed.
var pawnioSetupHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// maxSetupBytes caps the download. The installer is a few megabytes; this only
// stops a redirect gone wrong from filling the disk.
const maxSetupBytes = 64 << 20

// DownloadPawnIOSetup fetches the installer into dir and returns its path.
//
// It deliberately stops there. Running a freshly downloaded executable is not
// something this program should do on the user's behalf: handing the file to
// the shell instead means Windows applies its own checks — signature,
// SmartScreen, the elevation prompt — and the user sees who signed it before
// anything happens. The two steps are separate functions so that intent is
// impossible to lose by accident.
func DownloadPawnIOSetup(ctx context.Context, dir string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, PawnIOSetupURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the download returned %s", response.Status)
	}
	if err := checkDownloadHost(response.Request.URL); err != nil {
		return "", err
	}

	// Written under a temporary name and renamed once complete, so an
	// interrupted download can never be mistaken for an installer.
	partial, err := os.CreateTemp(dir, "PawnIO_setup-*.partial")
	if err != nil {
		return "", err
	}
	tempName := partial.Name()
	defer os.Remove(tempName)

	written, err := io.Copy(partial, io.LimitReader(response.Body, maxSetupBytes))
	closeErr := partial.Close()
	switch {
	case err != nil:
		return "", err
	case closeErr != nil:
		return "", closeErr
	case written == 0:
		return "", fmt.Errorf("the download was empty")
	case written == maxSetupBytes:
		return "", fmt.Errorf("the download exceeded %d bytes", int64(maxSetupBytes))
	}

	final := filepath.Join(dir, "PawnIO_setup.exe")
	if err := os.Rename(tempName, final); err != nil {
		return "", err
	}
	return final, nil
}

// checkDownloadHost makes sure the redirect chain ended somewhere it should.
//
// There is no published checksum for this asset, so this is as much as can
// honestly be verified here. The real assurance comes afterwards, from the
// signature Windows checks when the installer is launched — which is the other
// reason this program does not launch it itself.
func checkDownloadHost(final *url.URL) error {
	if final == nil {
		return fmt.Errorf("the download had no final address")
	}
	if final.Scheme != "https" {
		return fmt.Errorf("the download ended on %s, which is not https", final.Scheme)
	}
	if !pawnioSetupHosts[final.Hostname()] {
		return fmt.Errorf("the download ended on %s, which is not a PawnIO release host",
			final.Hostname())
	}
	return nil
}
