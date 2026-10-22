//go:build !windows

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ampLock serializes jobs for one orb. Amp can send pause while a spawn is
// still running; unserialized, the spawn's setup wakes the sandbox the pause
// just stopped, and it keeps billing while Amp thinks it is paused.
//
// flock is released by the kernel when the process exits, even on SIGKILL,
// so a job Amp kills at its five-minute limit cannot leave the orb locked.
//
// shortcut: the lock is per machine; running the provider on several hosts
// needs a shared lock (none exists in the API today).
func ampLock(ctx context.Context, name string) (unlock func(), err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not find your home folder: %w", err)
	}
	dir := filepath.Join(home, ".createos", "amp-locks")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 G703 -- name is amp-<hex>, from ampSandboxName
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd()) // #nosec G115 -- a file descriptor fits in int
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil //nolint:errcheck // closing releases the lock
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close() //nolint:errcheck
			return nil, fmt.Errorf("lock %s: %w", name, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close() //nolint:errcheck
			return nil, fmt.Errorf("another job for %s is still running: %w", name, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}
