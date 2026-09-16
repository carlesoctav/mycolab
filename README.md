# mycolab

Multiple workspaces/accounts for the `colab` CLI, as switchable profiles.
Go + Cobra, modeled on the `ref/agys` project structure.

## Install

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

`mycolab ssh` writes a single `Host colab` block (ProxyCommand over
`colab ssh --proxy-mode`) into a separate `~/.ssh/colab_config`, following
the active profile. Your main `~/.ssh/config` needs one line to pick it up:

```
Include ~/.ssh/colab_config
```

`mycolab ssh` offers to append it when missing; otherwise add it by hand.

## Notes

- A new profile starts logged out; the first `colab ...` command after
  switching triggers the login flow for that account.
- Switching profiles while the old one has live sessions: colab's
  keep-alive daemons look sessions up in the active file, so they may exit
  and let those VMs idle out. `mycolab use` warns when this applies.
- Profile names: letters, digits, `-`, `_` only.
