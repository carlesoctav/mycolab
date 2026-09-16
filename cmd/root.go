package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "mycolab",
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "mycolab manages multiple colab-cli workspaces/accounts via switchable profiles",
	Long: `mycolab manages multiple colab-cli workspaces (accounts) as switchable profiles.

Each profile keeps its own session list and login token under
~/.config/mycolab. 'mycolab use <name>' activates a profile by symlinking
colab-cli's sessions.json and token.json at the profile's files, so plain
'colab ...' commands then operate on that workspace.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
