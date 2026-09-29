package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull remote files back over the lsyncd mapping (remote -> local)",
	Long: `Pull remote files back to the local checkout (remote -> local), using the
mapping in ./lsyncd.conf.lua (run from the project directory).

Source, host, targetdir and excludes come from the config written by
'mycolab lsyncd'; this runs the reverse of the live-sync direction as a
one-shot rsync. Local-only files are left alone (no --delete), but files
that exist on both sides are overwritten with the remote versions.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return pullFromRemote(".", dryRun)
	},
}

func init() {
	pullCmd.Flags().BoolP("dry-run", "n", false, "print the rsync command without running it")
	rootCmd.AddCommand(pullCmd)
}

// lsyncdMapping is the sync mapping parsed out of lsyncd.conf.lua.
type lsyncdMapping struct {
	Source   string
	Host     string
	Target   string
	Excludes []string
}

// defaultPullExcludes applies when the config has no exclude block.
var defaultPullExcludes = []string{".git/", ".venv/"}

var (
	luaFieldRe  = regexp.MustCompile(`(?m)^\s*(source|host|targetdir)\s*=\s*["']([^"']+)["']`)
	luaExclRe   = regexp.MustCompile(`exclude\s*=\s*\{([^}]*)\}`)
	luaQuotedRe = regexp.MustCompile(`["']([^"']+)["']`)
)

// parseLsyncdConf reads the sync mapping from the lsyncd.conf.lua in dir.
func parseLsyncdConf(dir string) (*lsyncdMapping, error) {
	path := filepath.Join(dir, "lsyncd.conf.lua")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			where := dir
			if where == "." {
				where = "the current directory"
			}
			return nil, fmt.Errorf("no lsyncd.conf.lua in %s (scaffold one with `mycolab lsyncd -s <session> <source> <target>`)", where)
		}
		return nil, err
	}
	m := &lsyncdMapping{}
	for _, match := range luaFieldRe.FindAllStringSubmatch(string(content), -1) {
		switch match[1] {
		case "source":
			m.Source = match[2]
		case "host":
			m.Host = match[2]
		case "targetdir":
			m.Target = match[2]
		}
	}
	if m.Source == "" || m.Host == "" || m.Target == "" {
		return nil, fmt.Errorf("%s: could not find source/host/targetdir (is this an lsyncd.conf.lua written by `mycolab lsyncd`?)", path)
	}
	if !strings.HasPrefix(m.Target, "/") {
		return nil, fmt.Errorf("%s: targetdir %q is not absolute", path, m.Target)
	}
	if excl := luaExclRe.FindStringSubmatch(string(content)); excl != nil {
		for _, q := range luaQuotedRe.FindAllStringSubmatch(excl[1], -1) {
			m.Excludes = append(m.Excludes, q[1])
		}
	}
	if len(m.Excludes) == 0 {
		m.Excludes = defaultPullExcludes
	}
	return m, nil
}

// pullArgs builds the rsync invocation for remote -> local. Trailing
// slashes sync contents-to-contents; there is deliberately no --delete,
// so local-only files are never removed.
func pullArgs(m *lsyncdMapping, dryRun bool) []string {
	args := []string{"-avz"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	for _, e := range m.Excludes {
		args = append(args, "--exclude="+e)
	}
	args = append(args, "-e", "ssh",
		m.Host+":"+strings.TrimSuffix(m.Target, "/")+"/",
		strings.TrimSuffix(m.Source, "/")+"/")
	return args
}

func pullFromRemote(dir string, dryRun bool) error {
	m, err := parseLsyncdConf(dir)
	if err != nil {
		return err
	}
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		return fmt.Errorf("rsync not found in PATH")
	}
	args := pullArgs(m, dryRun)
	fmt.Printf("Pulling %s:%s/ -> %s/\n", m.Host, strings.TrimSuffix(m.Target, "/"), strings.TrimSuffix(m.Source, "/"))
	fmt.Printf("Running: rsync %s\n", quoteArgs(args))
	if dryRun {
		return nil
	}
	c := exec.Command(rsyncBin, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("rsync failed: %w", err)
	}
	fmt.Println("Pull complete.")
	return nil
}

// quoteArgs renders argv for display, quoting elements with shell
// metacharacters so the printed command stays copy-pasteable.
func quoteArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'") {
			out[i] = shellQuote(a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}
