package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a Colab session, then wire it for ssh",
	Long: `Run 'colab new' for -s/--session in the active profile, then wire
it for ssh (Host entry plus runtime push steps):

    mycolab new -s trainer --gpu L4

Accelerator flags mirror 'colab new'; the --no-* flags tune the setup
step. Note: 'new' with an existing name replaces the runtime (the old
one is orphaned until Colab reclaims it).`,
	Args: cobra.NoArgs,
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
		colabBin, err := exec.LookPath("colab")
		if err != nil {
			return fmt.Errorf("colab binary not found in PATH")
		}
		if err := runColabForeground(colabBin, colabNewArgs(cmd, sessionName)); err != nil {
			return err
		}
		dropMaster(sessionName)
		return runSSHSetup(cmd, sessionName)
	},
}

func init() {
	bindAcceleratorFlags(newCmd)
	bindSSHSetupFlags(newCmd)
	rootCmd.AddCommand(newCmd)
}

// bindAcceleratorFlags registers the 'colab new' accelerator flags on c.
// Shared by 'mycolab new' and 'server new'.
func bindAcceleratorFlags(c *cobra.Command) {
	c.Flags().String("gpu", "", "GPU accelerator for the new runtime (T4, L4, G4, H100, A100)")
	c.Flags().String("tpu", "", "TPU accelerator for the new runtime (v5e1, v6e1)")
	c.Flags().Bool("high-mem", false, "request a high-RAM machine shape")
}

// colabNewArgs builds the 'colab new' argument list (without the binary)
// from the session and accelerator flags. Shared by 'mycolab new' (local)
// and 'server new' (remote).
func colabNewArgs(cmd *cobra.Command, session string) []string {
	args := []string{"new", "-s", session}
	if gpu, _ := cmd.Flags().GetString("gpu"); gpu != "" {
		args = append(args, "--gpu", gpu)
	}
	if tpu, _ := cmd.Flags().GetString("tpu"); tpu != "" {
		args = append(args, "--tpu", tpu)
	}
	if highMem, _ := cmd.Flags().GetBool("high-mem"); highMem {
		args = append(args, "--high-mem")
	}
	return args
}

// runColabForeground runs the colab binary with args, streaming stdio both
// ways so login flows and progress output work.
func runColabForeground(colabBin string, args []string) error {
	c := exec.Command(colabBin, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("colab %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}
