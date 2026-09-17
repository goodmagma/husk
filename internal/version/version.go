// Package version holds the program version shared by husk and husk-gui.
package version

// Version can be overridden at build time:
//
//	go build -ldflags "-X github.com/goodmagma/husk/internal/version.Version=0.2.0" ./cmd/husk
//
// Keep it in sync with cmd/husk-gui/FyneApp.toml.
var Version = "0.1.1"
