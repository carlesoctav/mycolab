package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/carlesoctav/mycolab/pkg/sshconfig"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var sshCmd = &cobra.Command{
	Use:   "ssh [session_name]",
	Short: "Point the 'colab' SSH host at one of the active profile's sessions",
	Long: `Write a 'Host colab' entry for a session into ~/.ssh/colab_config.

With a session name, that session is used directly. Without one, an
interactive picker is shown (j/k or arrow keys to move, Enter to select).

The entry uses 'colab ssh --proxy-mode' as its ProxyCommand and follows the
active mycolab profile. It is kept clean (no RemoteCommand) so editors can
run their own remote commands. Multiplexing (ControlMaster auto) is enabled
because Colab allows a single concurrent proxy connection: shells, rsync,
and lsyncd share it instead of tripping HTTP 429 against each other.
Your main ~/.ssh/config must contain
'Include ~/.ssh/colab_config' in global scope, before any Host block (this
command offers to add it); afterwards connect with 'ssh colab'.`,
	ValidArgsFunction: completeSessionNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		sessionName := ""
		if len(args) == 1 {
			sessionName = args[0]
			if !sessionExists(sessions, sessionName) {
				fmt.Printf("Note: session %q is not in profile %q yet; "+
					"`colab ssh` will auto-create it on first connect.\n", sessionName, current)
			}
		} else {
			if len(sessions) == 0 {
				return fmt.Errorf("profile %q has no sessions (create one with `colab new -s <name>`, or pass a name to auto-create on connect)", current)
			}
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				names := make([]string, len(sessions))
				for i, s := range sessions {
					names[i] = s.Name
				}
				return fmt.Errorf("no interactive terminal; pass a session explicitly: `mycolab ssh <%s>`", strings.Join(names, "|"))
			}
			lines := make([]string, len(sessions))
			for i, s := range sessions {
				lines[i] = formatSession(s)
			}
			idx, err := Select(fmt.Sprintf("Select session (profile: %s):", current), lines)
			if err != nil {
				if errors.Is(err, ErrCancelled) {
					return nil
				}
				return err
			}
			sessionName = sessions[idx].Name
		}

		configPath, err := profile.ColabSSHConfig()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			return err
		}
		block := colabHostBlock(current, sessionName)
		if err := os.WriteFile(configPath, []byte(block), 0o600); err != nil {
			return err
		}
		fmt.Printf("Wrote Host colab (session %q) to %s\n", sessionName, configPath)

		if err := ensureSSHInclude(); err != nil {
			return err
		}
		if err := verifyColabHost(sessionName); err != nil {
			return err
		}
		fmt.Println("Connect with `ssh colab`.")
		return nil
	},
}

// colabHostBlock renders the managed 'Host colab' entry. Options mirror
// colab's own ssh invocation (_ssh_base_args plus target
// root@colab-runtime): the bridge needs User root and disabled host-key
// checking. Agent forwarding stays on so keys held locally work on the
// runtime. Multiplexing is on because Colab allows only one concurrent
// proxy connection per runtime: the first connection becomes the master
// and later ones (interactive shells, rsync, lsyncd) share it as extra
// channels instead of fighting over the slot with HTTP 429.
func colabHostBlock(profile, session string) string {
	return fmt.Sprintf(`# Managed by mycolab — regenerated on every 'mycolab ssh' run, manual edits will be lost.
# Profile: %s | Session: %s
Host colab
    HostName colab-runtime
    User root
    ProxyCommand colab ssh --proxy-mode -s %s
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
    ForwardAgent yes
    AddKeysToAgent yes
    ControlMaster auto
    ControlPath ~/.ssh/cm-%%C
    ControlPersist 10m
`, profile, session, session)
}

func sessionExists(sessions []profile.Session, name string) bool {
	for _, s := range sessions {
		if s.Name == name {
			return true
		}
	}
	return false
}

func formatSession(s profile.Session) string {
	hw := s.Accelerator
	if hw == "" || hw == "NONE" {
		hw = "CPU"
	}
	return fmt.Sprintf("%s  [%s]", s.Name, hw)
}

func completeSessionNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	current, err := profile.GetCurrent()
	if err != nil || current == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	sessions, err := profile.Sessions(current)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var results []string
	for _, s := range sessions {
		results = append(results, s.Name+"\t"+formatSession(s))
	}
	return results, cobra.ShellCompDirectiveNoFileComp
}

// ensureSSHInclude makes sure the main ~/.ssh/config includes the
// mycolab-managed file from global scope (an Include inside a Host block is
// conditional and would not apply to Host colab).
func ensureSSHInclude() error {
	mainConfig, err := profile.MainSSHConfig()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(mainConfig)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	const includeLine = "Include ~/.ssh/colab_config"
	updated, changed := sshconfig.EnsureGlobalInclude(string(content), includeLine, "colab_config")
	if !changed {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("Add this line near the top of %s (before any Host block) to enable the host:\n  %s\n", mainConfig, includeLine)
		return nil
	}
	backup := ""
	if len(content) > 0 {
		backup = sshconfig.BackupPath(mainConfig)
	}
	question := fmt.Sprintf("Add global `%s` to %s?", includeLine, mainConfig)
	if backup != "" {
		question = fmt.Sprintf("Add global `%s` to %s (backup to %s)?", includeLine, mainConfig, backup)
	}
	if !promptYesNo(question, true) {
		fmt.Printf("Skipped. Add this line near the top of %s (before any Host block) to enable the host:\n  %s\n", mainConfig, includeLine)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(mainConfig), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(mainConfig); err == nil {
		mode = fi.Mode().Perm()
	}
	if backup != "" {
		if err := os.WriteFile(backup, content, mode); err != nil {
			return err
		}
	}
	if err := os.WriteFile(mainConfig, []byte(updated), mode); err != nil {
		return err
	}
	fmt.Printf("Added global `%s` to %s\n", includeLine, mainConfig)
	return nil
}

// verifyColabHost checks via `ssh -G` that the managed host actually
// resolves to the managed ProxyCommand. A missing ssh binary skips the check.
// MYCOLAB_SSH_CONFIG overrides the config file under test (ssh ignores $HOME
// when locating its own config, so tests point it at a fixture with -F).
func verifyColabHost(session string) error {
	return verifyManagedHost("colab", session)
}

func verifyManagedHost(host, session string) error {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return nil
	}
	args := []string{"-G", host}
	if alt := os.Getenv("MYCOLAB_SSH_CONFIG"); alt != "" {
		args = []string{"-F", alt, "-G", host}
	}
	out, err := exec.Command(sshBin, args...).Output()
	if err != nil {
		return nil
	}
	proxy := ""
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "proxycommand "); ok {
			proxy = rest
			break
		}
	}
	if proxy == "" {
		return fmt.Errorf("'ssh %s' has no ProxyCommand: the 'Include ~/.ssh/colab_config' line may be missing or inactive", host)
	}
	pinned := false
	fields := strings.Fields(proxy)
	for i, f := range fields {
		if f == "-s" && i+1 < len(fields) && fields[i+1] == session {
			pinned = true
			break
		}
	}
	if !strings.Contains(proxy, "--proxy-mode") || !pinned {
		return fmt.Errorf("'ssh %s' is shadowed by another config block (effective ProxyCommand: %s). Remove the conflicting 'Host %s' block and re-run `mycolab ssh`", host, proxy, host)
	}
	return nil
}

func promptYesNo(question string, def bool) bool {
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	fmt.Printf("%s %s ", question, hint)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return def
	}
	return answer == "y" || answer == "yes"
}

func init() {
	rootCmd.AddCommand(sshCmd)
}
