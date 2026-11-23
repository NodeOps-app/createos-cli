package sandbox

import (
	"fmt"
	"strconv"

	"github.com/urfave/cli/v2"
)

func timeoutMinutesFlag() cli.Flag {
	return &cli.StringFlag{Name: "timeout-minutes", Usage: "Destroy after 2–1440 whole minutes, including paused time (omitted: no requested expiry)"}
}

func sigtermExpiryFlag() cli.Flag {
	return &cli.BoolFlag{Name: "sigterm-before-expiry", Usage: "Request best-effort SIGTERM to managed workloads in the final minute; requires --timeout-minutes"}
}

func parseSandboxTimeout(raw string, warn bool) (*int32, error) {
	if raw == "" {
		if warn {
			return nil, fmt.Errorf("--sigterm-before-expiry requires --timeout-minutes")
		}
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 2 || value > 1440 {
		return nil, fmt.Errorf("--timeout-minutes must be a whole number from 2 to 1440")
	}
	minutes := int32(value)
	return &minutes, nil
}
