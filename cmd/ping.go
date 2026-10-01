package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Keep a session alive by pinging its runtime every 60s",
	Long: `Keep a Colab session alive by running colab-cli's keep-alive loop for it.

Resolves the session's runtime endpoint from the active profile and execs
'colab keep-alive <endpoint> <session>', which pings the tunnel frontend
every 60s so the backend never idle-prunes the runtime. Runs in the
foreground until Ctrl-C; put it in tmux on an always-on host to survive
laptop lid closes.

The loop exits on its own after 24h, after 2 consecutive 4xx errors (dead
assignment), or when the session disappears from the local sessions.json,
which is re-read every minute — so keep sessions.json synced to this host.

Requires a colab-cli that still ships the loop (<= 0.7.2, i.e. before
googlecolab/google-colab-cli#144); 0.7.3+ removed it. Pair 0.7.2 with
jupyter-kernel-client==0.9.0.

Example:
  mycolab ping -s trainer
  tmux new -d -s ping 'mycolab ping -s trainer'`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPing(cmd)
	},
}

func init() {
	rootCmd.AddCommand(pingCmd)
}

// pingEndpoint returns the runtime endpoint recorded for a session.
func pingEndpoint(sessions []profile.Session, name string) (string, error) {
	for _, s := range sessions {
		if s.Name == name {
			if s.Endpoint == "" {
				return "", fmt.Errorf("session %q has no endpoint recorded (was it ever assigned?)", name)
			}
			return s.Endpoint, nil
		}
	}
	return "", fmt.Errorf("session %q not found in the active profile", name)
}

// colabSupportsKeepAlive probes for the pre-#144 keep-alive subcommand.
func colabSupportsKeepAlive(colabBin string) bool {
	out, err := exec.Command(colabBin, "keep-alive", "--help").CombinedOutput()
	return err == nil && !strings.Contains(string(out), "No such command")
}

func runPing(cmd *cobra.Command) error {
	session, err := requireSession(cmd)
	if err != nil {
		return err
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		return fmt.Errorf("colab binary not found in PATH")
	}
	current, err := profile.GetCurrent()
	if err != nil {
		return err
	}
	if current == "" {
		return fmt.Errorf("no active profile (use `mycolab use <profile_name>` first)")
	}
	sessions, err := profile.Sessions(current)
	if err != nil {
		return err
	}
	endpoint, err := pingEndpoint(sessions, session)
	if err != nil {
		return err
	}
	if !colabSupportsKeepAlive(colabBin) {
		return fmt.Errorf("this colab-cli has no keep-alive loop (removed in 0.7.3, googlecolab/google-colab-cli#144); use 0.7.2 with jupyter-kernel-client==0.9.0")
	}
	fmt.Printf("Pinging session %q (%s) every 60s; Ctrl-C to stop.\n", session, endpoint)
	loop := exec.Command(colabBin, "keep-alive", endpoint, session)
	loop.Stdin = os.Stdin
	loop.Stdout = os.Stdout
	loop.Stderr = os.Stderr
	return loop.Run()
}
