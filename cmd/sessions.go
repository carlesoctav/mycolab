package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List Colab sessions in the active profile",
	Long: `Run 'colab sessions' in the active profile: list every session
colab-cli currently tracks.

    mycolab sessions`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := requireActiveProfile(); err != nil {
			return err
		}
		colabBin, err := exec.LookPath("colab")
		if err != nil {
			return fmt.Errorf("colab binary not found in PATH")
		}
		return runColabForeground(colabBin, []string{"sessions"})
	},
}

func init() {
	rootCmd.AddCommand(sessionsCmd)
}
