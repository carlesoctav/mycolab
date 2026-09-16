package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all profiles (* marks the active one)",
	RunE: func(cmd *cobra.Command, args []string) error {
		profiles, err := profile.List()
		if err != nil {
			return err
		}
		if len(profiles) == 0 {
			dir, _ := profile.MycolabDir()
			fmt.Printf("No profiles found in %s\nUse `mycolab add <profile_name>` to create one.\n", dir)
			return nil
		}
		current, _ := profile.GetCurrent()
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, " \tPROFILE\tSESSIONS\tLOGGED IN")
		for _, p := range profiles {
			marker := " "
			if p == current {
				marker = "*"
			}
			count, _ := profile.SessionCount(p)
			auth := "no"
			if ok, _ := profile.HasToken(p); ok {
				auth = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", marker, p, count, auth)
		}
		tw.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
