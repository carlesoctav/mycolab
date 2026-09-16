package cmd

import (
	"fmt"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <profile_name>",
	Short: "Create a new empty profile",
	Long: `Create a new empty profile (an empty session list plus an empty token file).

The profile starts logged out; run 'mycolab use <profile_name>' to activate
it, then any 'colab ...' command will trigger the login flow for that
account on first use.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		path, err := profile.Create(name)
		if err != nil {
			return err
		}
		fmt.Printf("Profile %q created at %s\n", name, path)
		fmt.Printf("Activate it with `mycolab use %s`.\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
