// Package assets holds the binary resources compiled into rig-exporter.
package assets

import _ "embed"

// Both are packed from docs/images/rig-exporter-entity-512.png.
// Regenerate with: .\build.ps1 -Icon

// Icon is the tray icon, a multi-resolution ICO.
//
//go:embed icon.ico
var Icon []byte

// IconPNG is the same mark as a PNG, for anything that renders in a browser:
// the header of the local interface, and the picture on the Home Assistant
// update card. An ICO works in most browsers, a PNG in all of them, and that
// is the whole argument.
//
//go:embed icon.png
var IconPNG []byte
