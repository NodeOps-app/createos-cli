//go:build windows

package sandbox

// watchWindowSize is a no-op on Windows: there is no SIGWINCH. The initial
// terminal size is still sent once before the session starts; tracking
// live console resizes would require polling GetConsoleScreenBufferInfo,
// which isn't worth it for the keyless PTY path. Returns a no-op stop.
func watchWindowSize(onResize func()) func() {
	_ = onResize
	return func() {}
}
