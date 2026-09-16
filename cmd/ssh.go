package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
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
active mycolab profile. Your main ~/.ssh/config must contain
'Include ~/.ssh/colab_config' (this command offers to add it); afterwards
connect with 'ssh colab'.`,
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
		block := fmt.Sprintf(`# Managed by mycolab — regenerated on every 'mycolab ssh' run, manual edits will be lost.
# Profile: %s | Session: %s
Host colab
    ProxyCommand colab ssh --proxy-mode -s %s
`, current, sessionName, sessionName)
		if err := os.WriteFile(configPath, []byte(block), 0o600); err != nil {
			return err
		}
		fmt.Printf("Wrote Host colab (session %q) to %s\n", sessionName, configPath)

		if err := ensureSSHInclude(); err != nil {
			return err
		}
		fmt.Println("Connect with `ssh colab`.")
		return nil
	},
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
// mycolab-managed file, offering to append the Include line when needed.
func ensureSSHInclude() error {
	mainConfig, err := profile.MainSSHConfig()
	if err != nil {
		return err
	}
	included, err := sshConfigHasInclude(mainConfig)
	if err != nil {
		return err
	}
	if included {
		return nil
	}
	const includeLine = "Include ~/.ssh/colab_config"
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("Add this line to %s to enable the host:\n  %s\n", mainConfig, includeLine)
		return nil
	}
	fmt.Printf("%s does not include the mycolab config.\n", mainConfig)
	if !promptYesNo(fmt.Sprintf("Append `%s` to %s?", includeLine, mainConfig), true) {
		fmt.Printf("Skipped. Add this line to %s to enable the host:\n  %s\n", mainConfig, includeLine)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(mainConfig), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(mainConfig, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	content, _ := os.ReadFile(mainConfig)
	prefix := ""
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		prefix = "\n"
	}
	if _, err := f.WriteString(prefix + includeLine + "\n"); err != nil {
		return err
	}
	fmt.Printf("Appended `%s` to %s\n", includeLine, mainConfig)
	return nil
}

// sshConfigHasInclude reports whether path contains an Include directive
// covering the mycolab ssh config. A missing file counts as not included.
func sshConfigHasInclude(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		if i := strings.Index(line, "#"); i != -1 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Include") {
			continue
		}
		for _, pattern := range fields[1:] {
			if strings.Contains(pattern, "colab_config") {
				return true, nil
			}
		}
	}
	return false, nil
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
