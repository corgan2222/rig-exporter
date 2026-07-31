// Package assets holds the binary resources compiled into rig-exporter.
package assets

import _ "embed"

// Icon is the tray icon, a multi-resolution ICO.
// Regenerate with: go run ./tools/genicon
//
//go:embed icon.ico
var Icon []byte
