# CreateOS CLI — Development Guide

## Cross-repo mesh — CreateOS Sandbox

**You are in `createos-cli` — the public Go CLI.** The `fc` control plane
defines the API this calls; `createos-plugin` wraps this CLI; the website
mirrors these commands. On any command / flag change, run the protocol below —
the concrete website-regen steps live in "Keeping the Website Docs in Sync" near
the end of this file. This repo is one of five in the product mesh.

| repo | role | public? |
|---|---|---|
| **fc** | control-plane — source of truth | 🔒 private |
| **fc-sdk** | TypeScript SDK + `examples/` | 🌐 public |
| **createos-cli** | Go CLI | 🌐 public |
| **website-04** (`content/docs/Sandbox`) | public docs | 🌐 public |
| **createos-plugin** | Claude Code plugin over the `createos` CLI | 🌐 public |

**Shared surface** = HTTP endpoint or method · wire or JSON field · error shape ·
sandbox lifecycle/state · limit or quota · CLI command or flag · public SDK
method · documented behavior. Internals — refactor, private helper, test-only —
are not, so skip the mesh for them.

On a shared-surface change: search every sibling for the touched symbol
(`semble search` first, then `rg`), report a per-sibling matrix
(`already-present` / `missing-needs-update` / `n/a`) before finalizing, and never
silently duplicate what is already there. Origin matters — `fc` is upstream; a
downstream change implying new server behavior goes to the user, never invented
inside a client. **`fc` → any public repo is a leak-guard boundary:** strip
private implementation, security internals, infra, threat-model notes, and
internal-only tooling (`fcctl`, host filesystem paths, mTLS/CA internals),
respect each public repo's own wording rules (e.g. `fc-sdk/AGENTS.md` forbids
the word "VM"), and get approval before landing any public edit. The `sync-docs`
skill executes SDK / CLI / website reconciliation against upstream `fc`.

### Downstream reference implementations (not mesh-protocol members)

Two public repos shell out to this CLI and are worth checking before a
command/flag/help-text change ships, even though neither owns shared
surface and both sit outside the formal 5-repo protocol above:

- **createos-plugin** (`../createos-plugin/createos-sandbox`) — the primary
  CLI wrapper; most exposed, since it parses `createos` stdout.
- **createos-sandbox-ghar** (`../createos-sandbox-ghar`) — its
  `.github/workflows/bump-runner.yml` daily job shells out to this CLI to
  rebuild the `ghar-runner` rootfs template; a flag/output change here can
  break that job silently.

## Project Structure

```
cmd/
  auth/        login, logout, whoami commands
  projects/    projects subcommands (list, get, delete)
  skills/      skills subcommands (catalog, purchased)
  root/        app wiring, Before hook, default action
internal/
  api/         resty client, types, all API methods
  config/      token storage (~/.createos/.token)
  intro/       ASCII banner
  ui/          interactive TUI components (skills catalog)
main.go        entry point — error display only
```

## CLI Framework

Uses `github.com/urfave/cli/v2`. Commands are registered in `cmd/root/root.go`.

When adding a new command:
1. Create the file under `cmd/<group>/`
2. Register it in the group's `NewXxxCommand()` subcommands slice
3. Add it to the manual list in `root.go` Action (the home screen) in alphabetical order

## API Client

### Response shapes

The API has two response shapes — use the right one:

| Shape | Type | When |
|-------|------|------|
| Single item | `Response[T]` | `GET /resource/:id`, `POST` |
| Paginated list | `PaginatedResponse[T]` | `GET /resource` (list endpoints) |

`PaginatedResponse` wraps items under `data.data[]` with a `data.pagination` object. Do **not** use `Response[[]T]` for list endpoints — the API returns an object, not a direct array.

### Adding a new API method

1. Define the model struct in `internal/api/methods.go` (or `types.go` for shared types)
2. Match field names exactly to the JSON response — use nullable pointers (`*string`) for fields that can be `null`
3. For errors, return `ParseAPIError(resp.StatusCode(), resp.Body())` — never `fmt.Errorf("API error %d: %s", ...)`

## Machine-readable Output

Decisions and rejected alternatives: `docs/decisions.md`.

### Every mutation must emit JSON

A `sandbox` command that creates, changes, or deletes something calls
`renderResult` (`cmd/sandbox/jsonout.go`), never a bare `pterm.Success`:

```go
renderResult(c, "created", map[string]any{
    "id":   resp.ID,
    "name": str(resp.Name),
}, func() {
    pterm.Success.Printfln("Created %s", resp.ID)
})
```

The human renderer goes in the closure; it runs only in table mode. Rules:

- `action` names what happened (past tense: `created`, `paused`, `disk_attached`).
- Field names match the read commands — a caller diffs `create` against `get`.
- Nullable API pointers go through `str()` so a key is always present.
- Wrap the fields in `withResponse(resp, …)` when the API returns a struct, so
  callers also get everything the server sent. Curated keys win on collision.
- Never branch the API call on output format. One call path serves both; the
  spinner already writes to stderr. Two paths drift — that is how a JSON-mode
  `fork` once skipped its status check and reported a failed fork as success.
- Error strings in results go through `api.UserMessageVerbose(err)`, never
  `err.Error()` — raw Go errors leak syscall detail and local paths into JSON
  just as readily as into the terminal.
- Batch commands return a `results` array with one entry per ref, plus
  `deleted` / `failed` counts. An exit code cannot express a partial batch.
- Interactive streams (`shell`, `sync`, `editor`, `exec --stream`,
  `template logs`) stay text-only. Blocking commands that have a result
  (`tunnel`, `vpn up`) emit it *before* they block.

### Two JSON checks — pick the right one

| Helper | Use for |
|---|---|
| `output.IsJSON` / `output.Render` | Normal commands. True when `--output json` **or** stdout is not a TTY. |
| `output.IsJSONExplicit` | Commands whose stdout **is** the payload (`exec`). Only true when the user typed `--output json`, so `exec … > file` still writes raw bytes. |

### Streams

- **stdout** — data only. In JSON mode it holds exactly one document; the
  root `Before` hook redirects all pterm output to stderr to guarantee it.
- **stderr** — errors, progress, hints, spinners.
- Errors are formatted in `main.go`: a JSON envelope on stdout in JSON mode,
  otherwise plain text on stderr. Add new status codes to
  `api.APIError.Code()`, which produces the envelope's `code` slug.
- Colour is disabled automatically for non-TTY stdout and for `NO_COLOR`.

### Global flags

`internal/cliargs.Hoist` rewrites argv before `app.Run` so global flags work
after the subcommand. **When adding or renaming a global flag in
`root.go`, add it to `globalStringFlags` / `globalBoolFlags` too** — a
missing entry silently reintroduces the "flag provided but not defined"
error. Tokens after a bare `--` are never hoisted.

## Error Handling

### API errors

All API errors go through `ParseAPIError` which:
- Parses the `{"status":"fail","data":"..."}` envelope
- Extracts the human-readable message from `data`
- Returns an `*APIError` with `StatusCode` and `Message`

`main.go` then displays it via `pterm.Error` and appends a contextual `Hint()` based on status code:
- `400` → "Check that the value you provided is correct"
- `401/403` → "Run 'createos login' to sign in again"
- `404` → "Double-check the ID. Run the list command to see available items"

### User-facing error messages

Write errors as if talking to a non-technical user:
- No jargon, no stack traces, no raw JSON
- Say what went wrong in plain English
- Always tell them what to do next

```go
// Bad
return fmt.Errorf("project ID is required")

// Good
return fmt.Errorf("please provide a project ID\n\n  To see your projects and their IDs, run:\n    createos projects list")
```

### Auth errors

Use the consistent phrasing: `"you're not signed in — run 'createos login' to get started"`

Never expose the token file path in error messages shown during normal usage.

## Display & UX

### pterm usage

| Situation | Use |
|-----------|-----|
| Success action | `pterm.Success.Println(...)` |
| Error (in main.go) | `pterm.Error.Println(...)` |
| Field labels in detail views | `pterm.NewStyle(pterm.FgCyan)` |
| Tabular data | `pterm.DefaultTable.WithHasHeader().WithData(...).Render()` |
| Hints / secondary info | `pterm.Println(pterm.Gray("  Hint: ..."))` |
| Interactive confirm | `pterm.DefaultInteractiveConfirm` |
| Password input | `pterm.DefaultInteractiveTextInput.WithMask("*")` |

### Command descriptions

Every command must have:
- `Usage` — one short line, plain English, no "by ID" jargon
- `ArgsUsage` — e.g. `<project-id>` for positional args
- `Description` (optional but preferred for destructive commands) — multi-line with examples

### Home screen

The default `Action` in `root.go` manually prints available commands. Keep it in **alphabetical order** and update it whenever a new top-level command is added.

### Empty states

```go
// Bad
fmt.Println("No projects found.")

// Good
fmt.Println("You don't have any projects yet.")
```

Always suggest a next action in empty states where applicable.

## Pre-commit Hooks

The repo uses [pre-commit](https://pre-commit.com) with the following hooks:

| Hook | What it does |
|------|-------------|
| `detect-secrets` | Scans for accidentally committed secrets (Yelp detect-secrets v1.5.0) |
| `go-vet` | Runs `go vet ./...` on changed Go files |
| `go-build-tmp` | Builds the binary to a temp dir and removes it on success |

### First-time setup

```bash
pre-commit install
detect-secrets scan > .secrets.baseline
```

`.secrets.baseline` must be committed. Update it when a false positive is audited:

```bash
detect-secrets audit .secrets.baseline
```

## Adding a New Command Group

1. Create `cmd/<group>/` directory
2. Create `<group>.go` with `NewXxxCommand()` returning `*cli.Command` with subcommands
3. Import and register in `cmd/root/root.go` `Commands` slice
4. Add to the home screen manual list in `root.go` Action, alphabetically

## Keeping the Website Docs in Sync

Any change to a command, flag, or its behavior must be mirrored in the
website docs repo at `../website-04`. The CLI and the published docs are
two separate repos — editing one does **not** update the other.

Source-of-truth markdown (edit these by hand):

| File | Covers |
|------|--------|
| `content/docs/Sandbox/CLI/Commands.md` | Per-command flag tables + examples (sandbox group) |
| `content/docs/CLI/Command-Reference.md` | One-line summary row per command |

After editing the markdown, regenerate the derived files (they carry an
"Auto-generated — do not edit manually" header):

```bash
cd ../website-04
node scripts/generate-docs-content.mjs    # → lib/docs/docs-content.ts
node scripts/generate-search-index.mjs    # → lib/docs/search-index-generated.ts
```

Don't hand-edit the `lib/docs/*-generated.ts` files and don't run the
full `prebuild` (it also fetches remote content). Commit the markdown +
both regenerated TS files together.

Checklist when adding/changing/removing a command or flag:
- [ ] CLI code under `cmd/`
- [ ] This repo's `README.md`
- [ ] `../website-04` `Commands.md` (and `Command-Reference.md` for new commands)
- [ ] Regenerate the two `lib/docs/*.ts` files
