## Telemetry

The CLI ships PostHog telemetry via `internal/telemetry`. **Most new commands need ZERO telemetry code** — `command_invoked` / `command_completed` / `command_failed` are emitted automatically by the Action wrapper in `cmd/root/telemetry.go` and the `main.go` finalizer. Just write your command's `Action` and it will be tracked.

### When you DO need to touch telemetry

Add a `telemetry.Default.Capture(...)` call ONLY when a command represents a discrete domain event distinct from "command invoked/completed". Examples already in the codebase:

- `auth_event{action: login|logout|refresh, method, success}` — login/logout/refresh in `cmd/auth/`, `cmd/root/root.go` Before hook.
- `upgrade_event{from_version, to_version, success, failure_reason}` — `cmd/upgrade/`.

If you're adding a similar high-value lifecycle event (e.g. `deployment_event`, `vm_event`), follow the same pattern:
```go
if telemetry.Default != nil {
    telemetry.Default.Capture("<event_name>", map[string]any{
        "action":  "<verb>",
        "success": true,
        // domain-specific props (NO secrets, NO file paths, NO emails)
    })
}
```

### Hard rules (do NOT break)

- ❌ **Never** import `github.com/posthog/posthog-go` outside `internal/telemetry/`.
- ❌ **Never** add a `Flush(timeout)` method to `internal/telemetry/Client`. posthog-go's `Close()` is terminal and there is no non-terminal flush primitive.
- ❌ **Never** include user email, file paths, command output, tokens, or any flag value matching the redact denylist (token/password/secret/key/credential/bearer/auth) in event Properties. The Action wrapper auto-redacts flag values via `internal/telemetry/redact.go`; preserve that behavior — if you add a new sensitive flag alias, ensure `internal/telemetry/redact.go::denyKeywords` covers it (canonical name OR alias).
- ❌ **Never** call `apiClient.GetUser()` outside of `cmd/auth/login.go`'s `bindIdentityAndCapture`. Identity binding happens once at login, not per-command.
- ❌ **Never** persist user_id/email/anything PII to `~/.createos/.identity` beyond `{user_id, aliased_for_user_id}`. The file is intentionally minimal; PostHog Person properties (email, name, signup_date) are sent in-memory via `Client.SetPersonProperties` and never touch disk.
- ❌ **Never** emit telemetry from `App.Before` (subcommand name not yet resolved) or `App.After` (cannot see Action error). Use the Action wrapper or the `main.go` finalizer.
- ❌ **Never** call `telemetry.Default.Capture` from a hot loop or per-iteration code path. Events are coarse-grained — one per CLI invocation, plus a handful of domain lifecycle events. The free monthly quota is 1M events.

### When adding a new sensitive flag

If you add a flag whose value should be redacted from telemetry (any new auth/secret-bearing flag):
- Pick a name where the canonical OR any alias contains a denylist keyword (`token`, `secret`, etc.) — e.g. `--api-token`, `--ssh-key`. The redact path canonicalizes via `c.Lineage()` so any alias matching the denylist redacts the whole flag.
- If the flag name doesn't naturally contain a denylist keyword (e.g. a credential called `--cookie`), add the new keyword to `internal/telemetry/redact.go::denyKeywords`.

### When adding a new project-scoped command

The Action wrapper auto-attaches `project_id` to events when:
- the command has a `--project` or `--project-id` flag, OR
- a `.createos.json` exists in cwd / parent dirs (`config.FindProjectConfig`).

If your command resolves project ID via a different mechanism (e.g. positional arg only, or a custom env var), update `cmd/root/telemetry.go::resolveProjectID` so the project_id property is set correctly.

### Verifying your changes

After wiring telemetry, smoke test against a staging key:
```bash
go build -ldflags="-X github.com/NodeOps-app/createos-cli/internal/telemetry.PostHogAPIKey=<STAGING_KEY> \
  -X github.com/NodeOps-app/createos-cli/internal/telemetry.PostHogHost=https://us.i.posthog.com" \
  -o /tmp/createos-test .
/tmp/createos-test <your-command>
# wait ~10s for posthog-go batch flush + 3s Shutdown
# then query PostHog HogQL: SELECT event, properties FROM events WHERE timestamp > now() - INTERVAL 5 MINUTE
```

Run the anti-pattern grep audit from the plan (`docs/superpowers/plans/2026-05-01-posthog-telemetry-plan.md` §Phase 7) before merging.
