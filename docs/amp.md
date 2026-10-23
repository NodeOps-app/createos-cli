# Amp orbs on CreateOS

Run [Amp](https://ampcode.com) threads on CreateOS Sandboxes. Each thread gets
its own sandbox. It pauses when the thread is idle and resumes on the next
message.

```text
Amp (ampcode.com)          your machine                         CreateOS
─────────────────          ────────────                         ────────
new thread      ─ spawn ─▶ createos sandbox setup amp  ─ API ─▶ sandbox per thread
thread archived ─ pause ─▶   (leave it running)                   │
thread deleted  ─ delete ▶                                        │
agent tools  ◀════════════ amp orb bootstrap (outbound) ══════════╛
```

You need about 10 minutes, a Mac or Linux machine that stays on, a CreateOS
account, and an Amp account.

## 1. Install and sign in to the `createos` CLI

```sh
curl -sfL https://raw.githubusercontent.com/NodeOps-app/createos-cli/main/install.sh | sh -
createos login
```

Check that your version has the command. `amp` must be in this list:

```sh
createos sandbox setup --help
```

On a server without a browser, set `CREATEOS_API_KEY` instead of running
`createos login`.

## 2. Install and sign in to the `amp` CLI

```sh
curl -fsSL https://ampcode.com/install.sh | bash
amp login
```

`amp login` prints a link and a code. Open the link, check the code matches,
and approve.

## 3. Create an Amp workspace (skip if you have one)

Orb providers exist only inside a workspace, so a solo account does not show
the option until it has one. Workspaces are free.

1. Go to [ampcode.com/settings](https://ampcode.com/settings).
2. Click **Workspace Settings**.
3. Under **Create a Workspace**, fill in **Display Name** and **Short Name**, then
   click **Create Workspace**.

## 4. Add an orb provider and save its token

1. In **Workspace Settings**, open **Orbs** (under *Projects & Orbs*).
2. Check that **Self-Hosted Orbs** is on.
3. Click **Add Provider**, name it `createos`, and click **Add Provider**.
4. Copy the `aop_live_…` token. **Amp shows it only once.**

Save it to a file only you can read. Paste the token when asked; it is not shown:

```sh
mkdir -p ~/.config/amp
read -rs t && install -m 600 /dev/null ~/.config/amp/orb-provider-token && printf %s "$t" > ~/.config/amp/orb-provider-token && echo saved
```

## 5. Start the provider

```sh
export AMP_ORB_PROVIDER_TOKEN=$(cat ~/.config/amp/orb-provider-token)
createos sandbox setup amp --doctor   # checks only, starts nothing
createos sandbox setup amp            # leave this running
```

`--doctor` prints three lines:

```text
signed in to CreateOS
amp found at /Users/you/.local/bin/amp
AMP_ORB_PROVIDER_TOKEN is set
```

Then the provider waits for work:

```text
   script  ~/.createos/amp-dispatch-s-4vcpu-8gb-amp-orb
   amp     https://ampcode.com
   ctrl+c drains running jobs
17:52:58        READY    listening for jobs
```

Keep it running, for example in a `tmux` session. Amp updates itself in place
and restarts the provider on its own.

## 6. Start a thread on it

From the terminal:

```sh
amp -ox "your first message" --orb-provider createos
```

It prints the thread link and returns; the thread runs in Amp. In the Amp app,
pick **Orb → Self-Hosted → createos** in the thread composer instead.

The provider window shows each job:

```text
17:55:09  ●     SPAWN    amp-01a1…9b3fe32f        … claimed   1 running
17:55:16  │              › createos: created amp-272a8d15ccf60f518e (sb-…)
17:56:30  │              › createos: started amp orb bootstrap in amp-272a8d15ccf60f518e
17:56:30  ◆     SPAWN    amp-01a1…9b3fe32f        ✓ done      81.0s
```

## What happens next

| You do in Amp | Amp sends | What the CLI does | Measured |
| --- | --- | --- | --- |
| start a thread | `spawn` | creates and sets up the sandbox | 81 s first time |
| archive it, or leave it idle | `pause` | pauses it; disk **and** memory are kept | 23–29 s |
| unarchive it or send a message | `spawn` | resumes the same sandbox | 13 s |
| delete the thread | `delete` | deletes the sandbox | 2 s |

A paused sandbox costs no compute.

## Options

| Flag | Env var | Default | |
| --- | --- | --- | --- |
| `--shape` | `CREATEOS_ORB_SHAPE` | `s-4vcpu-8gb` | Size of each sandbox. Smaller pauses faster: 8 GB ≈ 30 s, 1 GB ≈ 7 s. List sizes with `createos sandbox shapes`. |
| `--template` | `CREATEOS_ORB_TEMPLATE` | `amp-orb` | Template each sandbox boots from. Built on first use. |

The CLI waits up to 5 minutes for the template before falling back to
`devbox:1`. Its overall dispatch deadline is 7 minutes 40 seconds, including
template waiting and sandbox setup. Amp's documented five-minute job limit
can still stop the dispatch earlier; increasing the CLI wait does not override
that provider limit.

## Troubleshooting

| You see | Do this |
| --- | --- |
| No **Orbs** or **Add Provider** in settings | You are in Personal Settings or have no workspace. Do step 3. |
| `AMP_ORB_PROVIDER_TOKEN is not set` | Run the `export` line in step 5 in the same shell. |
| `Session expired … Run 'amp login'` | Run `amp login` and approve the code. |
| `your session has expired … createos login` | Run `createos login`, or set `CREATEOS_API_KEY`. |
| `template amp-orb failed to build; booting devbox:1 instead` | The thread still works; its first start is slower. See `createos sandbox template logs amp-orb`, then `createos sandbox template rm amp-orb --force` to let the next thread rebuild it. |
| `No Amp project matches the Git remotes …` | The orb starts without a copy of your repository. Run `amp` from a repository Amp knows as a project. |
| `cannot look up sandbox …` | CreateOS could not be reached. The job failed on purpose; Amp retries it. |

## Stop it

Press `Ctrl+C` in the provider window. It lets running jobs finish, then exits.
The CLI allows up to 8 minutes 10 seconds for the provider to drain before
forcing it to stop; Amp may enforce a shorter job timeout.
To remove it for good, delete the provider in **Workspace Settings → Orbs**, and
check for leftover sandboxes with `createos sandbox list`.

## Limits

- **One machine per provider.** Jobs for a thread are kept in order with a lock
  on that machine. A second machine serving the same provider could race it.
- **No Desktop tab.** The sandbox has no graphical desktop.
- **Each new thread starts fresh** and runs `.agents/setup`. Pause and resume
  keep everything for that thread.
