# mycolab

Multiple workspaces/accounts for the `colab` CLI, as switchable profiles.
Go + Cobra, modeled on the `ref/agys` project structure.

## Install

One-liner (needs `gh` logged in, or `GITHUB_TOKEN` set — the repo is private):

```bash
curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
  https://raw.githubusercontent.com/carlesoctav/mycolab/main/install.sh | bash
```

Pin a version or change the install dir with env vars:

```bash
curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
  https://raw.githubusercontent.com/carlesoctav/mycolab/main/install.sh | MYCOLAB_VERSION=v0.1.0 INSTALL_DIR=~/.local/bin bash
```

From source:

```bash
go build -o ./bin/mycolab .
# then put ./bin/mycolab on your PATH, or:
go install .
```

## Usage

```bash
mycolab add work          # create an empty profile
mycolab add personal
mycolab list              # * marks the active profile
mycolab use               # interactive picker (j/k or arrows, Enter)
mycolab use work          # switch directly
mycolab new -s trainer --gpu L4  # create a session, then wire it for ssh
mycolab new -s eval       # second session, second Host entry
mycolab stop -s trainer   # stop a session
ssh trainer               # connect (after the Include step below)
mycolab lsyncd -s trainer ~/personal/try-agent /content/try-agent  # scaffold live-sync files
mycolab pull trainer.conf.lua  # one-shot remote -> local over the lsyncd mapping
mycolab sync trainer.conf.lua  # run lsyncd live sync in the foreground
mycolab tool -s trainer muse   # install tool + credentials on the runtime
mycolab server connect free    # select the always-on server (ssh host)
mycolab server new -s trainer --gpu L4  # create on the server + wire locally
mycolab usage             # remaining compute-unit credits per profile
mycolab usage work        # just one account (--json for scripts)
```

## How it works

Profiles live under `~/.config/mycolab`:

| File | Content |
| --- | --- |
| `<name>.json` | session list (colab `sessions.json` format, starts as `{}`) |
| `<name>.token.json` | oauth2 login token (empty until first login) |
| `server` | selected always-on ssh host (set by `server connect`) |

`mycolab use <name>` activates a profile by symlinking colab-cli's own
`sessions.json` and `token.json` at the profile's files, so plain
`colab ...` commands operate on that workspace/account with no extra flags.
Pre-existing real files are moved aside to `*.bak` (numbered if taken).

`mycolab new -s <session> ...` creates the session and writes a
`Host <session>` block (ProxyCommand over `colab ssh --proxy-mode`)
into a separate `~/.ssh/colab_config`, following the active profile.
Each session gets its own hostname, so several sessions stay usable
side by side; the entry is updated in place. Managed entries whose
session exists in no profile are pruned as inactive (`--no-prune` skips
this); foreign entries are always left alone. (A legacy single-host
`Host colab` entry from older mycolab versions is removed on the next
run.) Note: `new` with an existing name replaces the runtime. Your main
`~/.ssh/config` needs one line to pick it up:

```
Include ~/.ssh/colab_config
```

The command offers to add it (in global scope, before any Host block — an
Include buried inside a Host block would not apply); otherwise add it by hand.
The generated entry mirrors colab's own ssh options (`User root`, no
host-key checking). Keep your main config free of its own `Host <session>`
blocks — ssh uses the first match, so a duplicate would shadow the managed
entry (the command verifies the effective config and errors if that happens).

Colab allows a single concurrent proxy connection per runtime, so each
managed entry enables multiplexing (`ControlMaster auto`,
`ControlPersist 10m`): the first connection becomes the master and later
ones (shells, rsync, lsyncd) share it as extra channels instead of
tripping HTTP 429 against each other. `new` drops the stale master for
a replaced runtime; `ssh -O exit <session>` also works by hand.

`new` also pushes the bundled `cmd/tmux.conf` to
`/root/.tmux.conf` on the runtime (best-effort; `--no-tmux-sync` skips
it), so tmux on the server matches your local setup. Edit the bundled
copy and reinstall to change it.

It also syncs the runtime's kernel env into sshd (`--no-env-sync` skips
it). The runtime's sshd spawns shells with a near-empty env, so over
plain ssh, accelerators are invisible (`nvidia-smi` fails on GPU VMs, jax
sees only CPU on TPU VMs) while `colab console`/`colab exec` work fine.
The sync captures the kernel env through the exec door and installs it as
a managed sshd `SetEnv` block (validated with `sshd -t`, old block
replaced, listener reloaded; session-specific vars like `SSH_*` are
excluded). Reconnect `ssh <session>` afterwards to pick it up.

It also pins localhost to IPv4 in the runtime's /etc/hosts
(`--no-hosts-fix` skips it). Some runtimes list localhost under ::1 first,
and single-attempt clients (e.g. LMCache over tcp://localhost) then hang
against nothing; the fix drops the token from the ::1 line (backup at
/etc/hosts.bak.mycolab) and verifies with getent.

It also installs base CLI tools on the runtime (`--no-tools` skips it):
`fd`, `rg` and `jq` via apt, plus the latest stable Neovim as an
AppImage in `/usr/local/bin` (same approach as `ubuntu-tasks/nvim.sh`,
tracking the stable release). Re-runs are a no-op when everything is
already present.

## Live sync (lsyncd)

`mycolab lsyncd -s <session> <source> <target>` scaffolds one-way live
sync (local checkout → remote path) into any project dir:

```bash
mycolab lsyncd -s trainer ~/personal/try-agent /content/try-agent  # writes trainer.conf.lua
mycolab lsyncd -s eval ~/personal/try-agent /content/try-agent     # writes eval.conf.lua
cd ~/personal/try-agent && mycolab sync trainer.conf.lua  # foreground; Ctrl+C stops it
```

It writes `<session>.conf.lua` (one-way sync over the session's host,
ignoring `.git/`/`.venv/`, never deleting remote-only outputs) and the
shared `LSYNCD.md` (agent/human doc: develop locally, run on the remote,
session lifecycle). One checkout can hold several session configs side by
side; each daemon gets its own log/pid files under `/tmp`. Flags: `--host`
(default: the `-s` session), `--force` to overwrite. After `mycolab new
-s <session>`, restart the daemon so its startup full-sync repopulates
the fresh VM.

`mycolab pull <conf>` / `mycolab sync <conf>` (run from the project dir)
take the config file (`trainer.conf.lua`) or the bare session name
(`trainer`). With no argument, `./lsyncd.conf.lua` wins when present
(written by older mycolab), else the single `*.conf.lua` in the directory;
when several exist, the argument is required. `pull` reads back the
config's mapping and runs the reverse direction as a one-shot rsync:
remote → local. Local-only files are left alone (no `--delete`); add
`-n/--dry-run` to preview the transfer.

## Tools on a runtime

`mycolab tool -s <session> <tool>` installs a CLI tool on the runtime
and copies its local config/credential files over when available:

```bash
mycolab tool -s trainer muse
```

Supported tools: `muse` (upstream installer, then
`~/.config/muse/{auth,settings,trust}.json` → `/root/.config/muse/`),
`agy` (Antigravity CLI: upstream installer, then
`~/.gemini/antigravity-cli/antigravity-oauth-token` → `/root/.gemini/antigravity-cli/`).
Each tool is one spec file under `pkg/tools/`; failures fail the command.

## Server (always-on)

`mycolab server connect` selects an always-on machine (e.g. a free-tier
micro VM) over SSH, and `mycolab server new` creates the session there
instead of here. Profiles are tracked locally only: the command pushes
the active profile's token to a same-named mirror profile on the
server, runs `colab new` plus the runtime prep there, then pulls merged
sessions back and writes the direct local entry. Switching accounts is
just `mycolab use <profile>` — the next server command pushes that
account. Everything after creation (`stop`, `tool`, `usage`, plain
`ssh`) runs locally on the synced state:

```bash
mycolab server connect free    # select the server: an ssh host from ~/.ssh/config
mycolab server new -s trainer --gpu L4  # create on the server + wire locally
```

The server needs the `mycolab` and `colab` binaries installed
(`~/.local/bin` and `~/go/bin` are added to PATH automatically). The
connected host is stored in `~/.config/mycolab/server`; `--server
<ssh-host>` overrides it for one invocation.

Sync rules: sessions are never pushed — the server file is written by
server-side runs and merged back by name (server wins; local-only
sessions are preserved). The machine-local `keep_alive_pid` is stripped
from incoming entries. When a merged entry points at a different runtime
than the local one, the previous local file is backed up to
`<profile>.json.mycolab-sync.bak`.

## Notes

- A new profile starts logged out; the first `colab ...` command after
  switching triggers the login flow for that account.
- Switching profiles while the old one has live sessions: colab's
  keep-alive daemons look sessions up in the active file, so they may exit
  and let those VMs idle out. `mycolab use` warns when this applies.
- Profile names: letters, digits, `-`, `_` only.
