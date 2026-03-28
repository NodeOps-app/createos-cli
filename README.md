# CreateOS CLI

```
 ██████╗██████╗ ███████╗ █████╗ ████████╗███████╗ ██████╗ ███████╗
██╔════╝██╔══██╗██╔════╝██╔══██╗╚══██╔══╝██╔════╝██╔═══██╗██╔════╝
██║     ██████╔╝█████╗  ███████║   ██║   █████╗  ██║   ██║███████╗
██║     ██╔══██╗██╔══╝  ██╔══██║   ██║   ██╔══╝  ██║   ██║╚════██║
╚██████╗██║  ██║███████╗██║  ██║   ██║   ███████╗╚██████╔╝███████║
 ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝   ╚═╝   ╚══════╝ ╚═════╝ ╚══════╝
  Your intelligent infrastructure CLI
```

The official command-line interface for [CreateOS](https://createos.nodeops.network?utm_source=createos-cli) — manage your projects and skills from the terminal.

## Installation

Requires Go 1.21+.

```bash
git clone https://github.com/NodeOps-app/createos-cli
cd createos-cli
go build -o createos .
```

## Getting started

**1. Sign in**

Choose one of the following methods:

**Option A — Browser login (OAuth, recommended)**

```bash
createos login
```

This opens your browser to complete sign in. Your session is saved automatically.

**Option B — API token**

Get your API token from your [CreateOS dashboard](https://createos.nodeops.network/profile), then run:

```bash
createos login --token <your-api-token>
```

Or run `createos login` interactively and select "Sign in with API token" when prompted.

> In CI or non-interactive environments, you must use the `--token` flag.

**2. Confirm your account**

```bash
createos whoami
```

**3. Link your project**

```bash
createos init
```

**4. Explore commands**

```bash
createos --help
```

## Commands

### Authentication

| Command           | Description                           |
| ----------------- | ------------------------------------- |
| `createos login`  | Sign in with browser or API token     |
| `createos logout` | Sign out                              |
| `createos whoami` | Show the currently authenticated user |

### Projects

| Command                    | Description      |
| -------------------------- | ---------------- |
| `createos projects list`   | List all projects |
| `createos projects get`    | Get project details |
| `createos projects delete` | Delete a project |

### Deployments

| Command                           | Description                          |
| --------------------------------- | ------------------------------------ |
| `createos deployments list`       | List deployments for a project       |
| `createos deployments logs`       | Stream runtime logs for a deployment |
| `createos deployments build-logs` | Stream build logs for a deployment   |
| `createos deployments retrigger`  | Retrigger a deployment               |
| `createos deployments wakeup`     | Wake up a sleeping deployment        |
| `createos deployments cancel`     | Cancel a deployment                  |

### Environments

| Command                        | Description                     |
| ------------------------------ | ------------------------------- |
| `createos environments list`   | List environments for a project |
| `createos environments delete` | Delete an environment           |

### Environment Variables

| Command                | Description                                       |
| ---------------------- | ------------------------------------------------- |
| `createos env list`    | List environment variables for a project          |
| `createos env set`     | Set one or more environment variables             |
| `createos env rm`      | Remove an environment variable                    |
| `createos env pull`    | Download environment variables to a local `.env` file |
| `createos env push`    | Upload environment variables from a local `.env` file |

### Domains

| Command                    | Description                       |
| -------------------------- | --------------------------------- |
| `createos domains list`    | List custom domains for a project |
| `createos domains add`     | Add a custom domain               |
| `createos domains verify`  | Check DNS propagation and wait for verification |
| `createos domains delete`  | Remove a custom domain            |

### Templates

| Command                    | Description                                   |
| -------------------------- | --------------------------------------------- |
| `createos templates list`  | Browse available project templates            |
| `createos templates info`  | Show details about a template                 |
| `createos templates use`   | Download and scaffold a project from a template |

### VMs

| Command                  | Description                     |
| ------------------------ | ------------------------------- |
| `createos vms list`      | List VM instances               |
| `createos vms get`       | Get details of a VM             |
| `createos vms deploy`    | Deploy a new VM                 |
| `createos vms ssh`       | Connect to a VM via SSH         |
| `createos vms reboot`    | Reboot a VM                     |
| `createos vms resize`    | Resize a VM to a different plan |
| `createos vms terminate` | Permanently destroy a VM        |

### Skills

| Command                     | Description                |
| --------------------------- | -------------------------- |
| `createos skills catalog`   | Browse the skills catalog  |
| `createos skills purchased` | List your purchased skills |

### Quick Actions

| Command           | Description                                       |
| ----------------- | ------------------------------------------------- |
| `createos init`   | Link the current directory to a CreateOS project  |
| `createos status` | Show a project's health and deployment status     |
| `createos open`   | Open a project's live URL in your browser         |
| `createos scale`  | Adjust replicas and resources for an environment  |

### OAuth

| Command                         | Description              |
| ------------------------------- | ------------------------ |
| `createos oauth clients list`   | List OAuth clients       |
| `createos oauth clients create` | Create a new OAuth client |

### Users

| Command                                | Description             |
| -------------------------------------- | ----------------------- |
| `createos users oauth-consents list`   | List OAuth consents     |
| `createos users oauth-consents revoke` | Revoke an OAuth consent |

### Other

| Command               | Description                                             |
| --------------------- | ------------------------------------------------------- |
| `createos ask`        | Ask the AI assistant to help manage your infrastructure |
| `createos completion` | Generate shell completion script (bash/zsh/fish/powershell) |
| `createos version`    | Print the current version                               |

## Non-interactive / CI usage

All commands that would normally show an interactive prompt accept flags instead:

```bash
# Projects
createos projects get --project <id>

# Deployments
createos deployments logs --project <id> --deployment <id>
createos deployments retrigger --project <id> --deployment <id>

# Environments
createos environments delete --project <id> --environment <id>

# Environment variables
createos env list --project <id> --environment <id>
createos env set KEY=value --project <id> --environment <id>
createos env pull --project <id> --environment <id> --force

# Domains
createos domains add example.com --project <id>
createos domains verify --project <id> --domain <id> --no-wait
createos domains delete --project <id> --domain <id>

# Templates
createos templates use --template <id> --yes

# VMs
createos vms get --vm <id>
createos vms reboot --vm <id> --force
createos vms terminate --vm <id> --force
createos vms resize --vm <id> --size 1
createos vms deploy --zone nyc3 --size 1
```

## Options

| Flag          | Description                                           |
| ------------- | ----------------------------------------------------- |
| `--output json` | Output results as JSON (supported on most list/get commands) |
| `--debug, -d` | Print HTTP request/response details (token is masked) |
| `--api-url`   | Override the API base URL                             |

## Security

- Your API token is stored at `~/.createos/.token` with `600` permissions (readable only by you).
- OAuth session tokens are stored at `~/.createos/.oauth` with `600` permissions (readable only by you).
- Debug mode masks your token in output — only the first 6 and last 4 characters are shown.
- Never share your token or commit it to version control.
