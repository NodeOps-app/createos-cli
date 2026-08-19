# CreateOS CLI — Development Guide

## Cross-repo mesh — CreateOS Sandbox

**You are in `createos-cli` — the public Go CLI.** The `fc` control plane
defines the API this calls; `createos-plugin` wraps this CLI; the website
mirrors these commands. On any command / flag change, run the protocol below —
the concrete website-regen steps live in "Keeping the Website Docs in Sync" near
the end of this file. This repo is one of five in the product mesh.

### Repo map

| repo | path | role | public? | changes that ripple across the mesh |
|---|---|---|---|---|
| **fc** | `../fc` | control-plane — **source of truth** | 🔒 private | HTTP API, wire/JSON fields, error shapes, lifecycle/state, limits/quotas, behavior |
| **fc-sdk** | `../fc-sdk` | TypeScript SDK **+ `examples/`** | 🌐 public | public SDK methods, wire types, example apps |
| **createos-cli** | `../createos-cli` | Go CLI | 🌐 public | commands, flags, help/UX text |
| **website-04** | `../website-04` (`content/docs/Sandbox`) | public docs | 🌐 public | REST / SDK / CLI reference + concept pages |
| **createos-plugin** | `../createos-plugin` | Claude Code plugin over the `createos` CLI | 🌐 public | skills, slash commands, hooks |

### What counts as a shared surface

HTTP endpoint or method · wire or JSON field · error shape · sandbox
lifecycle/state · limit or quota · CLI command or flag · public SDK method ·
documented behavior. A change confined to internals — refactor, comment,
private helper, test-only — is **not** a shared surface, so skip the mesh for it.

### Protocol — run before finalizing a shared-surface change

1. **Classify origin.** `fc` is the source of truth; SDK / CLI / docs / plugin
   are downstream consumers. A downstream change that implies a backend change
   (new field, new endpoint) → surface it to the user; never invent server
   behavior inside a client.
2. **Search every sibling** for the touched symbol / endpoint / flag —
   `semble search` first, then `rg`.
3. **Build a status matrix** per sibling: `already-present` ·
   `missing-needs-update` · `n/a`. **Flag the already-present ones to the user**
   ("already exists in fc-sdk + docs"). Never silently duplicate a change that is
   already there — that is the whole point of this check.
4. **`fc` → any public repo is a leak-guard boundary (security).** Strip private
   implementation, security internals, infra, threat-model notes, and
   internal-only tooling (`fcctl`, host filesystem paths, mTLS/CA internals)
   before anything lands in a public repo. Respect each public repo's own wording
   rules (e.g. `fc-sdk/AGENTS.md` forbids the word "VM"). Report the proposed diff
   and **ask for approval before landing any public edit** — never auto-write
   across the boundary.
5. **Use the `sync-docs` skill** to execute SDK / CLI / website reconciliation
   against upstream `fc` where it applies.

## Project Structure

```
cmd/
  auth/        login, logout, whoami commands
  projects/    projects subcommands (list, get, delete)
  root/        app wiring, Before hook, default action
internal/
  api/         resty client, types, all API methods
  config/      token storage (~/.createos/.token)
  intro/       ASCII banner
  ui/          interactive TUI components
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
