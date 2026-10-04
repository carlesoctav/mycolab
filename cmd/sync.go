package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [conf]",
	Short: "Run lsyncd live sync in the foreground (local -> remote)",
	Long: `Run lsyncd live sync in the foreground using a <session>.conf.lua config
(run from the project directory).

CONF selects the config written by 'mycolab lsyncd -s <session> <source>
<target>': pass the file ('trainer.conf.lua') or the bare session/config
name ('trainer'). With no CONF, './lsyncd.conf.lua' wins when present
(written by older mycolab), else the single '*.conf.lua' in the directory;
when several exist, CONF is required.

Equivalent to 'lsyncd <conf>'; stop it with Ctrl+C.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		confArg := ""
		if len(args) == 1 {
			confArg = args[0]
		}
		return syncLive(".", confArg)
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}

// findLsyncd locates the lsyncd binary. A variable so tests can stub it.
var findLsyncd = func() (string, error) {
	return exec.LookPath("lsyncd")
}

// resolveLsyncdConfig resolves the lsyncd config file to use. An explicit
// confArg selects it: a path to a file, or a session/config name resolved
// to '<name>.conf.lua' in dir. With no confArg, ./lsyncd.conf.lua wins when
// present (written by older mycolab), else the single *.conf.lua in dir;
// zero or several configs without an explicit choice is an error.
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
		return "", fmt.Errorf("no lsyncd config %q in %s (scaffold one with `mycolab lsyncd -s <session> <source> <target>`)", confArg, where)
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
		return "", fmt.Errorf("no lsyncd config (*.conf.lua) in %s (scaffold one with `mycolab lsyncd -s <session> <source> <target>`)", where)
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
