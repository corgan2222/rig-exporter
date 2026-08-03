//go:build !windows

package config

// osLanguage has nothing to consult off Windows, where this package only exists
// so the settings stay testable. An empty result leaves the default in place.
func osLanguage() string { return "" }
