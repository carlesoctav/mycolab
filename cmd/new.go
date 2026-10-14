package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a Colab session, then wire it for ssh",
	Long: `Run 'colab new' for -s/--session in the active profile, then wire
it for ssh (Host entry plus runtime push steps):

    mycolab new -s trainer --gpu L4
    mycolab new -s trainer --gpu L4 -v me/data:/content/data
    mycolab new -s trainer --gpu L4 -L 8000:localhost:8000

Accelerator flags mirror 'colab new'; the --no-* flags tune the setup
step. Mount HF Storage Buckets with -v/--volume. Port forwards (-L/-R)
establish an SSH multiplex master holding the ports open. Note: 'new' with
an existing name replaces the runtime (the old one is orphaned until Colab
reclaims it).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName, err := requireSession(cmd)
		if err != nil {
			return err
		}
		volumes, _ := cmd.Flags().GetStringArray("volume")
		var mounts []mountSpec
		for _, v := range volumes {
			m, err := parseMount(v)
			if err != nil {
				return err
			}
			mounts = append(mounts, m)
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
		if err := runSSHSetup(cmd, sessionName); err != nil {
			return err
		}
		if err := mountVolumes(sessionName, mounts); err != nil {
			return err
		}
		forwards, err := forwardArgsFromFlags(cmd)
		if err != nil {
			return err
		}
		if len(forwards) > 0 {
			logf := func(format string, a ...any) { fmt.Printf("[mycolab new] "+format+"\n", a...) }
			if err := ensureMaster(context.Background(), sessionName, logf, setupMasterBudget, forwards...); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	bindAcceleratorFlags(newCmd)
	bindSSHSetupFlags(newCmd)
	bindVolumeFlags(newCmd)
	bindForwardFlags(newCmd)
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

// bindForwardFlags registers -L/--local-forward and -R/--remote-forward on c.
func bindForwardFlags(c *cobra.Command) {
	c.Flags().StringArrayP("local-forward", "L", nil, "forward local port to remote address (repeatable, format [bind_address:]port:host:hostport)")
	c.Flags().StringArrayP("remote-forward", "R", nil, "forward remote port to local address (repeatable, format [bind_address:]port:host:hostport)")
	origNorm := c.Flags().GetNormalizeFunc()
	c.Flags().SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		switch name {
		case "L":
			name = "local-forward"
		case "R":
			name = "remote-forward"
		}
		if origNorm != nil {
			return origNorm(f, name)
		}
		return pflag.NormalizedName(name)
	})
}

// forwardArgsFromFlags extracts the -L and -R arguments from flags.
func forwardArgsFromFlags(cmd *cobra.Command) ([]string, error) {
	var args []string
	lForwards, _ := cmd.Flags().GetStringArray("local-forward")
	if cmd.Flags().Changed("local-forward") && len(lForwards) == 0 {
		return nil, fmt.Errorf("invalid -L/--local-forward: value cannot be empty")
	}
	for _, f := range lForwards {
		if strings.TrimSpace(f) == "" {
			return nil, fmt.Errorf("invalid -L/--local-forward: value cannot be empty")
		}
		args = append(args, "-L", f)
	}
	rForwards, _ := cmd.Flags().GetStringArray("remote-forward")
	if cmd.Flags().Changed("remote-forward") && len(rForwards) == 0 {
		return nil, fmt.Errorf("invalid -R/--remote-forward: value cannot be empty")
	}
	for _, f := range rForwards {
		if strings.TrimSpace(f) == "" {
			return nil, fmt.Errorf("invalid -R/--remote-forward: value cannot be empty")
		}
		args = append(args, "-R", f)
	}
	return args, nil
}

