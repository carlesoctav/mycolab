# mycolab

A collection of utilities on top of `colab-cli` to make developing, testing, and running jobs on Google Colab runtimes and servers much easier.

Key capabilities:
- **Multiple accounts/workspaces**: Seamlessly switch between Google accounts with persistent profiles.
- **Automated SSH wiring & multiplexing**: Manage per-session SSH host configs with connection sharing to prevent HTTP 429 rate limits.
- **Environment & accelerator fixes**: Automatically sync kernel env vars into sshd so `nvidia-smi` and accelerators work immediately over SSH.
- **Sync / Live sync**: Two-way sync workflows (`sync` and `pull`) between local repositories and Colab runtimes respecting `.gitignore`.
- **Job runner (`run` & `server run`)**: One-command execution that stages project directories, mounts Hugging Face buckets, manages auxiliary sidecars in tmux, and captures logs either locally or via an always-on server.
- **Runtime tooling**: Bootstrap CLI utilities (`muse`, `agy`) and sync local credentials directly into the runtime.

## Install

One-liner (automatically installs `uv`, `colab-cli`, `rsync`, and `mycolab`):

```bash
curl -fsSL https://raw.githubusercontent.com/carlesoctav/mycolab/main/install.sh | bash
```

From source:

```bash
go build -o ./bin/mycolab .
# then put ./bin/mycolab on your PATH, or:
go install .
```

## Quick Start

```bash
# Profile and session management
mycolab add work                            # create an empty profile
mycolab add personal
mycolab list                                # * marks the active profile
mycolab use                                 # interactive picker (j/k or arrows, Enter)
mycolab use work                            # switch profile directly
mycolab new -s trainer --gpu L4             # create session, configure ssh, sync env & tools
mycolab stop -s trainer                     # stop session
ssh trainer                                 # connect over ssh (after Include step)

# Syncing files
mycolab sync -s trainer . /content/proj     # live-sync local dir to Colab runtime
mycolab pull                                # one-shot pull remote files back to local

# Running jobs
mycolab run -s trainer --gpu L4 --dir .:/content/proj -- python train.py
mycolab tool -s trainer muse                # install tool (muse or agy) + credentials on runtime

# Always-on server orchestration
mycolab server connect free                 # select always-on ssh host
mycolab server new -s trainer --gpu L4      # create on server + wire locally
mycolab server run -s trainer --gpu L4 --dir .:/content/proj -- python train.py

# Usage tracking
mycolab usage                               # remaining compute-unit credits per profile
mycolab usage work                          # single account (--json for scripts)
```

## Running Jobs (`run` and `server run`)

`mycolab run` packages the entire lifecycle into a single command: creating a session (or reusing one with `--reuse`), mounting HF buckets, syncing directories, running the job inside tmux, and capturing execution logs.

```bash
mycolab run -s trainer --gpu L4 \
  --dir .:/content/proj \
  -v myuser/data:/content/data \
  --sidecar "server:uv run python server.py" \
  -- python train.py --epochs 10
```

### Flags for `run`

| Flag | Description |
| --- | --- |
| `--dir <local>:<remote>` | Directory mapping to rsync to the runtime (repeatable; the first is the working directory). Respects `.gitignore`. |
| `-v, --volume <bucket>:<remote>` | Hugging Face bucket to mount via `hf-mount` (repeatable; uses `HF_TOKEN` from `mycolab env`). |
| `-S, --sidecar "[name:]cmd"` | Auxiliary background command to run in a concurrent tmux window (repeatable; format `[name:]cmd`). |
| `--timeout <duration>` | Auto-stop the Colab session if execution exceeds duration (e.g. `2h`, `30m`). |
| `--reuse` | Use an existing session instead of launching a fresh runtime. |
| `--no-daemon` | Stream the run log directly to the current terminal instead of detaching. |
| `--persistent` | Keep the staged copy of the directories after the run finishes. |
| `--gpu`, `--tpu`, `--high-mem` | Accelerator options passed to runtime creation (e.g. `T4`, `L4`, `A100`, `v5e1`). |

Logs are saved locally to `~/mycolab/run/<session>/<id>.log`.

### Sidecars

Auxiliary services (mock APIs, environment servers, monitoring daemons) can run alongside the main process via `--sidecar` (or `-S`).

```bash
--sidecar "wordle:uv run python scripts/wordle_server.py"
```

- Prefixing with `<name>:` names the tmux window (`main` is window 1, sidecars follow as window 2, 3, etc.).
- Both the main command and sidecars start concurrently in tmux.
- Quote the sidecar argument so flags and arguments are parsed correctly.

### Always-on Server Run (`server run`)

When running long training jobs, you may not want to keep your laptop open or connected:

```bash
mycolab server run -s trainer --gpu L4 \
  --dir ./proj:/content/proj \
  -v myuser/data:/content/data \
  --timeout 8h \
  -- python train.py
```

`server run` pushes your profile credentials to the connected server, stages the directories at `~/mycolab/run/<session>/<id>/` on the server, creates the Colab session, mounts volumes, and executes the job detached on the server. You can safely close your laptop and follow logs later via:

```bash
ssh <server> tail -f ~/mycolab/run/<session>/<id>.log
```

## Sync / Live sync

Keep your local project directory and remote Colab workspace synchronized during development.

### `mycolab sync`

Live-sync a local checkout to a Colab session in real time:

```bash
# Foreground sync (Ctrl+C to stop)
mycolab sync -s trainer . /content/my-project

# Run in background as a daemon
mycolab sync -s trainer --daemon . /content/my-project
```

- If omitted, `local_dir` defaults to `.` and `remote_dir` defaults to `/content/<basename>`.
- If `-s` is omitted in an interactive terminal, an interactive session picker is shown.
- Automatically respects `.gitignore` rules (ignores `.git/`, `.venv/`, `__pycache__/`, etc.).
- No manual configuration scaffolding required.

### `mycolab pull`

Pull remote files (such as checkpoints, outputs, or logs generated on Colab) back into your local directory:

```bash
mycolab pull
```

- Performs a one-shot reverse rsync (remote &rarr; local).
- Safe by default: leaves local-only files untouched (does not delete local changes).
- Add `-n` / `--dry-run` to inspect transferred files without downloading.

## Tools on a Runtime

`mycolab tool -s <session> <tool>` installs CLI tools on the runtime and copies your local credentials and configs:

```bash
mycolab tool -s trainer muse
mycolab tool -s trainer agy
```

Supported tools:
- **`muse`**: Installs `muse` and copies `~/.config/muse/{auth,settings,trust}.json` &rarr; `/root/.config/muse/`.
- **`agy`**: Installs Antigravity CLI and copies `~/.gemini/antigravity-cli/antigravity-oauth-token` &rarr; `/root/.gemini/antigravity-cli/`.

Hugging Face CLI tools (`hf`, `hf-mount`) are installed automatically on runtime creation to power `--volume` mounts.

## Server (Always-On Management)

`mycolab server connect` selects an always-on machine (e.g. a free-tier micro VM) over SSH, allowing you to orchestrate Colab runtimes remotely:

```bash
mycolab server connect free                 # select the server: an ssh host from ~/.ssh/config
mycolab server new -s trainer --gpu L4      # create on the server + wire locally
```

The server needs `mycolab`, `colab`, and `rsync` installed on its `PATH`. Profile states and session mappings are synced seamlessly between your machine and the server.

---

## How It Works & Account Management

### Account Profiles

Profiles live under `~/.config/mycolab`:

| File | Content |
| --- | --- |
| `<name>.json` | session list (colab `sessions.json` format, starts as `{}`) |
| `<name>.token.json` | oauth2 login token (empty until first login) |
| `server` | selected always-on ssh host (set by `server connect`) |

`mycolab use <name>` activates a profile by symlinking `colab-cli`'s own `sessions.json` and `token.json` to the profile's files. Plain `colab ...` commands operate on that workspace/account without extra flags. Pre-existing files are safely moved aside to `*.bak` (numbered if taken).

### Session Creation & SSH Wiring

`mycolab new -s <session> ...` creates the session and writes a `Host <session>` block (ProxyCommand over `colab ssh --proxy-mode`) into a separate `~/.ssh/colab_config`, following the active profile.

Each session gets its own hostname, allowing multiple sessions to stay usable side by side; the entry is updated in place. Managed entries whose session exists in no profile are pruned as inactive (`--no-prune` skips this); foreign entries are always left alone. (A legacy single-host `Host colab` entry from older mycolab versions is removed on the next run.)

> **Note**: `new` with an existing name replaces the runtime.

Your main `~/.ssh/config` needs one line to pick up managed hosts:

```ssh
Include ~/.ssh/colab_config
```

The command offers to add it automatically (in global scope, before any `Host` block — an `Include` buried inside a `Host` block would not apply); otherwise add it by hand. The generated entry mirrors Colab's own SSH options (`User root`, no host-key checking). Keep your main config free of its own `Host <session>` blocks — SSH uses the first match, so a duplicate would shadow the managed entry (the command verifies the effective config and errors if that happens).

### SSH Connection Multiplexing

Colab allows a single concurrent proxy connection per runtime. Each managed entry enables multiplexing (`ControlMaster auto`, `ControlPersist 10m`): the first connection becomes the master and later connections (shells, rsync, sync daemons) share it as extra channels instead of tripping HTTP 429 rate limits against each other.

`new` drops the stale master for a replaced runtime; `ssh -O exit <session>` also works manually.

### Runtime Environment & SSH Fixes

- **Kernel environment sync (`SetEnv`)**: The runtime's sshd normally spawns shells with a near-empty environment, making accelerators invisible over plain SSH (`nvidia-smi` fails on GPU VMs, JAX sees only CPU on TPU VMs) while web console/notebooks work fine. `mycolab new` captures the kernel env through the exec door and installs it as a managed sshd `SetEnv` block (validated with `sshd -t`, old block replaced, listener reloaded; session-specific vars like `SSH_*` are excluded). `--no-env-sync` skips this.
- **IPv4 localhost pinning**: Some runtimes list localhost under `::1` first in `/etc/hosts`, causing single-attempt clients (e.g. LMCache over `tcp://localhost`) to hang indefinitely. `mycolab new` removes the token from the `::1` line (backup at `/etc/hosts.bak.mycolab`) and verifies with `getent`. `--no-hosts-fix` skips this.
- **Tmux config sync**: Pushes the bundled `tmux.conf` to `/root/.tmux.conf` on the runtime (best-effort; `--no-tmux-sync` skips it), keeping keybindings and status bar consistent.
- **Base CLI tools**: Installs `fd`, `rg`, and `jq` via apt, plus the latest stable Neovim as an AppImage in `/usr/local/bin` (`--no-tools` skips it). Re-runs are a no-op when already present.

## Notes

- A new profile starts logged out; the first `colab ...` command after switching triggers the Google OAuth login flow for that account.
- Switching profiles while the old one has live sessions: Colab's keep-alive daemons look up sessions in the active file, so they may exit and let those VMs idle out. `mycolab use` warns when this applies.
- Profile names: letters, digits, `-`, `_` only.
