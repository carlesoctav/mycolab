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
mycolab ssh               # pick a session -> writes Host colab
mycolab ssh trainer       # same, without the picker
ssh colab                 # connect (after the Include step below)
mycolab lsyncd ~/personal/try-agent /content/try-agent  # scaffold live-sync files
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

`mycolab ssh` writes a `Host colab` block (ProxyCommand over
`colab ssh --proxy-mode`) into a separate `~/.ssh/colab_config`, following
the active profile. Your main `~/.ssh/config` needs one line to pick it up:

```
Include ~/.ssh/colab_config
```

`mycolab ssh` offers to add it (in global scope, before any Host block — an
Include buried inside a Host block would not apply); otherwise add it by hand.
The generated entry mirrors colab's own ssh options (`User root`, no
host-key checking). Keep your main config free of its own `Host colab`
block — ssh uses the first match, so a duplicate would shadow the managed
entry (the command verifies the effective config and errors if that happens).

Colab allows a single concurrent proxy connection per runtime, so the
managed entry enables multiplexing (`ControlMaster auto`,
`ControlPersist 10m`): the first connection becomes the master and later
ones (shells, rsync, lsyncd) share it as extra channels instead of
tripping HTTP 429 against each other. After `colab new`, drop the stale
master once with `ssh -O exit colab`.

## Live sync (lsyncd)

`mycolab lsyncd <source> <target>` scaffolds one-way live sync
(local checkout → remote path) into any project dir:

```bash
mycolab lsyncd ~/personal/try-agent /content/try-agent
cd ~/personal/try-agent && lsyncd lsyncd.conf.lua
```

It writes `lsyncd.conf.lua` (one-way sync over the `colab` host, ignoring
`.git/`/`.venv/`, never deleting remote-only outputs) and `LSYNCD.md`
(agent/human doc: develop locally, run on the remote, session lifecycle).
Flags: `--host` (default `colab`), `--force` to overwrite. After
`colab new` + `mycolab ssh`, restart the daemon so its startup full-sync
repopulates the fresh VM.

## Notes

- A new profile starts logged out; the first `colab ...` command after
  switching triggers the login flow for that account.
- Switching profiles while the old one has live sessions: colab's
  keep-alive daemons look sessions up in the active file, so they may exit
  and let those VMs idle out. `mycolab use` warns when this applies.
- Profile names: letters, digits, `-`, `_` only.
