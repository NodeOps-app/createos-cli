# CreateOS CLI — Early Access Guide

> You're one of the first developers to try the new CreateOS CLI. This guide gets you from install to managing your infrastructure from the terminal in under 5 minutes.

---

## Install

**macOS (Homebrew):**

```bash
brew install createos
```

**macOS / Linux (curl):**

```bash
curl -sfL https://raw.githubusercontent.com/NodeOps-app/createos-cli/main/install.sh | sh -
```

**Verify:**

```bash
createos version
```

You should see the CreateOS banner with the version number.

---

## Sign In

```bash
createos login
```

This opens your browser for OAuth sign-in. Once authenticated, you're set — the CLI remembers your session and auto-refreshes it.

**Already have an API key?** Use token-based auth instead:

```bash
createos login --token
```

**Verify you're signed in:**

```bash
createos whoami
```

---

## Link Your Project

If you have an existing project on CreateOS, link it to your local directory:

```bash
cd your-project/
createos init
```

This creates a `.createos.json` file that lets all other commands auto-detect your project — no more passing `--project <id>` everywhere.

**Already know your project ID?**

```bash
createos init --project <your-project-id>
```

---

## What You Can Do

### Check project health at a glance

```bash
createos status
```

Shows: deployment status, live URLs, environments, domains, recent deploys.

### Manage environment variables

```bash
# See what's set
createos env list

# Set variables
createos env set DATABASE_URL=postgres://... API_KEY=sk-xxx

# Pull to a local .env file
createos env pull

# Push from a local .env file
createos env push
```

### View deployment logs

```bash
# Latest logs
createos deployments logs

# Tail in real-time
createos deployments logs -f
```

### Manage custom domains

```bash
# Add a domain (shows DNS records to configure)
createos domains add <your-domain.com>

# Check verification status
createos domains verify
```

### Scale resources

```bash
# See current allocation
createos scale --show

# Adjust
createos scale --replicas 2 --cpu 300 --memory 512
```

### Schedule cron jobs

```bash
# Create a cron job that hits your endpoint every hour
createos cronjobs create --name "hourly-cleanup" --schedule "0 * * * *" --path /api/cleanup

# See execution history
createos cronjobs activities
```

### Browse and use templates

```bash
# See available templates
createos templates list

# Scaffold from a template
createos templates use <template-id>
```

### Open in browser

```bash
# Open your live project URL
createos open

# Open the CreateOS dashboard
createos open --dashboard
```

---

## CI / Non-Interactive Usage

Every command works in scripts and CI pipelines. Use flags instead of interactive prompts:

```bash
# Set env vars in CI
createos env set DATABASE_URL=$DB_URL --project <id> --environment <id>

# Check deployment logs
createos deployments logs --project <id> --deployment <id>

# JSON output for scripting
createos projects list --output json
createos status --output json
```

---

## Keep It Updated

The CLI checks for updates automatically. To upgrade:

```bash
createos upgrade
```

Or via Homebrew:

```bash
brew upgrade createos
```

---

## Deploy

```bash
# Deploy your project (auto-detects project type)
createos deploy

# Deploy from a specific branch (GitHub projects)
createos deploy --branch staging

# Deploy a Docker image
createos deploy --image myapp:v1.0
```

The CLI auto-detects your project type (GitHub VCS, upload, or Docker image) and deploys accordingly. Polls for status and prints your live URL when done.

---

## Feedback

**We'd love your feedback on the CLI.** What works well? What's confusing? What do you wish it did? Reply to this message — your input directly shapes what we build next.

---

## Quick Reference

| Command | What it does |
|---|---|
| `createos login` | Sign in |
| `createos init` | Link project to current directory |
| `createos deploy` | Deploy your project |
| `createos status` | Project health dashboard |
| `createos env list/set/rm/pull/push` | Manage environment variables |
| `createos deployments logs -f` | Tail logs in real-time |
| `createos domains add/verify` | Custom domains with DNS setup |
| `createos scale` | Adjust replicas, CPU, memory |
| `createos cronjobs create/list` | Scheduled HTTP tasks |
| `createos templates list/use` | Browse and scaffold projects |
| `createos open` | Open project in browser |
| `createos upgrade` | Self-update to latest version |

**Need help?** Run `createos <command> --help` for any command, or reach out to us directly.
