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
mycolab ssh -s trainer    # write Host trainer -> ~/.ssh/colab_config
mycolab ssh -s eval       # second session, second Host entry
ssh trainer               # connect (after the Include step below)
mycolab lsyncd -s trainer ~/personal/try-agent /content/try-agent  # scaffold live-sync files
mycolab pull              # one-shot remote -> local over the lsyncd mapping
mycolab sync              # run lsyncd live sync in the foreground
mycolab install -s trainer ruff  # uv tool install on the runtime
mycolab usage             # remaining compute-unit credits per profile
mycolab usage work        # just one account (--json for scripts)
```

## How it works

Profiles live under `~/.config/mycolab`:

| File | Content |
| --- | --- |
| `<name>.json` | session list (colab `sessions.json` format, starts as `{}`) |
| `<name>.token.json` | oauth2 login token (empty until first login) |

`mycolab use <name>` activates a profile by symlinking colab-cli's own
`sessions.json` and `token.json` at the profile's files, so plain
`colab ...` commands operate on that workspace/account with no extra flags.
Pre-existing real files are moved aside to `*.bak` (numbered if taken).

`mycolab ssh -s <session>` writes a `Host <session>` block (ProxyCommand
over `colab ssh --proxy-mode`) into a separate `~/.ssh/colab_config`,
following the active profile. Each session gets its own hostname, so
several sessions stay usable side by side; re-running for a session
updates its entry in place and leaves the others alone. (A legacy
single-host `Host colab` entry from older mycolab versions is removed on
the next run.) Your main `~/.ssh/config` needs one line to pick it up:

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
tripping HTTP 429 against each other. After `colab new`, drop the stale
master once with `ssh -O exit <session>` (re-running `mycolab ssh` after a
profile switch closes it for you).

`mycolab ssh -s <session>` also pushes the bundled `cmd/tmux.conf` to
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

## Live sync (lsyncd)

`mycolab lsyncd -s <session> <source> <target>` scaffolds one-way live
sync (local checkout → remote path) into any project dir:

```bash
mycolab lsyncd -s trainer ~/personal/try-agent /content/try-agent
cd ~/personal/try-agent && mycolab sync  # foreground; Ctrl+C stops it
```

It writes `lsyncd.conf.lua` (one-way sync over the session's host,
ignoring `.git/`/`.venv/`, never deleting remote-only outputs) and
`LSYNCD.md` (agent/human doc: develop locally, run on the remote, session
lifecycle). Flags: `--host` (default: the `-s` session), `--force` to
overwrite. After `colab new` + `mycolab ssh -s <session>`, restart the
daemon so its startup full-sync repopulates the fresh VM.

`mycolab pull` (run from the project dir) reads back that same
`lsyncd.conf.lua` and runs the reverse direction as a one-shot rsync:
remote → local. Local-only files are left alone (no `--delete`); add
`-n/--dry-run` to preview the transfer.

## Install tools on a runtime

`mycolab install -s <session> <package> [...]` runs `uv tool install`
for each package on the runtime via `colab exec`:

```bash
mycolab install -s trainer ruff
```

Unlike the best-effort `mycolab ssh` push steps, a failure here fails the
command. uv must already exist on the runtime.

## Notes

- A new profile starts logged out; the first `colab ...` command after
  switching triggers the login flow for that account.
- Switching profiles while the old one has live sessions: colab's
  keep-alive daemons look sessions up in the active file, so they may exit
  and let those VMs idle out. `mycolab use` warns when this applies.
- Profile names: letters, digits, `-`, `_` only.
