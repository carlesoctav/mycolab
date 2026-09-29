package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install <package> [package...]",
	Short: "Install tools on the session's runtime with uv",
	Long: `Install one or more tools on the runtime with 'uv tool install'.

The session comes from the '-s/--session' flag:

    mycolab install -s trainer ruff
    mycolab install -s trainer ruff typos

Each package is installed through the 'colab exec' door, so it lands in
the runtime environment. Unlike the best-effort 'mycolab ssh' push steps,
a failure here fails the command. uv must already exist on the runtime.`,
	Args: cobra.MinimumNArgs(1),
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
			return fmt.Errorf("session %q is not in profile %q (create it with `colab new -s %s`)", sessionName, current, sessionName)
		}
		colabBin, err := exec.LookPath("colab")
		if err != nil {
			return fmt.Errorf("colab binary not found in PATH")
		}
		out, err := runColabForMarker(colabBin, uvInstallStdin(args), uvInstallMarker, "exec", "-s", sessionName)
		if err != nil {
			return fmt.Errorf("uv install failed (%v). Last output:\n%s\n(if uv is missing on the runtime, install it first, e.g. `echo '!pip install uv' | colab exec -s %s`)", err, out, sessionName)
		}
		fmt.Printf("Installed %s on session %q.\n", strings.Join(args, ", "), sessionName)
		return nil
	},
}

// uvInstallMarker is echoed by the remote command so we can confirm every
// install in the chain went through (exec mixes notices into stdout, so
// look for this).
const uvInstallMarker = "UV_INSTALL_OK"

// uvInstallStdin builds the `colab exec` stdin that installs each package
// with 'uv tool install'. The installs chain with && so one failure stops
// the rest and the marker is only reached when all succeeded.
func uvInstallStdin(pkgs []string) string {
	steps := make([]string, len(pkgs))
	for i, p := range pkgs {
		steps[i] = "uv tool install " + shellQuote(p)
	}
	return "!" + strings.Join(steps, " && ") + " && echo " + uvInstallMarker + "\n"
}

// shellQuote wraps s in single quotes for the remote shell, escaping
// embedded quotes so arguments cannot inject extra commands.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func init() {
	rootCmd.AddCommand(installCmd)
}
