package cmd

import (
	"fmt"
	"os/exec"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a Colab session",
	Long: `Run 'colab stop' for -s/--session in the active profile, then close
the local multiplex master for it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName, err := requireSession(cmd)
		if err != nil {
			return err
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
		if !sessionExists(sessions, sessionName) {
			return fmt.Errorf("session %q is not in profile %q", sessionName, current)
		}
		colabBin, err := exec.LookPath("colab")
		if err != nil {
			return fmt.Errorf("colab binary not found in PATH")
		}
		if err := runColabForeground(colabBin, []string{"stop", "-s", sessionName}); err != nil {
			return err
		}
		dropMaster(sessionName)
		fmt.Printf("Session %q stopped.\n", sessionName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
