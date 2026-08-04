//go:build windows

package updater

import (
	"errors"
	"os"
	"runtime"
	"strings"
)

// New creates the production updater for the running executable.
func New(opts Options) (*Manager, error) {
	if strings.TrimSpace(opts.CurrentVersion) == "" {
		return nil, errors.New("current update version is empty")
	}
	if opts.RequestRestart == nil {
		return nil, errors.New("update restart callback is unavailable")
	}
	if runtime.GOARCH != "amd64" {
		return nil, errors.New("self-update is available only for windows/amd64 builds")
	}
	if opts.ExecutablePath == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		opts.ExecutablePath = executable
	}
	source, err := newGitHubReleaseSource(nil, signingCertificatePEM, opts.ExecutablePath)
	if err != nil {
		return nil, err
	}
	return newManager(source, SystemApplyPreparer{}, opts), nil
}
