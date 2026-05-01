// Package telemetry sends anonymous usage data to PostHog.
//
// All events are best-effort and never block the CLI. The package is a no-op
// when disabled (empty PostHog API key or CREATEOS_DO_NOT_TRACK=1).
//
// IMPORTANT: do not import this package outside its own files except via the
// thin wiring in main.go and cmd/root. Do not log to stdout/stderr from any
// path here — telemetry must stay silent.
package telemetry

import "os"

// PostHogAPIKey is injected at build time via -ldflags. Empty by default,
// which disables telemetry entirely.
var PostHogAPIKey = ""

// PostHogHost is the PostHog ingestion endpoint. Overridable via -ldflags.
var PostHogHost = "https://us.i.posthog.com"

const (
	envOptOut = "CREATEOS_DO_NOT_TRACK"
	envKey    = "CREATEOS_POSTHOG_KEY"
	envHost   = "CREATEOS_POSTHOG_HOST"
)

// IsDisabled reports whether telemetry should be a no-op for this process.
func IsDisabled() bool {
	if os.Getenv(envOptOut) == "1" {
		return true
	}
	return effectiveKey() == ""
}

// effectiveKey returns the PostHog API key — env var wins over ldflag.
func effectiveKey() string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return PostHogAPIKey
}

// effectiveHost returns the PostHog endpoint — env var wins over ldflag.
func effectiveHost() string {
	if v := os.Getenv(envHost); v != "" {
		return v
	}
	return PostHogHost
}
