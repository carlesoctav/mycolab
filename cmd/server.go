package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/carlesoctav/mycolab/pkg/sshconfig"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run colab operations on the always-on server over SSH",
	Long: `Run colab operations on the always-on server (e.g. a free-tier
micro VM) over SSH. The server must have the mycolab and colab binaries
installed; select it with 'mycolab server connect <ssh-host>' first
(the host must be defined in ~/.ssh/config).

Profiles are tracked locally only: every subcommand pushes the active
profile's token to a same-named mirror profile on the server, runs
there, and pulls merged sessions back. Switching accounts is just
'mycolab use <profile>' — the next server command pushes that account.

    mycolab server connect free        # select the server (stored locally)
    mycolab server new -s trainer --gpu L4`,
}

func init() {
	serverCmd.PersistentFlags().String("server", "", "SSH host to use instead of the connected server")
	bindAcceleratorFlags(serverNewCmd)
	bindSSHSetupFlags(serverNewCmd)
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(serverConnectCmd, serverNewCmd)
}

// loadKnownSSHHosts lists the Host names declared in the user's ssh config
// (Include files followed). A variable so tests can stub it.
var loadKnownSSHHosts = func() ([]string, error) {
	mainConfig, err := profile.MainSSHConfig()
	if err != nil {
		return nil, err
	}
	return sshconfig.LoadHostNames(mainConfig)
}

// validateServerHost rejects names with no exact Host entry in the ssh
// config. The name is also embedded in a generated ProxyCommand message,
// so whitespace, quotes and shell metacharacters are rejected, as is a
// leading '-' (it would parse as an ssh flag).
func validateServerHost(hosts []string, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\r\n#*?!'\"\\$`;:&|<>()") {
		return fmt.Errorf("invalid ssh host name %q", name)
	}
	for _, h := range hosts {
		if h == name {
			return nil
		}
	}
	mainConfig, _ := profile.MainSSHConfig()
	return fmt.Errorf("ssh host %q not found in %s (define a `Host %s` block there first)", name, mainConfig, name)
}

// resolveServerHost returns the server for a server subcommand: the
// --server flag wins, otherwise the host stored by `server connect`.
func resolveServerHost(cmd *cobra.Command) (string, error) {
	hosts, err := loadKnownSSHHosts()
	if err != nil {
		return "", err
	}
	if flag, _ := cmd.Flags().GetString("server"); flag != "" {
		if err := validateServerHost(hosts, flag); err != nil {
			return "", err
		}
		return flag, nil
	}
	name, err := profile.GetServer()
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("no server selected (run `mycolab server connect <ssh-host>` first)")
	}
	if err := validateServerHost(hosts, name); err != nil {
		return "", err
	}
	return name, nil
}

// remotePATHPrefix prepends the usual user install dirs to PATH for remote
// commands: mycolab installs into ~/.local/bin by default, which a
// non-interactive ssh shell typically lacks.
const remotePATHPrefix = `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`

// serverSSHArgs builds the local ssh invocation that runs remote on server.
// Every remote word is quoted so session and profile names cannot inject
// extra shell commands; tty requests a remote terminal for interactive
// prompts (the `use` picker).
func serverSSHArgs(server string, remote []string, tty bool) []string {
	quoted := make([]string, 0, len(remote)+1)
	quoted = append(quoted, remotePATHPrefix)
	for _, a := range remote {
		quoted = append(quoted, shellQuote(a))
	}
	args := []string{}
	if tty {
		args = append(args, "-t")
	}
	return append(args, server, strings.Join(quoted, " "))
}

// stdinIsTerminal reports whether local stdin is a terminal. A variable so
// tests can stub it.
var stdinIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

var findSSH = func() (string, error) {
	return exec.LookPath("ssh")
}

// runOnServer runs remote on server via ssh, streaming stdio both ways so
// remote interactive prompts (login flows, pickers) work.
func runOnServer(server string, remote []string) error {
	sshBin, err := findSSH()
	if err != nil {
		return fmt.Errorf("ssh binary not found in PATH")
	}
	c := exec.Command(sshBin, serverSSHArgs(server, remote, stdinIsTerminal())...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("server %q: %s failed: %w", server, strings.Join(remote, " "), err)
	}
	return nil
}

// runOnServerOutput runs remote on server via ssh and returns its stdout.
// Stdin is detached, so remote commands see a non-terminal.
func runOnServerOutput(server string, remote []string) (string, error) {
	sshBin, err := findSSH()
	if err != nil {
		return "", fmt.Errorf("ssh binary not found in PATH")
	}
	c := exec.Command(sshBin, serverSSHArgs(server, remote, false)...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("server %q: %s failed: %w: %s", server, strings.Join(remote, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// dropServerMaster closes the server-side multiplex master for session, if
// one is running. Best-effort: a missing master or Host block is fine.
func dropServerMaster(server, session string) {
	_, _ = runOnServerOutput(server, []string{"ssh", "-O", "exit", session})
}

var serverConnectCmd = &cobra.Command{
	Use:   "connect <ssh-host>",
	Short: "Select the always-on server (an SSH host from ~/.ssh/config)",
	Long: `Select the always-on server by its ssh host name.

The host must be defined in ~/.ssh/config (includes followed). The
choice is stored locally and used by every other 'mycolab server'
command; '--server <ssh-host>' overrides it for one invocation.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts, err := loadKnownSSHHosts()
		if err != nil {
			return err
		}
		if err := validateServerHost(hosts, args[0]); err != nil {
			return err
		}
		if err := profile.SetServer(args[0]); err != nil {
			return err
		}
		fmt.Printf("Server set to %q (ssh %s).\n", args[0], args[0])
		fmt.Println("The server needs the mycolab and colab binaries installed.")
		fmt.Println("Create a session with `mycolab server new -s <session>`.")
		return nil
	},
}

var serverNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a Colab session on the server, then wire it locally",
	Long: `Push the active profile's token to the server, run 'colab new'
there for -s/--session, then wire it on both ends (server-side prep,
pull merged sessions back, local entry):

    mycolab server new -s trainer --gpu L4

Accelerator flags mirror 'colab new'; the --no-* flags tune the
server-side prep. Afterwards the session is usable both nested (via
'ssh <server>') and directly from here. Note: 'new' with an existing
name replaces the runtime.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName, err := requireSession(cmd)
		if err != nil {
			return err
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
		if err := runOnServer(server, append([]string{"colab"}, colabNewArgs(cmd, sessionName)...)); err != nil {
			return err
		}
		dropServerMaster(server, sessionName)
		return runServerSSH(cmd, server, current, sessionName)
	},
}

// runServerSSH wires session on both ends: server-side setup, pull merged
// sessions back, then the direct local entry. The caller pushes the active
// profile's token first. Only 'server new' runs this.
func runServerSSH(cmd *cobra.Command, server, current, session string) error {
	// Server side: Host entry plus runtime push steps.
	remoteSSH := append([]string{"mycolab", "ssh", "-s", session}, sshSetupPassthrough(cmd)...)
	if err := runOnServer(server, remoteSSH); err != nil {
		return err
	}
	// Pull merged sessions back, then write the direct local entry
	// (prep already ran on the server, so it is skipped here).
	if err := pullServerSessions(server, current); err != nil {
		return err
	}
	return runLocalWiring(session)
}

// sshSetupPassthrough returns the ssh opt-out flags set on cmd, for
// forwarding to the server-side 'mycolab ssh'.
func sshSetupPassthrough(cmd *cobra.Command) []string {
	var out []string
	for _, f := range sshSetupFlagNames {
		if v, _ := cmd.Flags().GetBool(f); v {
			out = append(out, "--"+f)
		}
	}
	return out
}
