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
	Long: `Write <session>.conf.lua and LSYNCD.md into a local project directory.

SOURCE is the local checkout to watch (relative or absolute); TARGET is the
absolute remote path it syncs to. Example:

    mycolab lsyncd -s trainer ~/personal/try-agent /content/try-agent

The generated <session>.conf.lua syncs one-way local -> remote over the given
SSH host (the '-s/--session' session by default, whose entry is managed by
'mycolab new -s <session>'; pass --host to override), ignoring .git/ and
.venv/. Each session gets its own config file, so several sessions can sync
the same checkout side by side; LSYNCD.md is shared and documents the setup
for humans and coding agents: develop locally, run/test on the remote.

An existing config is left alone unless --force is given.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		hostFlag, _ := cmd.Flags().GetString("host")
		sessionFlag, _ := cmd.Flags().GetString("session")
		host, err := resolveSyncHost(hostFlag, sessionFlag)
		if err != nil {
			return err
		}
		name := sessionFlag
		if name == "" {
			name = hostFlag
		}
		force, _ := cmd.Flags().GetBool("force")
		return scaffoldLsyncd(args[0], args[1], host, name, force)
	},
}

func init() {
	lsyncdCmd.Flags().String("host", "", "SSH host to sync to (defaults to the -s session)")
	lsyncdCmd.Flags().BoolP("force", "f", false, "overwrite the existing <session>.conf.lua / LSYNCD.md")
	rootCmd.AddCommand(lsyncdCmd)
}

// resolveSyncHost picks the SSH host for live sync: an explicit --host
// wins, otherwise the -s session (whose 'Host <session>' entry 'mycolab
// new -s <session>' manages).
func resolveSyncHost(host, session string) (string, error) {
	if host != "" {
		return host, nil
	}
	if session != "" {
		return session, nil
	}
	return "", fmt.Errorf("no SSH host selected (pass `--host <host>` or `-s <session>`)")
}

// scaffoldLsyncd renders the sync config (<name>.conf.lua) and the shared
// agent doc (LSYNCD.md) into sourceDir. The per-session config lets several
// sessions sync the same checkout side by side; the doc is written on the
// first scaffold and kept as-is afterwards unless --force is given.
func scaffoldLsyncd(source, target, host, name string, force bool) error {
	if host == "" {
		return fmt.Errorf("host must not be empty")
	}
	if err := validateConfName(name); err != nil {
		return err
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
	confFile := name + ".conf.lua"
	data := struct {
		Source   string
		Target   string
		Host     string
		Slug     string
		Name     string
		ConfFile string
	}{Source: abs, Target: target, Host: host, Slug: slugify(filepath.Base(abs)), Name: name, ConfFile: confFile}

	confPath := filepath.Join(abs, confFile)
	if !force {
		if _, err := os.Stat(confPath); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", confPath)
		}
	}
	if err := renderTemplateToFile(confPath, confFile, lsyncdConfTemplate, data); err != nil {
		return err
	}
	docPath := filepath.Join(abs, "LSYNCD.md")
	if !force {
		if _, err := os.Stat(docPath); err == nil {
			fmt.Printf("Kept existing %s (use --force to refresh it for %s)\n", docPath, confFile)
			fmt.Printf("Start syncing: cd %s && mycolab sync %s\n", abs, confFile)
			return nil
		}
	}
	if err := renderTemplateToFile(docPath, "LSYNCD.md", lsyncdDocTemplate, data); err != nil {
		return err
	}
	fmt.Printf("Start syncing: cd %s && mycolab sync %s\n", abs, confFile)
	return nil
}

// renderTemplateToFile renders tmpl with data and writes it to path.
func renderTemplateToFile(path, tmplName, tmpl string, data any) error {
	var sb strings.Builder
	if err := template.Must(template.New(tmplName).Parse(tmpl)).Execute(&sb, data); err != nil {
		return fmt.Errorf("render %s: %w", tmplName, err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("Wrote %s\n", path)
	return nil
}

// validateConfName rejects config names that would make a bad
// '<name>.conf.lua' file: path separators and '.'/'..' escape the project
// dir, a leading '-' parses as a flag, and whitespace/quotes/globs break
// the shell, the lua, and the ssh Host line alike.
func validateConfName(name string) error {
	if name == "" {
		return fmt.Errorf("config name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid config name %q", name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid config name %q: must not start with '-'", name)
	}
	if strings.ContainsAny(name, "/\\ \t\r\n#*?!'\"") {
		return fmt.Errorf("invalid config name %q: must not contain path separators, whitespace, or any of #*?!'\"", name)
	}
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
-- Config: {{.ConfFile}} (one per session; scaffold another with
--   'mycolab lsyncd -s <other> {{.Source}} {{.Target}}')
-- Start:  mycolab sync {{.ConfFile}}   (from {{.Source}})
-- Runs in the foreground: stop it with Ctrl+C.
-- Logs:   tail -f /tmp/lsyncd-{{.Slug}}-{{.Name}}.log   (from another terminal)
--
-- NOTE 1: Colab allows only ONE 'colab ssh' connection per runtime. This
-- is handled via SSH multiplexing (ControlMaster in ~/.ssh/colab_config,
-- managed by 'mycolab new -s <session>'): interactive shells and lsyncd's
-- rsync share one connection instead of tripping HTTP 429 against each other.
--
-- NOTE 2: after 'mycolab new -s <session>' (fresh VM, empty remote dir):
--   ssh -O exit {{.Host}}   # drop the stale multiplex master, if any
-- then restart lsyncd (Ctrl+C, run again) so its startup full-sync
-- repopulates the new VM.

settings {
    logfile    = "/tmp/lsyncd-{{.Slug}}-{{.Name}}.log",
    statusFile = "/tmp/lsyncd-{{.Slug}}-{{.Name}}.status",
    pidfile    = "/tmp/lsyncd-{{.Slug}}-{{.Name}}.pid",
    nodaemon   = true,    -- foreground: stop with Ctrl+C
    insist     = true,   -- keep retrying across transient SSH failures
}

sync {
    default.rsyncssh,
    source    = "{{.Source}}",
    host      = "{{.Host}}", -- managed by 'mycolab new -s <session>'
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
| SSH host | '{{.Host}}' (` + "`~/.ssh/colab_config`" + `, managed by ` + "`mycolab new -s <session>`" + `) |
| Config file | '{{.ConfFile}}' (next to this file) |

## Sync daemon (lsyncd)

Run from '{{.Source}}':

` + "```bash" + `
mycolab sync {{.ConfFile}}                  # start (foreground, full sync on startup; Ctrl+C stops it)
tail -f /tmp/lsyncd-{{.Slug}}-{{.Name}}.log   # logs (from another terminal)
cat /tmp/lsyncd-{{.Slug}}-{{.Name}}.status    # pending work
` + "```" + `

Ignored: '.git/', '.venv/', '__pycache__/', '*.pyc'.
'{{.ConfFile}}' and this file sync too — that is harmless.

## Multiple sessions

Each session gets its own config: 'mycolab lsyncd -s <session>
<source> <target>' writes '<session>.conf.lua' next to this file, so one
checkout can sync to several runtimes. Run one daemon per config
('mycolab sync <session>.conf.lua'); the paths above describe
'{{.ConfFile}}', but the commands work the same for any config.

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
mycolab pull {{.ConfFile}}   # from '{{.Source}}': reads {{.ConfFile}}, syncs remote -> local
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
mycolab new -s <session>     # Host block + remote setup (tmux, env, hosts, tools)
mycolab usage                # remaining compute-unit credits
colab status | colab sessions
colab stop -s <session>      # release the VM when done
` + "```" + `

After 'mycolab new -s <session>' the remote dir is empty
and the old multiplex master is stale:

` + "```bash" + `
ssh -O exit {{.Host}}   # drop the stale master, if any
# then in the lsyncd terminal: Ctrl+C, run 'mycolab sync {{.ConfFile}}' again for a full re-sync
` + "```" + `

## Warnings for agents

- Do not run 'colab new' / 'colab stop' on your own: they spend real
  compute units / destroy the VM. Ask the human first.
- Do not remove ControlMaster from the managed ssh config: without it,
  sync and shells fight over Colab's single connection (HTTP 429).
- The remote can vanish without warning (preemption, idle timeout).
  Irreplaceable outputs must be pulled back, not left in /content.
`
