package sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// Default polling cadence + timeout. Tuned for snapshot/restore which
// typically completes in seconds for cached bundles, up to ~30 s on a
// cold R2 fetch.
const (
	pollInterval = 2 * time.Second
	pollTimeout  = 5 * time.Minute
)

// waitForStatus polls GET /v1/sandboxes/:id until the sandbox lands in
// one of the target statuses or the timeout fires. Returns the final
// view. The set of "failed" statuses always counts as terminal so we
// don't spin forever after a backend error.
func waitForStatus(ctx context.Context, client *api.SandboxClient, id string, targets ...string) (*api.SandboxView, error) {
	deadline := time.Now().Add(pollTimeout)
	wanted := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		wanted[t] = struct{}{}
	}
	// Always treat "failed" as terminal — the operation is done, even
	// if not the way the caller hoped.
	wanted[api.SandboxStatusFailed] = struct{}{}

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		sb, err := client.GetSandbox(ctx, id)
		if err != nil {
			return nil, err
		}
		if _, hit := wanted[sb.Status]; hit {
			return sb, nil
		}
		if time.Now().After(deadline) {
			return sb, fmt.Errorf("sandbox stuck in %q after %s — check `createos sandbox get %s`", sb.Status, pollTimeout, id)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
