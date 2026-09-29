package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Run lsyncd live sync in the foreground (local -> remote)",
	Long: `Run lsyncd live sync in the foreground using ./lsyncd.conf.lua (run
from the project directory).

Equivalent to 'lsyncd lsyncd.conf.lua'; stop it with Ctrl+C. The config
is written by 'mycolab lsyncd -s <session> <source> <target>'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return syncLive(".")
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}

// findLsyncd locates the lsyncd binary. A variable so tests can stub it.
var findLsyncd = func() (string, error) {
	return exec.LookPath("lsyncd")
}

// resolveSyncConfig returns the lsyncd.conf.lua path in dir, or an error
// when it is missing.
func resolveSyncConfig(dir string) (string, error) {
	path := filepath.Join(dir, "lsyncd.conf.lua")
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		where := dir
		if where == "." {
			where = "the current directory"
		}
		return "", fmt.Errorf("no lsyncd.conf.lua in %s (scaffold one with `mycolab lsyncd -s <session> <source> <target>`)", where)
	}
	return path, nil
}

// syncLive runs lsyncd on the config in dir, attached to the terminal.
func syncLive(dir string) error {
	if _, err := resolveSyncConfig(dir); err != nil {
		return err
	}
	bin, err := findLsyncd()
	if err != nil {
		return fmt.Errorf("lsyncd not found in PATH (install lsyncd to run live sync)")
	}
	fmt.Printf("Starting live sync (%s).\n", filepath.Join(dir, "lsyncd.conf.lua"))
	c := exec.Command(bin, "lsyncd.conf.lua")
	c.Dir = dir
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
