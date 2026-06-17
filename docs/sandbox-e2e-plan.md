# Sandbox CLI E2E Test Suite — Plan

Status: **proposed** (awaiting sign-off). Live-backend suite that drives the
compiled `createos` binary against the real fc-spawn sandbox API from the CLI
user's perspective.

## Decisions (locked)

| # | Decision | Choice |
|---|----------|--------|
| Q1 | Test layer | **Live backend integration** — real API, real sandboxes, real auth |
| Q2 | Harness | **Go `testing` + `os/exec`**, gated `//go:build e2e` (excluded from `go test ./...`) |
| Q3/Q4 | Sandbox ID capture | **Add `-o json` to mutating commands** (product change), parse JSON directly |
| Q4 | JSON scope | All **state-returning mutators**: `create`, `fork`, `pause`, `resume`, `edit`, `rm`. Streaming (`shell`/`sync`/`tunnel`) untouched; `exec` keeps raw stdout |
| Q5 | Cleanup | **`t.Cleanup` per sandbox + `TestMain` pre/post sweep** of `e2e-<runid>-*` |
| Q6 | Auth | **Temp HOME + `login --token $CREATEOS_E2E_API_KEY`**; `t.Skip` whole suite if unset |
| Q7 | Coverage | **Tier 1 full live now**; Tier 2/3 documented TODO (below) |
| Q8 | Execution | **Sandbox per test**, serial (no `t.Parallel`) |
| Q9 | Readiness | **`waitRunning` poll `get -o json` until `status==running`** + layered timeouts |
| Q10 | Fixture | **Discover smallest shape** via `shapes -o json`; default rootfs; env overrides |
| Q11 | CI | **`workflow_dispatch` + nightly cron**, secret-gated; never per-PR |
| Q12 | Delivery | **This plan doc first**, then single implementation pass |

## Key codebase facts

- Global flag `--output/-o {table,json}` exists. `internal/output.DetectFormat`
  returns `json` automatically when **stdout is not a TTY** → tests via
  `os/exec` get JSON for any command using `output.Render`, no flag needed.
- Commands already honoring JSON: `list`, `get`, `catalog`, `shapes`(catalog),
  `rootfs`, `template`, `network`, `firewall`, `disk`.
- Commands printing pterm unconditionally (need wiring): `create`, `fork`,
  `pause`, `resume`, `edit`, `rm`, + streaming/interactive (out of scope).
- Auth: `createos login --token <key>` writes `~/.createos/.token`
  non-interactively. No env-var token path → isolate via temp `HOME`.
- `wait` is an internal helper (`cmd/sandbox/wait.go`), **not** a CLI command.
- Tier-1 transports are pure HTTP API (`ExecSandbox`, `UploadFile`,
  `DownloadFile`) — **no SSH key / mutagen needed**. SSH/mutagen only Tier 3.

### Relevant CLI surfaces

```
sandbox create  --shape --name --rootfs --disk-mib --ssh-key --egress --env --ingress --auto-pause
sandbox rm      [<id> …]  --force/-y/-yes        (force required non-interactive)
sandbox exec    <sandbox> -- <cmd> [args…]  --stream --env
sandbox push    <sandbox> <local-path> <remote-path>
sandbox pull    <sandbox> <remote-path> <local-path|->
sandbox get     <id>                            (json)
sandbox list                                    (json)
sandbox shapes                                  (json)
```

### JSON shapes asserted

- `SandboxView`: `id`, `name`, `shape`, `rootfs`, `status`, `ip`,
  `ingress_enabled`, `auto_pause_after_seconds`, …
- `SandboxExecResult`: `stdout`, `stderr`, `exit_code`.

## Product change (prerequisite — separate commit)

`feat(sandbox): json output for mutating commands`

Wrap final human print in `output.Render(c, resp, func(){ <existing pterm> })`
for: `create`, `fork`, `pause`, `resume`, `edit`, `rm`. Behavior unchanged on a
TTY (table/pterm); non-TTY or `-o json` emits the struct. `rm` JSON = deleted
id(s) + status.

Acceptance: `createos sandbox create --shape X -o json | jq .id` returns the id;
TTY output visually unchanged.

## Harness (`test/e2e/`)

All files `//go:build e2e`. Package `e2e`.

### `main_test.go` — `TestMain`
1. Read `CREATEOS_E2E_API_KEY`; if empty → `fmt.Println(skip msg)` + `os.Exit(0)`
   (suite is opt-in; absence is not a failure).
2. Create temp dir → set `HOME` (and `XDG_*`) so real `~/.createos/` untouched.
3. `runCLI("login", "--token", key)`.
4. Compute `runID` (short random; **no** `Date.now`-style host calls inside
   workflow scripts — plain `crypto/rand` in Go test is fine).
5. Discover smallest shape: `shapes -o json` → pick min vcpu+mem
   (override `CREATEOS_E2E_SHAPE` / `CREATEOS_E2E_ROOTFS`).
6. **Pre-sweep**: `list -o json` → `rm --force` every `e2e-*` orphan.
7. `m.Run()`.
8. **Post-sweep** (deferred): same as pre-sweep.

### `helpers_test.go`
- `runCLI(args...) (stdout, stderr string, exit int)` — `exec.CommandContext`
  with per-call timeout; binary path from `go build` once in TestMain (temp).
- `mustJSON[T](t, stdout) T` — decode + fatal on error.
- `newSandbox(t, opts...) SandboxView` — `create` with name
  `e2e-<runID>-<test>`, `t.Cleanup(rm --force)`, returns parsed view.
- `waitRunning(t, id)` — poll `get -o json` every 2s until `status==running`,
  cap 5m, fatal with last status on timeout.
- Timeouts: create 240s, exec 90s, push/pull 90s, default 60s, `go test -timeout 30m`.

### Naming/safety invariant
Every test sandbox name starts `e2e-`. Sweeps **only** match `e2e-*` — never a
real user sandbox. Sweep is the layered defense behind per-test `t.Cleanup`.

## Tier 1 tests (build now)

| File | Covers |
|------|--------|
| `readonly_test.go` | `shapes`, `catalog`, `rootfs`, `template list`, `list`, `get` — JSON parses, non-empty, schema fields present |
| `lifecycle_test.go` | `create → waitRunning → get` (assert shape/status) → `edit` (e.g. rename/auto-pause, re-`get` reflects) → `pause` (status `paused`) → `resume` (status `running`) → `fork` (new id, `e2e-` named, cleanup) → `rm --force` (gone from `list`) |
| `exec_test.go` | `exec <id> -- echo hello` → `exit_code==0`, stdout contains `hello`; non-zero cmd → non-zero `exit_code` surfaced |
| `files_test.go` | `push` local tmp file → `pull` back → byte-identical round-trip; `pull` to `-` streams to stdout |

Each test: own sandbox via `newSandbox`, serial, auto-cleaned.

## CI (`.github/workflows/e2e.yml`)

- Triggers: `workflow_dispatch` + nightly `schedule` (cron).
- Secret: `CREATEOS_E2E_API_KEY`.
- Steps: checkout → setup-go → `go build` → `go test -tags=e2e -timeout 30m ./test/e2e/...`.
- Plain PR CI keeps running `go test ./...` (no `e2e` tag) → fast, free, unaffected.

## Docs deliverable

This file + a "How to run" section appended on implementation:

## How to run

```bash
export CREATEOS_E2E_API_KEY=<your key>
go test -tags=e2e -timeout 30m ./test/e2e/... -v
```

Nightly and manual CI: `.github/workflows/e2e.yml` (`workflow_dispatch` or cron).

---

# Tier 2 — gated live (FUTURE TODO)

Run only when extra infra env present, else `t.Skip`. Each needs a real
external dependency the core suite deliberately avoids.

### `network` + `firewall`
- **Prereq**: ability to create a sandbox network (quota). Env gate: none beyond
  API key likely, but isolate net name `e2e-net-<runID>`.
- **Flow**: create network (`network create -o json` → id) → `create --network <id>`
  → `get -o json` shows network attached → `firewall` add/list/remove rule →
  assert rule present then absent → teardown sandbox then network.
- **Cleanup**: extend sweep to `e2e-net-*` networks (after sandbox sweep —
  ordering matters: detach/destroy sandboxes before their network).
- **Assertion risk**: firewall rule application may be async — may need a poll.

### `disk`
- **Prereq**: a real S3/R2 bucket the account can mount. Env gate
  `CREATEOS_E2E_BUCKET` (+ creds if separate). Skip when unset.
- **Flow**: `disk` attach `--disk <bucket>:/mnt/data` at create → `exec` writes a
  file to `/mnt/data` → (optionally) second sandbox mounts same disk → reads it
  back → assert persistence across sandbox lifetimes.
- **Cleanup**: bucket is external/shared — tests must write under a
  `e2e-<runID>/` prefix and delete only that; never wipe the bucket.
- **Cost note**: disk-backed sandboxes + storage egress add real cost.

# Tier 3 — smoke only (FUTURE TODO)

No true interactive e2e. Assert **CLI wiring + flag validation + fast-fail
errors**, not full session behavior. These need PTY/mutagen/port-forward
automation that is too flaky/infra-heavy to gate releases on yet.

### `shell`
- **Smoke**: `sandbox shell` with no/invalid sandbox id → friendly error +
  non-zero exit. `--help` lists flags. `shell <bad-id>` fails fast (not hang).
- **Deferred full e2e**: needs SSH gateway reachability + ephemeral keypair
  (`create --ssh-key`) + PTY driver (e.g. `creack/pty`) to type a command and
  read the prompt. Gateway default `gateway.sb.createos.sh:2222`.
- **Risk**: interactive resize (`shell_resize_*`), TTY detection, network egress
  to gateway. High flake surface.

### `sync`
- **Smoke**: arg validation (missing local/remote path), `mutagen` absence
  produces the install hint (`mutagen_install.go` path), `--help`.
- **Deferred full e2e**: requires `mutagen` binary installed in CI, a running
  sandbox with SSH, create a file locally → assert it appears in sandbox (poll)
  → edit in sandbox → assert local reflects. Bidirectional convergence is
  inherently timing-sensitive → generous polls.
- **Risk**: mutagen daemon lifecycle, session leak across tests, two-way conflict.

### `tunnel`
- **Smoke**: arg/flag validation, bad sandbox id fast-fail, `--help`.
- **Deferred full e2e**: long-running port-forward. Need a sandbox running a
  trivial HTTP server (via `exec`/`push` a server), open tunnel in a goroutine,
  `curl localhost:<port>` from the test host, assert response, then signal-stop
  the tunnel. Must guarantee the tunnel process is killed on cleanup (orphan
  process + held port otherwise).
- **Risk**: port allocation collisions under serial reruns, process teardown.

### Misc utility commands (audit later)
`bandwidth`, `slider`, `resolve`, `mutagen_install`, `wizard` — decide per
command whether smoke (flag/help/error) is worth it. `wizard` is interactive
(TTY-only) → smoke = non-TTY produces the "use --shape" headless error.
