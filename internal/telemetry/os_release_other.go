//go:build !linux && !darwin && !windows

package telemetry

// osRelease returns "" on platforms we don't have a kernel-version probe for.
func osRelease() string { return "" }
