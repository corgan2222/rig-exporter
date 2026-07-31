//go:build windows

// Package autostart toggles the "start with Windows" registry entry.
//
// It writes to HKCU rather than HKLM so no elevation is needed, which matches
// how rig-exporter is meant to run: as the logged-in user, next to the game.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Enabled reports whether the current executable is registered to start with
// Windows. A stale entry pointing at a different path counts as disabled, so
// moving the exe shows up in the UI as "off" instead of silently not working.
func Enabled(valueName string) (bool, error) {
	exe, err := executablePath()
	if err != nil {
		return false, err
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open Run key: %w", err)
	}
	defer key.Close()

	value, _, err := key.GetStringValue(valueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", valueName, err)
	}
	return strings.EqualFold(strings.Trim(value, `"`), exe), nil
}

// Set adds or removes the autostart entry.
func Set(valueName string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer key.Close()

	if !enabled {
		if err := key.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", valueName, err)
		}
		return nil
	}

	exe, err := executablePath()
	if err != nil {
		return err
	}
	// Quoted so a path containing spaces is not split into arguments.
	if err := key.SetStringValue(valueName, `"`+exe+`"`); err != nil {
		return fmt.Errorf("write %s: %w", valueName, err)
	}
	return nil
}

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil // a non-resolvable path is still worth registering
	}
	return resolved, nil
}
