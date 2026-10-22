//go:build windows

package sandbox

import (
	"context"
	"fmt"
)

func ampLock(context.Context, string) (func(), error) {
	return nil, fmt.Errorf("the Amp orb provider runs on macOS and Linux only")
}
