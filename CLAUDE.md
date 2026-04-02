# CreateOS CLI — Development Guide

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
