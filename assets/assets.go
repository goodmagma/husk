// Package assets embeds the program artwork.
package assets

import _ "embed"

// IconSVG is the program icon (window icon, README logo).
//
//go:embed icon.svg
var IconSVG []byte
