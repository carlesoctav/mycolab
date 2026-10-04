package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var serverRunCmd = &cobra.Command{
	Use:   "run [flags] -- <command>",
	Short: "Run a command on a new Colab session, orchestrated from the server",
	Long: `Like 'mycolab run', but everything runs on the always-on server: the
active profile's token is pushed there, the --dir dirs are copied to
~/mycolab/run/<session>/<id>/ on the server, and the server creates the
session, mounts buckets (-v), rsyncs the dirs to the runtime, runs the
command and captures the log at ~/mycolab/run/<session>/<id>.log (on the
server). The staged copy is removed afterwards unless --persistent.

    mycolab server run -s trainer --gpu L4 --dir ./proj:/content/proj \
        -v myuser/data:/content/data --timeout 6h -- python train.py

By default the job is detached on the server, so you can close your laptop;
follow it with 'ssh <server> tail -f ~/mycolab/run/<session>/<id>.log'.
--no-daemon streams the log back to this terminal. --timeout stops the
session (colab stop, on the server) if the command overruns.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		j, err := jobFromFlags(cmd, args)
		if err != nil {
			return err
		}
		for _, d := range j.Dirs {
			if st, err := os.Stat(d.Local); err != nil || !st.IsDir() {
				return fmt.Errorf("--dir: %s is not a directory", d.Local)
			}
		}
		server, err := resolveServerHost(cmd)
		if err != nil {
			return err
		}
		current, err := requireActiveProfile()
		if err != nil {
			return err
		}
		if err := pushServerState(server, current); err != nil {
			return err
		}
		if _, err := runOnServerOutput(server, []string{"sh", "-c", "command -v rsync >/dev/null && command -v colab >/dev/null && command -v mycolab >/dev/null"}); err != nil {
			return fmt.Errorf("server %q needs rsync, colab and mycolab on PATH (e.g. `ssh %s sudo apt-get install -y rsync`)", server, server)
		}
		home, err := cachedRemoteHome(server)
		if err != nil {
			return err
		}
		remoteStage := fmt.Sprintf("%s/mycolab/run/%s/%s", home, j.Session, j.ID)
		staged := make([]dirMap, len(j.Dirs))
		if len(j.Dirs) > 0 {
			rsyncBin, err := exec.LookPath("rsync")
			if err != nil {
				return fmt.Errorf("rsync not found in PATH")
			}
			for i, d := range j.Dirs {
				dst := fmt.Sprintf("%s/%d", remoteStage, i)
				fmt.Printf("Copying %s -> %s:%s\n", d.Local, server, dst)
				args := append([]string{"-az"}, rsyncIgnoreFilters(d.Local)...)
				args = append(args, "-e", "ssh",
					"--rsync-path", "mkdir -p "+shellQuote(dst)+" && rsync",
					strings.TrimRight(d.Local, "/")+"/", server+":"+dst+"/")
				c := exec.Command(rsyncBin, args...)
				c.Stdout, c.Stderr = os.Stdout, os.Stderr
				if err := c.Run(); err != nil {
					return fmt.Errorf("copy %s to server failed: %w", d.Local, err)
				}
				staged[i] = dirMap{Local: dst, Remote: d.Remote}
			}
		}
		remote := []string{"mycolab", "run", "-s", j.Session, "--id", j.ID}
		if len(staged) > 0 {
			remote = append(remote, "--staged")
		}
		for _, d := range staged {
			remote = append(remote, "--dir", d.Local+":"+d.Remote)
		}
		for _, m := range j.Mounts {
			remote = append(remote, "-v", m.Bucket+":"+m.Remote)
		}
		if j.Timeout > 0 {
			remote = append(remote, "--timeout", j.Timeout.String())
		}
		if j.Persistent {
			remote = append(remote, "--persistent")
		}
		if j.Reuse {
			remote = append(remote, "--reuse")
		}
		if nd, _ := cmd.Flags().GetBool("no-daemon"); nd {
			remote = append(remote, "--no-daemon")
		}
		remote = append(remote, acceleratorPassthrough(cmd)...)
		remote = append(remote, sshSetupPassthrough(cmd)...)
		remote = append(remote, "--")
		remote = append(remote, j.Command...)
		if err := runOnServer(server, remote); err != nil {
			return err
		}
		logRel := filepath.ToSlash(filepath.Join("mycolab", "run", j.Session, j.ID+".log"))
		fmt.Printf("Server log: %s:~/%s\n", server, logRel)
		return nil
	},
}

// acceleratorPassthrough returns the accelerator flags set on cmd, for
// forwarding to the server-side 'mycolab run'.
func acceleratorPassthrough(cmd *cobra.Command) []string {
	var out []string
	if v, _ := cmd.Flags().GetString("gpu"); v != "" {
		out = append(out, "--gpu", v)
	}
	if v, _ := cmd.Flags().GetString("tpu"); v != "" {
		out = append(out, "--tpu", v)
	}
	if v, _ := cmd.Flags().GetBool("high-mem"); v {
		out = append(out, "--high-mem")
	}
	return out
}

func init() {
	bindAcceleratorFlags(serverRunCmd)
	bindSSHSetupFlags(serverRunCmd)
	bindRunFlags(serverRunCmd)
	for _, f := range []string{"id", "staged", "detached"} {
		_ = serverRunCmd.Flags().MarkHidden(f)
	}
	serverCmd.AddCommand(serverRunCmd)
}
