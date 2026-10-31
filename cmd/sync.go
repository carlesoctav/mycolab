package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var syncCmd = &cobra.Command{
	Use:   "sync [local_dir] [remote_dir]",
	Short: "Live-sync a local directory to a Colab runtime using lsyncd",
	Long: `Live-sync a local directory to a Colab session in the background or foreground.

No project configuration file is created. Instead, a temporary configuration
is generated in /tmp/<session>.conf.lua, respecting ignore rules in .gitignore
if present.

Example:
    mycolab sync -s trainer . /content/my-project
    mycolab sync -s trainer --daemon . /content/my-project

If directories are omitted, local defaults to the current directory ('.')
and remote defaults to /content/<basename>.
If -s is omitted, an interactive picker lets you choose an active session.`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. If single arg is an existing config file (legacy mode), run it directly
		if len(args) == 1 {
			if confPath, err := resolveLsyncdConfig(".", args[0]); err == nil {
				return syncLive(".", confPath)
			}
		}

		// 2. Resolve session name
		sessionName, _ := cmd.Flags().GetString("session")
		if sessionName == "" {
			var err error
			sessionName, err = pickSession()
			if err != nil {
				return err
			}
		}
		if err := validateSessionHost(sessionName); err != nil {
			return err
		}

		// 3. Resolve local and remote dirs
		localDir := "."
		if len(args) >= 1 {
			localDir = args[0]
		}
		absLocal, err := filepath.Abs(localDir)
		if err != nil {
			return fmt.Errorf("resolve local dir: %w", err)
		}
		fi, err := os.Stat(absLocal)
		if err != nil || !fi.IsDir() {
			return fmt.Errorf("local dir %q is not a directory", absLocal)
		}

		remoteDir := ""
		if len(args) == 2 {
			remoteDir = args[1]
		} else {
			remoteDir = "/content/" + filepath.Base(absLocal)
		}
		if !strings.HasPrefix(remoteDir, "/") {
			return fmt.Errorf("remote dir %q must be an absolute path (e.g. /content/...)", remoteDir)
		}

		daemon, _ := cmd.Flags().GetBool("daemon")

		// 4. Output instructions for AI coding agents to stdout
		printAgentInstructions(absLocal, sessionName, remoteDir)

		// 5. Generate /tmp/<session>.conf.lua and start lsyncd
		return runSessionSync(absLocal, sessionName, remoteDir, daemon)
	},
}

func init() {
	syncCmd.Flags().Bool("daemon", false, "run lsyncd in the background as a daemon")
	rootCmd.AddCommand(syncCmd)
}

// pickSession prompts user to select a session from the active profile.
func pickSession() (string, error) {
	current, err := profile.GetCurrent()
	if err != nil {
		return "", err
	}
	if current == "" {
		return "", fmt.Errorf("no active profile (use `mycolab use <profile>` first)")
	}
	sessions, err := profile.Sessions(current)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions found in profile %q (create one with `mycolab new -s <name>`)", current)
	}
	if len(sessions) == 1 {
		return sessions[0].Name, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("multiple sessions exist in profile %q; specify one with -s <session>", current)
	}

	lines := make([]string, len(sessions))
	for i, s := range sessions {
		lines[i] = fmt.Sprintf("%-16s %s", s.Name, formatSession(s))
	}
	idx, err := Select("Select session to sync with:", lines)
	if err != nil {
		return "", err
	}
	return sessions[idx].Name, nil
}

// loadSyncExcludes reads exclude patterns from .gitignore in localDir.
func loadSyncExcludes(localDir string) []string {
	defaults := []string{".git/", ".venv/", "__pycache__/", "*.pyc"}
	gitignore := filepath.Join(localDir, ".gitignore")

	target := ""
	if st, err := os.Stat(gitignore); err == nil && !st.IsDir() {
		target = gitignore
	}

	if target == "" {
		return defaults
	}

	file, err := os.Open(target)
	if err != nil {
		return defaults
	}
	defer file.Close()

	var excludes []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		excludes = append(excludes, line)
		seen[line] = true
	}
	for _, d := range defaults {
		if !seen[d] {
			excludes = append(excludes, d)
		}
	}
	return excludes
}

// isSyncExcluded checks if a file/dir name matches any exclude pattern.
func isSyncExcluded(name string, excludes []string) bool {
	cleanName := strings.Trim(name, "/")
	for _, e := range excludes {
		cleanE := strings.Trim(e, "/")
		if cleanName == cleanE {
			return true
		}
		if matched, _ := filepath.Match(cleanE, cleanName); matched {
			return true
		}
	}
	return false
}

// renderLsyncdConfig creates the Lua configuration string for lsyncd.
func renderLsyncdConfig(sourceDir, sessionName, targetDir string, daemon bool) string {
	excludes := loadSyncExcludes(sourceDir)
	var exclLines []string
	for _, e := range excludes {
		exclLines = append(exclLines, fmt.Sprintf("        %q,", e))
	}

	slug := slugify(filepath.Base(sourceDir))
	nodaemonStr := "true"
	if daemon {
		nodaemonStr = "false"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`settings {
    logfile    = "/tmp/lsyncd-%s-%s.log",
    statusFile = "/tmp/lsyncd-%s-%s.status",
    pidfile    = "/tmp/lsyncd-%s-%s.pid",
    nodaemon   = %s,
    insist     = true,
}

sync {
    default.rsyncssh,
    source    = %q,
    host      = %q,
    targetdir = %q,
    delay     = 1,
    delete    = false,

    exclude = {
%s
    },

    rsync = {
        archive  = true,
        compress = true,
        _extra   = { "--copy-links" },
    },
}
`, slug, sessionName, slug, sessionName, slug, sessionName, nodaemonStr, sourceDir, sessionName, targetDir, strings.Join(exclLines, "\n")))

	// Inotify does not follow directory symlinks, so edits inside symlinked
	// folders would be missed by lsyncd. Emit an extra sync block for each
	// top-level symlinked directory so inotify watches the real targets directly.
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		absSource = sourceDir
	}
	entries, err := os.ReadDir(absSource)
	if err == nil {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		seenTargets := map[string]bool{absSource: true}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			if isSyncExcluded(entry.Name(), excludes) {
				continue
			}
			linkPath := filepath.Join(absSource, entry.Name())
			resolved, err := filepath.EvalSymlinks(linkPath)
			if err != nil {
				continue
			}
			absResolved, err := filepath.Abs(resolved)
			if err != nil {
				continue
			}
			fi, err := os.Stat(absResolved)
			if err != nil || !fi.IsDir() {
				continue
			}
			if rel, err := filepath.Rel(absSource, absResolved); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
				continue
			}
			if seenTargets[absResolved] {
				continue
			}
			seenTargets[absResolved] = true

			subExcludes := loadSyncExcludes(absResolved)
			var subExclLines []string
			for _, se := range subExcludes {
				subExclLines = append(subExclLines, fmt.Sprintf("        %q,", se))
			}
			remoteSubDir := strings.TrimRight(targetDir, "/") + "/" + entry.Name()

			sb.WriteString(fmt.Sprintf(`
sync {
    default.rsyncssh,
    source    = %q,
    host      = %q,
    targetdir = %q,
    delay     = 1,
    delete    = false,

    exclude = {
%s
    },

    rsync = {
        archive  = true,
        compress = true,
        _extra   = { "--copy-links" },
    },
}
`, absResolved, sessionName, remoteSubDir, strings.Join(subExclLines, "\n")))
		}
	}

	return sb.String()
}

// runSessionSync writes /tmp/<session>.conf.lua and starts lsyncd.
func runSessionSync(localDir, sessionName, remoteDir string, daemon bool) error {
	bin, err := findLsyncd()
	if err != nil {
		return fmt.Errorf("lsyncd not found in PATH (install lsyncd to run live sync)")
	}

	confContent := renderLsyncdConfig(localDir, sessionName, remoteDir, daemon)
	confPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s.conf.lua", sessionName))
	if err := os.WriteFile(confPath, []byte(confContent), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", confPath, err)
	}

	slug := slugify(filepath.Base(localDir))
	logFile := fmt.Sprintf("/tmp/lsyncd-%s-%s.log", slug, sessionName)
	pidFile := fmt.Sprintf("/tmp/lsyncd-%s-%s.pid", slug, sessionName)

	if daemon {
		fmt.Printf("Starting lsyncd daemon in background...\n")
		fmt.Printf("  Local:   %s\n", localDir)
		fmt.Printf("  Remote:  %s:%s\n", sessionName, remoteDir)
		fmt.Printf("  Config:  %s\n", confPath)
		fmt.Printf("  Log:     tail -f %s\n", logFile)

		cmd := exec.Command(bin, confPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start lsyncd daemon: %w", err)
		}
		_ = cmd.Process.Release()
		fmt.Println("Lsyncd daemon started.")
		return nil
	}

	fmt.Printf("Starting live sync in foreground (%s -> %s:%s)...\n", localDir, sessionName, remoteDir)
	fmt.Printf("Logs: tail -f %s | Stop: Ctrl+C\n", logFile)
	c := exec.Command(bin, confPath)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && !exitErr.Exited() {
			fmt.Println("\nSync stopped.")
			_ = os.Remove(pidFile)
			return nil
		}
		return fmt.Errorf("lsyncd failed: %w", err)
	}
	return nil
}

// printAgentInstructions prints instructions to stdout explaining the
// local edit / remote test workflow for AI coding agents and developers.
func printAgentInstructions(localDir, sessionName, remoteDir string) {
	fmt.Printf(`
================================================================================
LSYNCD LIVE SYNC ACTIVE — Develop Local, Run & Test on Colab
================================================================================
> Source of truth: Edit and save code LOCALLY in %s
> Continuous sync:  Changes sync to %s:%s via lsyncd (~1s)
> Remote execution: Run, test, and debug on Colab via SSH:
    ssh %s 'cd %s && python ...'
================================================================================

`, localDir, sessionName, remoteDir, sessionName, remoteDir)
}

// findLsyncd locates the lsyncd binary. A variable so tests can stub it.
var findLsyncd = func() (string, error) {
	return exec.LookPath("lsyncd")
}

// resolveLsyncdConfig resolves legacy lsyncd config files (kept for backward compatibility).
func resolveLsyncdConfig(dir, confArg string) (string, error) {
	isFile := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && !fi.IsDir()
	}
	where := dir
	if where == "." {
		where = "the current directory"
	}
	if confArg != "" {
		p := confArg
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if isFile(p) {
			return p, nil
		}
		if !strings.HasSuffix(confArg, ".lua") {
			if alt := filepath.Join(dir, confArg+".conf.lua"); isFile(alt) {
				return alt, nil
			}
		}
		return "", fmt.Errorf("no lsyncd config %q in %s", confArg, where)
	}
	if legacy := filepath.Join(dir, "lsyncd.conf.lua"); isFile(legacy) {
		return legacy, nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.conf.lua"))
	var files []string
	for _, m := range matches {
		if isFile(m) {
			files = append(files, m)
		}
	}
	switch len(files) {
	case 0:
		return "", fmt.Errorf("no lsyncd config (*.conf.lua) in %s", where)
	case 1:
		return files[0], nil
	default:
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = filepath.Base(f)
		}
		sort.Strings(names)
		return "", fmt.Errorf("multiple lsyncd configs in %s (%s); pass one explicitly", where, strings.Join(names, ", "))
	}
}

// syncLive runs lsyncd on the config selected by confArg in dir, attached
// to the terminal.
func syncLive(dir, confArg string) error {
	confPath, err := resolveLsyncdConfig(dir, confArg)
	if err != nil {
		return err
	}
	bin, err := findLsyncd()
	if err != nil {
		return fmt.Errorf("lsyncd not found in PATH (install lsyncd to run live sync)")
	}
	abs, err := filepath.Abs(confPath)
	if err != nil {
		return fmt.Errorf("resolve config: %w", err)
	}
	fmt.Printf("Starting live sync (%s).\n", confPath)
	c := exec.Command(bin, filepath.Base(abs))
	c.Dir = filepath.Dir(abs)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && !exitErr.Exited() {
			fmt.Println("Stopped.")
			return nil
		}
		return fmt.Errorf("lsyncd failed: %w", err)
	}
	return nil
}
