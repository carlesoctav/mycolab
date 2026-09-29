package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

var lsyncdCmd = &cobra.Command{
	Use:   "lsyncd [source] [target]",
	Short: "Scaffold lsyncd live-sync files for a local dir -> Colab path",
	Long: `Write lsyncd.conf.lua and LSYNCD.md into a local project directory.

SOURCE is the local checkout to watch (relative or absolute); TARGET is the
absolute remote path it syncs to. Example:

    mycolab lsyncd ~/personal/try-agent /content/try-agent

The generated lsyncd.conf.lua syncs one-way local -> remote over the given
SSH host (the '-s/--session' session by default, whose entry is managed by
'mycolab ssh -s <session>'; pass --host to override), ignoring .git/ and
.venv/. LSYNCD.md documents the setup for humans and coding agents:
develop locally, run/test on the remote.

Existing files are left alone unless --force is given.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		hostFlag, _ := cmd.Flags().GetString("host")
		sessionFlag, _ := cmd.Flags().GetString("session")
		host, err := resolveSyncHost(hostFlag, sessionFlag)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		return scaffoldLsyncd(args[0], args[1], host, force)
	},
}

func init() {
	lsyncdCmd.Flags().String("host", "", "SSH host to sync to (defaults to the -s session)")
	lsyncdCmd.Flags().BoolP("force", "f", false, "overwrite existing lsyncd.conf.lua / LSYNCD.md")
	rootCmd.AddCommand(lsyncdCmd)
}

// resolveSyncHost picks the SSH host for live sync: an explicit --host
// wins, otherwise the -s session (whose 'Host <session>' entry 'mycolab
// ssh -s <session>' manages).
func resolveSyncHost(host, session string) (string, error) {
	if host != "" {
		return host, nil
	}
	if session != "" {
		return session, nil
	}
	return "", fmt.Errorf("no SSH host selected (pass `--host <host>` or `-s <session>`)")
}

// scaffoldLsyncd renders the sync config and agent doc into sourceDir.
func scaffoldLsyncd(source, target, host string, force bool) error {
	if host == "" {
		return fmt.Errorf("host must not be empty")
	}
	if !strings.HasPrefix(target, "/") {
		return fmt.Errorf("target must be an absolute remote path, got %q", target)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("source %q: %w", source, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("source %q is not a directory", source)
	}
	data := struct {
		Source string
		Target string
		Host   string
		Slug   string
	}{Source: abs, Target: target, Host: host, Slug: slugify(filepath.Base(abs))}

	files := map[string]string{
		"lsyncd.conf.lua": lsyncdConfTemplate,
		"LSYNCD.md":       lsyncdDocTemplate,
	}
	for name, tmpl := range files {
		path := filepath.Join(abs, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists (use --force to overwrite)", path)
			}
		}
		var sb strings.Builder
		if err := template.Must(template.New(name).Parse(tmpl)).Execute(&sb, data); err != nil {
			return fmt.Errorf("render %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("Wrote %s\n", path)
	}
	fmt.Printf("Start syncing: cd %s && lsyncd lsyncd.conf.lua\n", abs)
	return nil
}

// slugify turns a directory basename into a safe /tmp filename fragment.
func slugify(base string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	slug := strings.Trim(sb.String(), "-.")
	if slug == "" {
		return "sync"
	}
	return slug
}

const lsyncdConfTemplate = `-- lsyncd config: live-sync this dir -> {{.Host}}:{{.Target}}.
-- One-way local -> remote. Local checkout is the source of truth.
--
-- Start:  mycolab sync   (from {{.Source}}; runs lsyncd lsyncd.conf.lua)
-- Runs in the foreground: stop it with Ctrl+C.
-- Logs:   tail -f /tmp/lsyncd-{{.Slug}}.log   (from another terminal)
--
-- NOTE 1: Colab allows only ONE 'colab ssh' connection per runtime. This
-- is handled via SSH multiplexing (ControlMaster in ~/.ssh/colab_config,
-- managed by 'mycolab ssh -s <session>'): interactive shells and lsyncd's
-- rsync share one connection instead of tripping HTTP 429 against each other.
--
-- NOTE 2: after 'colab new' + 'mycolab ssh -s <session>' (fresh VM, empty remote dir):
--   ssh -O exit {{.Host}}   # drop the stale multiplex master, if any
-- then restart lsyncd (Ctrl+C, run again) so its startup full-sync
-- repopulates the new VM.

settings {
    logfile    = "/tmp/lsyncd-{{.Slug}}.log",
    statusFile = "/tmp/lsyncd-{{.Slug}}.status",
    pidfile    = "/tmp/lsyncd-{{.Slug}}.pid",
    nodaemon   = true,    -- foreground: stop with Ctrl+C
    insist     = true,   -- keep retrying across transient SSH failures
}

sync {
    default.rsyncssh,
    source    = "{{.Source}}",
    host      = "{{.Host}}", -- managed by 'mycolab ssh -s <session>'
    targetdir = "{{.Target}}",
    delay     = 1,

    exclude = {
        ".git/",
        ".venv/",
        "__pycache__/",
        "*.pyc",
    },

    rsync = {
        archive  = true,
        compress = true,
        -- NOTE: no 'delete = true' on purpose: files produced remotely
        -- (checkpoints, logs, results) must survive local syncs.
    },
}
`

const lsyncdDocTemplate = `# LSYNCD — develop local, run remote

> If you are an AI coding agent working in this repo, read this first.
> If you are a human, the short version: edit files here; they land on
> Colab ~1 second after every save; run them there.

## Golden rule

- **This checkout is the source of truth.** All development happens here.
- **The remote is for running/testing only.** Sync is one-way local → remote.
- **Never edit code on the remote** — the next sync of that file overwrites it.
- Files created only on the remote (checkpoints, outputs) are safe from
  sync (it never deletes), but a dead session wipes them: pull results
  back promptly (see below).

## Paths

| | |
| --- | --- |
| Local source | '{{.Source}}' |
| Remote target | '{{.Host}}:{{.Target}}' |
| SSH host | '{{.Host}}' (` + "`~/.ssh/colab_config`" + `, managed by ` + "`mycolab ssh -s <session>`" + `) |

## Sync daemon (lsyncd)

Run from '{{.Source}}':

` + "```bash" + `
mycolab sync                        # start (foreground, full sync on startup; Ctrl+C stops it)
tail -f /tmp/lsyncd-{{.Slug}}.log     # logs (from another terminal)
cat /tmp/lsyncd-{{.Slug}}.status      # pending work
` + "```" + `

Ignored: '.git/', '.venv/', '__pycache__/', '*.pyc'.
'lsyncd.conf.lua' and this file sync too — that is harmless.

## Running / testing on the remote

Colab allows a single concurrent SSH connection; it is shared via
multiplexing (ControlMaster in the managed ssh config), so shells and
syncs coexist. Prefer short-lived commands over a held-open shell:

` + "```bash" + `
echo '!<shell command>' | colab exec   # run anything, e.g. '!python3 train.py --epochs 2'
ssh {{.Host}}                             # interactive shell when really needed
colab ls {{.Target}}                   # list remote files
colab upload <local> <remote>          # one-off push outside the sync
colab download <remote> <local>        # one-off fetch
colab install <pkg>                    # pip install on the runtime
` + "```" + `

GPU sanity check:

` + "```bash" + `
echo '!python3 -c "import torch; print(torch.cuda.is_available())"' | colab exec
` + "```" + `

## Long-running programs (tmux)

If the code must keep running after you disconnect (training, servers),
run it under tmux on the remote: the tmux server survives dropped
connections, and the human can attach later with ` + "`ssh {{.Host}}`" + `
then ` + "`tmux a -t <name>`" + `.

` + "```bash" + `
echo '!tmux new -d -s train "python3 train.py 2>&1 | tee train.log"' | colab exec -s <session>
echo '!tmux ls' | colab exec -s <session>                                     # list sessions
echo '!tmux capture-pane -p -t train | tail -20' | colab exec -s <session>   # peek at output
echo '!tmux kill-session -t train' | colab exec -s <session>                 # stop it
` + "```" + `

Rules for agents:

- Start tmux via ` + "`colab exec`" + ` so the program inherits the full
  kernel env (GPU/TPU vars). Always pass the same ` + "`-s <session>`" + `
  on every command once more than one session exists.
- Log to a file (` + "`tee`" + `) as well as the pane; poll with
  ` + "`capture-pane`" + `, never block waiting on output.
- tmux dies with the VM: checkpoint often and pull outputs back (below).
- One short session name per job (` + "`train`" + `, ` + "`eval-x`" + `).

## Fetching results back (one-shot pull)

` + "```bash" + `
mycolab pull   # from '{{.Source}}': reads lsyncd.conf.lua, syncs remote -> local
` + "```" + `

This overwrites local files with remote versions when you need
checkpoints/outputs back (local-only files are left alone). It runs the
equivalent of:

` + "```bash" + `
rsync -avz --exclude='.git/' --exclude='.venv/' -e ssh {{.Host}}:{{.Target}}/ {{.Source}}/
` + "```" + `

## Session lifecycle (mycolab / colab)

` + "```bash" + `
mycolab list                 # profiles, * = active
mycolab use <name>           # switch account/workspace
colab new --gpu l4           # fresh VM (run project setup after, if any)
mycolab ssh -s <session>     # Host block + remote setup (tmux, env, hosts)
mycolab usage                # remaining compute-unit credits
colab status | colab sessions
colab stop -s <session>      # release the VM when done
` + "```" + `

After 'colab new' + 'mycolab ssh -s <session>' the remote dir is empty
and the old multiplex master is stale:

` + "```bash" + `
ssh -O exit {{.Host}}   # drop the stale master, if any
# then in the lsyncd terminal: Ctrl+C, run 'mycolab sync' again for a full re-sync
` + "```" + `

## Warnings for agents

- Do not run 'colab new' / 'colab stop' on your own: they spend real
  compute units / destroy the VM. Ask the human first.
- Do not remove ControlMaster from the managed ssh config: without it,
  sync and shells fight over Colab's single connection (HTTP 429).
- The remote can vanish without warning (preemption, idle timeout).
  Irreplaceable outputs must be pulled back, not left in /content.
`
