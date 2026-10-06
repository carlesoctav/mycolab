package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

// sshCmd is hidden: it is the ProxyCommand written into ~/.ssh/colab_config
// by 'mycolab prepare' (one per session: 'ProxyCommand mycolab ssh -s
// <session>'), so plain 'ssh <session>' reaches the runtime. Unlike 'colab
// ssh -s', which auto-creates the named session on first connect, this
// command refuses when the session is missing from the active profile — a
// typo must never silently spin up a billable VM.
var sshCmd = &cobra.Command{
	Use:    "ssh",
	Hidden: true,
	Short:  "Bridge stdio to a session's Colab SSH tunnel (SSH ProxyCommand)",
	Long: `Bridge this process's stdio to a Colab session's SSH endpoint.

This is the ProxyCommand managed by 'mycolab prepare', not an interactive
command: ssh invokes it as 'mycolab ssh -s <session>' whenever a new
multiplex master dials. It checks that the session exists in the active
profile and fails otherwise, then execs 'colab ssh --proxy-mode' with
stdio attached (propagating its exit code).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := requireSession(cmd)
		if err != nil {
			return err
		}
		colabBin, err := resolveBridgeSession(session)
		if err != nil {
			return err
		}
		return runBridge(colabBin, session)
	},
}

// resolveBridgeSession verifies the session exists in the active profile
// (never creating it) and returns the colab binary for the bridge.
func resolveBridgeSession(session string) (string, error) {
	current, err := profile.GetCurrent()
	if err != nil {
		return "", err
	}
	if current == "" {
		return "", fmt.Errorf("no active profile (use `mycolab use <profile_name>` first)")
	}
	sessions, err := profile.Sessions(current)
	if err != nil {
		return "", err
	}
	if !sessionExists(sessions, session) {
		return "", fmt.Errorf("session %q is not in profile %q; refusing to auto-create it (run `mycolab new -s %s` first)", session, current, session)
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		return "", fmt.Errorf("colab binary not found in PATH")
	}
	return colabBin, nil
}

// runBridge execs the colab websocket bridge with stdio attached,
// propagating its exit code for the ProxyCommand slot.
func runBridge(colabBin, session string) error {
	c := exec.Command(colabBin, "ssh", "--proxy-mode", "-s", session)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func init() {
	rootCmd.AddCommand(sshCmd)
}
