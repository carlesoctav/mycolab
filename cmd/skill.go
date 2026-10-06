package cmd

import (
	"fmt"

	"github.com/carlesoctav/mycolab/skills"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Print the mycolab agent skill to stdout",
	Long: `Print the bundled mycolab SKILL.md to stdout.

Ask the agent to run this before doing Colab work so it follows the
mycolab workflow (dev loop vs one-shot run, sync rules, tmux, gotchas):

    mycolab skill`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print(skills.Mycolab)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(skillCmd)
}
