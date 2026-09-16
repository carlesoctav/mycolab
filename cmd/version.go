package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/carlesoctav/mycolab/cmd.Version=vX.Y.Z".
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the version of mycolab",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("mycolab %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
