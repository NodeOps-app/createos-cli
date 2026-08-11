# Decisions

Running log of design decisions in this repo, with the options that were on
the table and why one won. Written for whoever — human or agent — picks this
up next.

## 2026-08-11 — Machine-readable output for CI and agents

An external review produced ~40 UX/DX/CI/agent findings. We shipped the five
that were blocking automation, plus the scope the user widened.

### D1. Errors go to stderr; JSON mode gets an error envelope

**Problem.** `main.go` printed errors with `pterm.Error`, whose default writer
is stdout. `createos … > out.json` captured the error text as data, and
`2>/dev/null` hid nothing.

Options considered:

- **(a) Print errors to stderr always.** Simple, conventional. In JSON mode a
  consumer reading only stdout gets an empty stream on failure and has to
  infer the error from the exit code.
- **(b) Error envelope on stdout in JSON mode, stderr otherwise.** ← chosen.
  A JSON consumer reading one stream always parses a valid document, success
  or failure. Human users get the Unix convention.
- (c) Envelope on stderr in JSON mode. Rejected: forces every consumer to
  merge two streams to find out what happened.

The envelope is `{"error":{"code","message"}}`. `code` is a stable slug
derived from the HTTP status (`api.APIError.Code()`), so callers branch on
the failure class instead of matching on message text.

### D2. `--output json` implies "stdout is only JSON"

`pterm.SetDefaultOutput(os.Stderr)` is set in the root `Before` hook whenever
JSON mode is active. Every pterm print in the codebase — including commands
not yet converted — lands on stderr, so no narration can corrupt the
document on stdout. Spinners already wrote to stderr (pterm's default).

### D3. `sandbox exec` uses `IsJSONExplicit`, not `IsJSON`

`output.DetectFormat` auto-selects JSON when stdout is not a TTY. For most
commands that is right. For `exec` it is actively wrong: stdout **is** the
payload, so `createos sandbox exec box -- cat data.csv > out` would have
written a JSON envelope instead of the file the caller expected.

So `exec` only wraps its result when the user typed `--output json`. Every
other command keeps the auto-detect behaviour. `output.IsJSONExplicit` marks
this distinction; use it for any future command whose stdout is a payload
rather than a report.

### D4. Global flags are hoisted in argv, not mirrored onto commands

**Problem.** urfave/cli v2 only parses app-level flags before the first
subcommand token, so `createos sandbox create --output json` died with
"flag provided but not defined: -output".

Options considered:

- **(a) Declare hidden copies of each global flag on every top-level command.**
  Rejected: the flags would *parse* but their values would never be read —
  the `App.Before` hook that consumes them runs before subcommand parsing, so
  `sandbox create --api-key X` would silently ignore the key. Silently-wrong
  is worse than the error it replaces.
- **(b) Re-detect the format in a per-command `Before` hook.** Fixes
  `--output` only; leaves `--api-key`, `--api-url`, `--debug` broken.
- **(c) Rewrite argv before `app.Run`.** ← chosen. `internal/cliargs.Hoist`
  moves known global flags (and their values) in front of the subcommand.
  One change, every flag and every command fixed, no per-command upkeep.

Tokens after a bare `--` are never touched, so `exec box -- ./ci.sh --debug`
keeps passing `--debug` to the user's script. `cmd/sandbox/exec.go`'s own
argv scan runs `Hoist` first, so it sees the same normalized line.

Verified safe: no subcommand declares any of the hoisted names or uses `-o`
/ `-d` as an alias.

### D5. Which sandbox commands emit JSON

The user's call: **all** sandbox subcommands, not just `create`/`fork`/`edit`.

Every command that creates, changes, or deletes now calls `renderResult`
(`cmd/sandbox/jsonout.go`) with an `action` field plus the ids needed to
chain the next call. Batch deletes (`rm`, `disk rm`, `network rm`,
`template rm`) return a per-ref `results` array so a caller can tell which
references succeeded — a single exit code cannot express a partial batch.

Left as human-readable, deliberately:

| Command | Why |
|---|---|
| `shell`, `editor`, `sync` | Interactive sessions. No single result to report. |
| `exec --stream` | The stream *is* the output; framing it would break live piping. |
| `template logs` | Same — a log stream. |
| `edit` interactive menu | TTY-only path; JSON mode never reaches it. |

`tunnel` and `vpn up` block until Ctrl-C, so they emit their JSON result
when the connection comes up rather than on exit — a caller needs the bound
address while the tunnel is alive, not after it dies.

### D6. `exec` stdin

`api.SandboxExecReq.Stdin` existed and was never populated. Now: piped stdin
is forwarded automatically, `--stdin FILE` reads from a file, `--stdin -`
forces reading the pipe. On a TTY with no `--stdin` nothing is read, so the
command does not block waiting on a keyboard.

Known limitation: consuming piped stdin means it is not available for
interactive prompts. In practice a caller that pipes data also passes the
sandbox ref and command explicitly, so no prompt is reached.

### D7. Reconciling with the JSON work that landed on main first

While this branch was open, `33c6f8a` ("fix: json output for create") added JSON
to `create`, `fork`, `disk create`, and `network create` by a different route:
an `if output.IsJSON(c)` early-return that repeats the API call and renders the
raw response struct. `57d2ebf` separately added `api.UserMessageVerbose()` to
stop raw Go errors leaking to users.

Resolved as follows:

- **Structure — kept the single path.** The early-return branch calls
  `client.CreateSandbox` twice in two places, so a change to one silently
  misses the other. It had already drifted: the `fork` branch skipped the
  `sb.Status != target` check, so a fork that ended in the wrong state
  reported success in JSON mode. The spinner writes to stderr, so one path
  serves both formats safely.
- **Payload — kept the whole response.** Rendering the raw struct returned
  more than the curated key set (`vcpu`, `mem_mib`, `disk_mib`, `egress`,
  quotas). Dropping those would regress anyone on today's `main`, so
  `withResponse` folds the response's own JSON fields in underneath the
  curated keys, which win on collision.
- **Sanitization — extended to JSON.** Batch results now carry
  `api.UserMessageVerbose(err)` rather than `err.Error()`. A raw Go error
  leaks syscall detail and local paths whichever stream it lands on, so the
  machine-readable field gets the same treatment as the human one.
- **`Hint()` — added to the envelope.** `main.go` on main had started showing
  `APIError.Hint()` to humans. The JSON envelope carries the same string as an
  optional `hint`, so an agent relaying a failure can pass on the same advice.
- **Arg order — theirs wins.** `05f45b6` swapped `network attach|detach` to
  `<network> <sandbox|device>`; the result objects were adapted to it.

### Not done (and why)

- **`--wait` on create/get** — real CI value, but a caller can poll `get`.
  Next tier, not blocking.
- **Cancel exit code 130** — correct convention; needs the ~52 scattered
  cancel strings unified in the same pass. Deferred as one coherent change.
- **`--quiet` on mutations** — superseded for machine callers by JSON output.
  Still worth it for shell users.
- **Shape price hints in the picker** — `api.Shape` carries no pricing. That
  is an `fc` change, not a CLI change.
