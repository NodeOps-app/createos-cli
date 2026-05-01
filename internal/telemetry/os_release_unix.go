//go:build linux || darwin

package telemetry

import "golang.org/x/sys/unix"

// osRelease returns a kernel release string like "23.6.0" (darwin) or
// "5.15.0-87-generic" (linux). Empty string on any error.
func osRelease() string {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return ""
	}
	return cstr(u.Release[:])
}

// cstr converts a C-string byte slice (NUL-terminated, fixed-width) to Go.
func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
