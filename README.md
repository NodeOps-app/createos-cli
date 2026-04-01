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

### macOS & Linux — install script (recommended)

```sh
curl -sfL https://raw.githubusercontent.com/NodeOps-app/createos-cli/main/install.sh | sh -
```

### macOS & Linux — Homebrew

```sh
brew tap nodeops-app/tap
brew install createos
```

### Nightly

Built daily from the latest commit on `main`. May contain unreleased features.

```sh
# Install script
curl -sfL https://raw.githubusercontent.com/NodeOps-app/createos-cli/main/install.sh | CREATEOS_CHANNEL=nightly sh -

# Homebrew
brew install createos --HEAD
```

### Pin a specific version

```sh
curl -sfL https://raw.githubusercontent.com/NodeOps-app/createos-cli/main/install.sh | CREATEOS_VERSION=v0.0.3 sh -
```

### Upgrade

```sh
createos upgrade
```

Or via Homebrew:

```sh
brew upgrade createos
```

### Build from source

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

| Command                    | Description         |
| -------------------------- | ------------------- |
| `createos projects list`   | List all projects   |
| `createos projects get`    | Get project details |
| `createos projects delete` | Delete a project    |

### Deploy

| Command                 | Description                                              |
| ----------------------- | -------------------------------------------------------- |
| `createos deploy`       | Deploy your project (auto-detects type)                  |

**Deploy flags:**

| Flag         | Description                                                        |
| ------------ | ------------------------------------------------------------------ |
| `--project`  | Project ID (auto-detected from `.createos.json`)                   |
| `--branch`   | Branch to deploy from (VCS/GitHub projects only)                   |
| `--image`    | Docker image to deploy (image projects only, e.g. `nginx:latest`)  |
| `--dir`      | Directory to zip and upload (upload projects only, default: `.`)   |

**Deploy behaviour by project type:**

| Project type   | What happens                                                                          |
| -------------- | ------------------------------------------------------------------------------------- |
| VCS / GitHub   | Triggers from the latest commit. Prompts for branch interactively if not provided.   |
| Upload         | Zips the local directory (respects `.gitignore`), uploads, and streams build logs.   |
| Image          | Deploys the specified Docker image.                                                   |

**Files excluded from upload zip:**

Sensitive and noisy files are always excluded: `.env`, `.env.*`, secrets/keys (`*.pem`, `*.key`, `*.p12`, etc.), `node_modules`, build artifacts (`target`, `coverage`, etc.), OS/editor files, and anything listed in your project's `.gitignore`.

### Deployments

| Command                           | Description                          |
| --------------------------------- | ------------------------------------ |
| `createos deployments list`       | List deployments for a project       |
| `createos deployments logs`       | Stream runtime logs for a deployment |
| `createos deployments build-logs` | Stream build logs for a deployment   |
| `createos deployments retrigger`  | Retrigger a deployment               |
| `createos deployments wakeup`     | Wake up a sleeping deployment        |
| `createos deployments cancel`     | Cancel a running deployment          |

### Environments

| Command                        | Description                     |
| ------------------------------ | ------------------------------- |
| `createos environments list`   | List environments for a project |
| `createos environments delete` | Delete an environment           |

### Environment Variables

| Command             | Description                                           |
| ------------------- | ----------------------------------------------------- |
| `createos env list` | List environment variables for a project              |
| `createos env set`  | Set one or more environment variables                 |
| `createos env rm`   | Remove an environment variable                        |
| `createos env pull` | Download environment variables to a local `.env` file |
| `createos env push` | Upload environment variables from a local `.env` file |

### Domains

| Command                   | Description                                     |
| ------------------------- | ----------------------------------------------- |
| `createos domains list`   | List custom domains for a project               |
| `createos domains create` | Create a custom domain for a project            |
| `createos domains verify` | Check DNS propagation and wait for verification |
| `createos domains delete` | Delete a custom domain                          |

### Cron Jobs

| Command                          | Description                            |
| -------------------------------- | -------------------------------------- |
| `createos cronjobs list`         | List cron jobs for a project           |
| `createos cronjobs create`       | Create a new HTTP cron job             |
| `createos cronjobs get`          | Show details for a cron job (including path, method, headers, body) |
| `createos cronjobs update`       | Update a cron job's name, schedule, or HTTP settings |
| `createos cronjobs suspend`      | Pause a cron job                       |
| `createos cronjobs unsuspend`    | Resume a suspended cron job            |
| `createos cronjobs activities`   | Show recent execution history          |
| `createos cronjobs delete`       | Delete a cron job                      |

**HTTP settings flags** (for `create` and `update`):

| Flag | Description |
| ---- | ----------- |
| `--path` | HTTP path to call, must start with `/` |
| `--method` | HTTP method: `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD` |
| `--header` | Header in `Key=Value` format, repeatable |
| `--body` | JSON body to send with the request (only for POST, PUT, PATCH) |

### Templates

| Command                   | Description                                      |
| ------------------------- | ------------------------------------------------ |
| `createos templates list` | Browse available project templates               |
| `createos templates info` | Show details about a template                    |
| `createos templates use`  | Download and scaffold a project from a template  |

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

| Command           | Description                                      |
| ----------------- | ------------------------------------------------ |
| `createos init`   | Link the current directory to a CreateOS project |
| `createos status` | Show a project's health and deployment status    |
| `createos open`   | Open a project's live URL in your browser        |
| `createos scale`  | Adjust replicas and resources for an environment |

### OAuth Clients

| Command                               | Description                          |
| ------------------------------------- | ------------------------------------ |
| `createos oauth-clients list`         | List your OAuth clients              |
| `createos oauth-clients create`       | Create a new OAuth client            |
| `createos oauth-clients instructions` | Show setup instructions for a client |
| `createos oauth-clients delete`       | Delete an OAuth client               |

### Me

| Command                             | Description             |
| ----------------------------------- | ----------------------- |
| `createos me oauth-consents list`   | List OAuth consents     |
| `createos me oauth-consents revoke` | Revoke an OAuth consent |

### Other

| Command               | Description                                             |
| --------------------- | ------------------------------------------------------- |
| `createos ask`        | Ask the AI assistant to help manage your infrastructure |
| `createos upgrade`    | Upgrade createos to the latest version                  |
| `createos version`    | Print the current version                               |

## Non-interactive / CI usage

All commands accept flags so they work in CI and non-interactive environments. Destructive commands require `--force` to skip the confirmation prompt.

```bash
# Deploy
createos deploy                                      # upload project — zips current dir
createos deploy --dir ./dist                         # upload project — zip a specific dir
createos deploy --branch main                        # VCS project — deploy from main
createos deploy --image nginx:latest                 # image project

# Projects
createos projects get --project <id>
createos projects delete --project <id> --force

# Deployments
createos deployments list --project <id>
createos deployments logs --project <id> --deployment <id>
createos deployments build-logs --project <id> --deployment <id>
createos deployments retrigger --project <id> --deployment <id>
createos deployments wakeup --project <id> --deployment <id>
createos deployments cancel --project <id> --deployment <id> --force

# Environments
createos environments list --project <id>
createos environments delete --project <id> --environment <id> --force

# Environment variables
createos env list --project <id> --environment <id>
createos env set KEY=value --project <id> --environment <id>
createos env rm KEY --project <id> --environment <id>
createos env pull --project <id> --environment <id> --force
createos env push --project <id> --environment <id> --force

# Domains
createos domains list --project <id>
createos domains create --project <id> --name example.com
createos domains verify --project <id> --domain <id> --no-wait
createos domains delete --project <id> --domain <id> --force

# Cron jobs
createos cronjobs list --project <id>

# Simple GET cron job
createos cronjobs create --project <id> --environment <id> \
  --name "Cleanup job" --schedule "0 * * * *" \
  --path /api/cleanup --method GET

# POST cron job with headers and JSON body
createos cronjobs create --project <id> --environment <id> \
  --name "Webhook" --schedule "*/5 * * * *" \
  --path /api/hook --method POST \
  --header "Authorization=Bearer token" --header "X-Source=cron" \
  --body '{"event":"tick"}'

# Update HTTP settings (headers and body preserved if omitted)
createos cronjobs update --project <id> --cronjob <id> \
  --path /api/hook --method POST \
  --header "Authorization=Bearer token" --body '{"event":"tick"}'

createos cronjobs get --project <id> --cronjob <id>
createos cronjobs delete --project <id> --cronjob <id> --force

# Templates
createos templates use --template <id> --yes

# VMs
createos vms list
createos vms get --vm <id>
createos vms deploy --zone nyc3 --size 1 --name "my-vm" --ssh-key "ssh-ed25519 ..."
createos vms reboot --vm <id> --force
createos vms terminate --vm <id> --force
createos vms resize --vm <id> --size 1

# OAuth clients
createos oauth-clients list
createos oauth-clients create \
  --name "My App" \
  --redirect-uri https://myapp.com/callback \
  --app-url https://myapp.com \
  --policy-url https://myapp.com/privacy \
  --tos-url https://myapp.com/tos \
  --logo-url https://myapp.com/logo.png
createos oauth-clients instructions --client <id>
createos oauth-clients delete --client <id> --force

# Me
createos me oauth-consents list
createos me oauth-consents revoke --client <id> --force
```

## JSON output and piping

All list and get commands output JSON automatically when stdout is a pipe, so `| jq` works without any flags:

```bash
createos projects list | jq '.[].id'
createos deployments list --project <id> | jq '.[] | select(.status == "running")'
createos cronjobs list --project <id> | jq '.[] | {id, name, schedule}'
createos vms list | jq '.[].extra.ip_address'
```

To force JSON output in a TTY, use `--output json` (or `-o json`):

```bash
createos projects get --project <id> --output json
createos environments list --project <id> -o json
```

## Options

| Flag                  | Description                                                          |
| --------------------- | -------------------------------------------------------------------- |
| `--output, -o <fmt>`  | Output format: `json` or `table` (default). Auto-json when piped.   |
| `--debug, -d`         | Print HTTP request/response details (token is masked)                |
| `--api-url`           | Override the API base URL                                            |

## Security

- Your API token is stored at `~/.createos/.token` with `600` permissions (readable only by you).
- OAuth session tokens are stored at `~/.createos/.oauth` with `600` permissions (readable only by you).
- Debug mode masks your token in output — only the first 6 and last 4 characters are shown.
- Never share your token or commit it to version control.
