---
name: mycolab
description: Work with Colab GPU sessions through mycolab — dev loop (new, ssh, sync, tmux) or one-shot runs.
---

# mycolab

Use this skill whenever the user wants Colab GPU work done through mycolab sessions: creating, connecting, syncing, running, or stopping sessions.

(Agents: this text is printed by `mycolab skill`. Re-run that command to reload it when starting Colab work.)

## Concepts

- A **session** (`-s <name>`) is one Colab runtime. `mycolab prepare` writes a matching `Host <name>` entry into `~/.ssh/colab_config`, so plain `ssh <name>` reaches the runtime over a multiplexed tunnel (one bridge slot shared by every shell and rsync).
- Sessions are idle-pruned after roughly 20 minutes without client activity. Keep traffic flowing or expect reclamation.
- `mycolab ssh` never auto-creates: connecting to a missing session fails with "run `mycolab new -s <name>` first" instead of silently spinning up a VM.

## Flow A — dev loop (iterating)

1. `mycolab new -s <session> [--gpu L4]` — creates the instance. `prepare` runs automatically.
2. `ssh <session>` — work on it and run programs there.
3. `mycolab sync <local_dir> <remote_dir>` — mirror the workspace to the session. **Local is the source of truth: always edit locally**, never treat the remote copy as canonical.
4. The live sync also persists the SSH connection (steady tunnel traffic is a keepalive). For anything long-lived, run it inside **tmux on the session** (e.g. under `/content/<dir>`) so it survives disconnects.

## Flow B — one-shot run (done with dev)

`mycolab run -s <session> --dir <local>:<remote> -- <cmd>` — a single detached job. The log goes to `./.mycolab/run/<session>_<id>.log`, and tunnel drops re-establish and resume automatically.

**Run-vs-dev rule:** default to the dev loop when iterating or when the user loosely asks to "run something"; use `mycolab run` when they name a single detached job or say dev is done.

## Gotchas

- Missing session on connect means run `mycolab new -s <name>` first (no auto-create).
- `mycolab sessions` lists sessions; `mycolab stop -s <name>` releases the runtime.
- Never `mycolab ping` (removed upstream). Keepalive is sync/stream activity; tmux guards the work itself.
