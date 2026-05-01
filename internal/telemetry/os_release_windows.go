//go:build windows

package telemetry

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// osRelease returns a "MAJOR.MINOR.BUILD" string from RtlGetVersion.
// Empty string on any error.
func osRelease() string {
	v := windows.RtlGetVersion()
	if v == nil {
		return ""
	}
	return strconv.FormatUint(uint64(v.MajorVersion), 10) + "." +
		strconv.FormatUint(uint64(v.MinorVersion), 10) + "." +
		strconv.FormatUint(uint64(v.BuildNumber), 10)
}
