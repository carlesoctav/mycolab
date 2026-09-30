package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/carlesoctav/mycolab/pkg/tools"
	"github.com/spf13/cobra"
)

var toolCmd = &cobra.Command{
	Use:   "tool <tool_name>",
	Short: "Install a CLI tool on the session's runtime, with local credentials",
	Long: `Install a CLI tool on the runtime and copy its local
configuration/credential files over when available.

    mycolab tool -s trainer muse

Supported tools: muse. Unlike the best-effort 'mycolab ssh' push steps,
a failure here fails the command.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName, err := requireSession(cmd)
		if err != nil {
			return err
		}
		spec, err := tools.Lookup(args[0])
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
			return fmt.Errorf("session %q is not in profile %q (create it with `colab new -s %s`)", sessionName, current, sessionName)
		}
		colabBin, err := exec.LookPath("colab")
		if err != nil {
			return fmt.Errorf("colab binary not found in PATH")
		}
		if _, err := runColabForMarkerTimeouts(colabBin, spec.InstallStdin(toolMarker), toolMarker, execLongCallTimeout, execLongRemoteTimeout, "exec", "-s", sessionName); err != nil {
			return fmt.Errorf("%s install failed (%v)", spec.Name, err)
		}
		fmt.Printf("Installed %s on session %q.\n", spec.Name, sessionName)
		return pushToolConfig(colabBin, sessionName, spec)
	},
}

// toolMarker is echoed by the remote install command so we can confirm it
// went through (exec mixes notices into stdout, so look for this).
const toolMarker = "TOOL_INSTALL_OK"

// pushToolConfig copies the tool's local config files to the runtime.
// Files missing locally are skipped with a note; a failed upload of a
// present file fails the command.
func pushToolConfig(colabBin, session string, spec tools.Spec) error {
	if len(spec.ConfigFiles) == 0 {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("unable to determine user home directory: %w", err)
	}
	for _, f := range spec.ConfigFiles {
		local := filepath.Join(home, ".config", spec.ConfigDir, f)
		remote := filepath.Join("/root/.config", spec.ConfigDir, f)
		if _, err := os.Stat(local); err != nil {
			fmt.Printf("Note: no local %s; skipping (nothing to copy).\n", local)
			continue
		}
		if _, err := runColab(colabBin, "", "upload", "-s", session, local, remote); err != nil {
			return fmt.Errorf("upload %s failed: %w", local, err)
		}
		fmt.Printf("Copied %s -> %s.\n", local, remote)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(toolCmd)
}
