package cmd

import (
	"fmt"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env <key> <value>",
	Short: "Add a custom env var synced to new runtimes",
	Long: `Add (or update) a custom env var. 'mycolab new' syncs these to the
runtime's ssh sessions alongside the captured runtime env.

    mycolab env HF_TOKEN abc123
    mycolab env delete HF_TOKEN
    mycolab env list`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return envAdd(args[0], args[1])
	},
}

var envAddCmd = &cobra.Command{
	Use:   "add <key> <value>",
	Short: "Add or update a custom env var",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return envAdd(args[0], args[1])
	},
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Delete a custom env var",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return envDelete(args[0])
	},
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List custom env var names",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return envList()
	},
}

func envAdd(key, value string) error {
	if err := profile.SetCustomEnv(key, value); err != nil {
		return err
	}
	fmt.Printf("Set custom env %s.\n", key)
	return nil
}

func envDelete(key string) error {
	if err := profile.DeleteCustomEnv(key); err != nil {
		return err
	}
	fmt.Printf("Deleted custom env %s.\n", key)
	return nil
}

func envList() error {
	vars, err := profile.CustomEnv()
	if err != nil {
		return err
	}
	for _, name := range profile.CustomEnvNames(vars) {
		fmt.Println(name)
	}
	return nil
}

// serverEnvCmd manages the custom env on the always-on server by running
// 'mycolab env ...' there over ssh.
var serverEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage custom env vars on the server (add, delete, list)",
}

func serverEnvRun(sub string, nargs int) *cobra.Command {
	c := &cobra.Command{
		Use:   sub,
		Short: sub + " a custom env var on the server",
		Args:  cobra.ExactArgs(nargs),
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := resolveServerHost(cmd)
			if err != nil {
				return err
			}
			return runOnServer(server, append([]string{"mycolab", "env", sub}, args...))
		},
	}
	switch sub {
	case "add":
		c.Use = "add <key> <value>"
	case "delete":
		c.Use = "delete <key>"
	}
	return c
}

func init() {
	envCmd.AddCommand(envAddCmd, envDeleteCmd, envListCmd)
	serverEnvCmd.AddCommand(serverEnvRun("add", 2), serverEnvRun("delete", 1), serverEnvRun("list", 0))
	rootCmd.AddCommand(envCmd)
	serverCmd.AddCommand(serverEnvCmd)
}
