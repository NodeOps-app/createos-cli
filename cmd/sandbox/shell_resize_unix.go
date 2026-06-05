//go:build !windows

package sandbox

import (
	"os"
	"os/signal"
	"syscall"
)

// watchWindowSize invokes onResize whenever the controlling terminal is
// resized (SIGWINCH). It returns a stop function that unregisters the
// handler and ends the goroutine; call it via defer.
func watchWindowSize(onResize func()) func() {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-winch:
				onResize()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(winch)
		close(done)
	}
}
