// Package version holds the current CLI version string.
package version

// Version is the current release version of the CreateOS CLI.
// Injected at build time via -ldflags="-X .../version.Version=vX.Y.Z"
var Version = "dev"

// Channel is the release channel: "stable" or "nightly".
// Injected at build time via -ldflags="-X .../version.Channel=stable"
var Channel = "stable"
