package cmd

import (
	"fmt"
	"os"

	"github.com/carlesoctav/mycolab/pkg/profile"
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
'colab ...' commands then operate on that workspace.

Session-scoped commands (ssh, lsyncd, install) take the session from the
'-s/--session' flag, e.g. 'mycolab ssh -s trainer'.`,
}

// requireSession returns the '-s/--session' value, or an error when it is
// missing or unusable as an SSH hostname.
func requireSession(cmd *cobra.Command) (string, error) {
	session, _ := cmd.Flags().GetString("session")
	if session == "" {
		return "", fmt.Errorf("no session selected (pass `-s <session>`, e.g. `mycolab %s -s trainer`)", cmd.Name())
	}
	if err := validateSessionHost(session); err != nil {
		return "", err
	}
	return session, nil
}

func completeSessionFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	current, err := profile.GetCurrent()
	if err != nil || current == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	sessions, err := profile.Sessions(current)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var results []string
	for _, s := range sessions {
		results = append(results, s.Name+"\t"+formatSession(s))
	}
	return results, cobra.ShellCompDirectiveNoFileComp
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("session", "s", "", "colab session to operate on (used by ssh, lsyncd, install)")
	rootCmd.RegisterFlagCompletionFunc("session", completeSessionFlag)
}
