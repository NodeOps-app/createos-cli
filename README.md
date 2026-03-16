# CreateOS CLI

The official command-line interface for [CreateOS](https://createos.io) — manage your projects and skills from the terminal.

## Installation

### Download binary

Download the latest release for your platform from the [Releases](../../releases) page and place it somewhere on your `$PATH`.

### Build from source

Requires Go 1.21+.

```bash
git clone https://github.com/NodeOps-app/createos-cli
cd createos-cli
go build -o createos .
```

## Getting started

**1. Sign in**

Get your API token from your [CreateOS dashboard](https://createos.io/settings/tokens), then run:

```bash
createos login
```

**2. Confirm your account**

```bash
createos whoami
```

**3. Explore commands**

```bash
createos --help
```

## Commands

| Command | Description |
|---------|-------------|
| `createos login` | Sign in with your API token |
| `createos logout` | Sign out |
| `createos whoami` | Show your account info |
| `createos projects list` | List all your projects |
| `createos projects get <id>` | Show details for a project |
| `createos projects delete <id>` | Delete a project |
| `createos skills catalog` | Browse available skills |
| `createos skills purchased` | List your purchased skills |

## Options

| Flag | Description |
|------|-------------|
| `--debug, -d` | Print HTTP request/response details (token is masked) |
| `--api-url` | Override the API base URL |

## Security

- Your API token is stored at `~/.createos/.token` with `600` permissions (readable only by you).
- Debug mode masks your token in output — only the first 6 and last 4 characters are shown.
- Never share your token or commit it to version control.

## License

MIT
