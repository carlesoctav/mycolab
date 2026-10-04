package cmd

import (
	"bufio"
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

// sshCmd is hidden: 'mycolab new' is the entry point, but the server runs
// this same binary and 'server new' delegates to remote 'mycolab ssh',
// so the command must stay callable.
var sshCmd = &cobra.Command{
	Use:    "ssh",
	Hidden: true,
	Short:  "Point the '<session>' SSH host at one of the active profile's sessions",
	Long: `Write a 'Host <session>' entry for a session into ~/.ssh/colab_config.

The session comes from the '-s/--session' flag and becomes the SSH
hostname, so each session gets its own entry and several sessions can be
used side by side:

    mycolab ssh -s trainer   # then: ssh trainer
    mycolab ssh -s eval      # then: ssh eval

Re-running for the same session updates its entry in place. Managed
entries whose session exists in no profile are pruned as inactive
(--no-prune skips this); foreign entries are always preserved.
A legacy single-host 'Host colab' entry from older mycolab
versions is removed (its multiplex master, if any, is closed).

The entry uses 'colab ssh --proxy-mode' as its ProxyCommand and follows the
active mycolab profile. It is kept clean (no RemoteCommand) so editors can
run their own remote commands. Multiplexing (ControlMaster auto) is enabled
because Colab allows a single concurrent proxy connection: shells, rsync,
and lsyncd share it instead of tripping HTTP 429 against each other.
The bundled tmux.conf is also pushed to /root/.tmux.conf on the runtime
(best-effort; --no-tmux-sync skips this), the runtime's kernel env is
installed into sshd so ssh sessions see the same accelerators as the
console (--no-env-sync skips this), localhost is pinned to IPv4 in
/etc/hosts (--no-hosts-fix skips this), and base CLI tools (fd, rg, jq,
nvim) are installed via apt (--no-tools skips this). Re-running after switching
profiles closes the stale multiplex master so the next connect dials the
new runtime.
Your main ~/.ssh/config must contain
'Include ~/.ssh/colab_config' in global scope, before any Host block (this
command offers to add it); afterwards connect with 'ssh <session>'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName, err := requireSession(cmd)
		if err != nil {
			return err
		}
		return runSSHSetup(cmd, sessionName)
	},
}

// runSSHSetup writes the Host entry for sessionName and runs the runtime
// push steps. 'mycolab new' runs this after creating the session; the same
// helper serves the server-side wiring.
func runSSHSetup(cmd *cobra.Command, sessionName string) error {
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
	known := sessionExists(sessions, sessionName)
	if !known {
		fmt.Printf("Note: session %q is not in profile %q yet; "+
			"`colab ssh` will auto-create it on first connect.\n", sessionName, current)
	}

	configPath, err := profile.ColabSSHConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	prevContent := ""
	if prev, err := os.ReadFile(configPath); err == nil {
		prevContent = string(prev)
	}
	updated, prevProfile, removedLegacy := upsertSessionHost(prevContent, current, sessionName)
	if noPrune, _ := cmd.Flags().GetBool("no-prune"); !noPrune {
		keep := map[string]bool{sessionName: true}
		for _, s := range sessions {
			keep[s.Name] = true
		}
		if profiles, err := profile.List(); err == nil {
			for _, p := range profiles {
				if p == current {
					continue
				}
				if ss, err := profile.Sessions(p); err == nil {
					for _, s := range ss {
						keep[s.Name] = true
					}
				}
			}
		}
		var pruned []string
		updated, pruned = pruneStaleHosts(updated, keep)
		for _, h := range pruned {
			fmt.Printf("Removed inactive Host %s (no such session in any profile).\n", h)
			if dropMaster(h) {
				fmt.Printf("Closed the stale multiplex master for %q.\n", h)
			}
		}
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		return err
	}
	fmt.Printf("Wrote Host %s (session %q) to %s\n", sessionName, sessionName, configPath)
	if removedLegacy {
		fmt.Println("Removed legacy 'Host colab' entry; connect with `ssh <session>` from now on.")
		if dropMaster("colab") {
			fmt.Println("Closed the old multiplex master for `colab`.")
		}
	}
	if prevProfile != "" && prevProfile != current {
		fmt.Printf("Profile changed (%s -> %s) for %q.\n", prevProfile, current, sessionName)
		if dropMaster(sessionName) {
			fmt.Printf("Closed the stale multiplex master for %q.\n", sessionName)
		}
	}

	if err := ensureSSHInclude(); err != nil {
		return err
	}
	if err := verifyManagedHost(sessionName, sessionName); err != nil {
		return err
	}
	if noTmux, _ := cmd.Flags().GetBool("no-tmux-sync"); !noTmux {
		pushTmuxConf(sessionName, known)
	}
	if noEnv, _ := cmd.Flags().GetBool("no-env-sync"); !noEnv {
		syncRuntimeEnv(sessionName, known)
	}
	if noHosts, _ := cmd.Flags().GetBool("no-hosts-fix"); !noHosts {
		pushLocalhostFix(sessionName, known)
	}
	if noTools, _ := cmd.Flags().GetBool("no-tools"); !noTools {
		pushBaseTools(sessionName, known)
	}
	fmt.Printf("Connect with `ssh %s`.\n", sessionName)
	return nil
}

// managedFileHeader marks the top of the managed file. One Host block per
// session follows; each 'mycolab new -s <session>' run replaces only its own
// block and preserves the rest.
const managedFileHeader = "# Managed by mycolab — one 'Host <session>' block per session, upserted by 'mycolab new -s <session>'."

// colabHostBlock renders the managed 'Host <session>' entry. Options mirror
// colab's own ssh invocation (_ssh_base_args plus target
// root@colab-runtime): the bridge needs User root and disabled host-key
// checking. Agent forwarding stays on so keys held locally work on the
// runtime. Multiplexing is on because Colab allows only one concurrent
// proxy connection per runtime: the first connection becomes the master
// and later ones (interactive shells, rsync, lsyncd) share it as extra
// channels instead of fighting over the slot with HTTP 429. Liveness pings
// stay on because the ProxyCommand bridge is silent when idle and middleboxes
// (NAT, LB) reap silent websockets: ServerAliveInterval sends a small
// encrypted ping every 60s (also steady tunnel traffic), and
// ServerAliveCountMax drops a truly dead peer within ~3 minutes instead of
// hanging until the next write fails with 'broken pipe'.
func colabHostBlock(profile, session string) string {
	return fmt.Sprintf(`# Profile: %s | Session: %s
Host %s
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
    ServerAliveInterval 60
    ServerAliveCountMax 3
`, profile, session, session, session)
}

// upsertSessionHost returns content with the Host block for session added
// or replaced, preserving every other block. A legacy managed 'Host colab'
// block (written by older mycolab versions, pinned to a single session)
// is removed unless the session itself is named colab. prevProfile is the
// profile recorded on the replaced block ("" when the host is new).
func upsertSessionHost(content, profile, session string) (updated, prevProfile string, removedLegacy bool) {
	preamble, blocks := splitHostBlocks(content)
	kept := make([]string, 0, len(blocks)+1)
	replaced := false
	for _, b := range blocks {
		hosts := hostNames(b)
		switch {
		case containsHost(hosts, session):
			prevProfile = managedProfileName(b)
			kept = append(kept, colabHostBlock(profile, session))
			replaced = true
		case session != "colab" && containsHost(hosts, "colab") && strings.Contains(b, "--proxy-mode"):
			removedLegacy = true
		default:
			kept = append(kept, b)
		}
	}
	if !replaced {
		kept = append(kept, colabHostBlock(profile, session))
	}
	var sb strings.Builder
	sb.WriteString(managedFileHeader + "\n")
	for _, line := range preamble {
		if isManagedComment(line) {
			continue
		}
		sb.WriteString(line + "\n")
	}
	for _, b := range kept {
		sb.WriteString(b)
	}
	return sb.String(), prevProfile, removedLegacy
}

// pruneStaleHosts removes managed Host blocks (those containing
// '--proxy-mode') whose host names are all absent from keep, and returns
// the removed host names. Foreign blocks are always preserved. When
// nothing is removed the content is returned byte-identical.
func pruneStaleHosts(content string, keep map[string]bool) (updated string, removed []string) {
	preamble, blocks := splitHostBlocks(content)
	if len(blocks) == 0 {
		return content, nil
	}
	kept := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if !strings.Contains(b, "--proxy-mode") {
			kept = append(kept, b)
			continue
		}
		alive := false
		for _, h := range hostNames(b) {
			if keep[h] {
				alive = true
				break
			}
		}
		if alive {
			kept = append(kept, b)
			continue
		}
		removed = append(removed, hostNames(b)...)
	}
	if len(removed) == 0 {
		return content, nil
	}
	var sb strings.Builder
	sb.WriteString(managedFileHeader + "\n")
	for _, line := range preamble {
		if isManagedComment(line) {
			continue
		}
		sb.WriteString(line + "\n")
	}
	for _, b := range kept {
		sb.WriteString(b)
	}
	return sb.String(), removed
}

// splitHostBlocks splits ssh config text into the preamble (lines before
// the first Host directive) and one raw text chunk per Host block. A
// block starts at its Host line plus any directly attached managed
// comment lines above it (the per-block '# Profile:' tag); foreign text
// stays with the preamble or the preceding block, byte-exact.
func splitHostBlocks(content string) (preamble []string, blocks []string) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var starts []int
	for i, line := range lines {
		if !isHostLine(line) {
			continue
		}
		start := i
		// Only the per-block '# Profile:' tag belongs to the block; the
		// file-level header stays in the preamble so it is written once.
		for start > 0 && isProfileTag(lines[start-1]) {
			start--
		}
		starts = append(starts, start)
	}
	if len(starts) == 0 {
		return lines, nil
	}
	preamble = lines[:starts[0]]
	for i, s := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		blocks = append(blocks, strings.Join(lines[s:end], "\n")+"\n")
	}
	return preamble, blocks
}

// isHostLine reports whether line opens an ssh Host block.
func isHostLine(line string) bool {
	code := line
	if i := strings.Index(code, "#"); i != -1 {
		code = code[:i]
	}
	fields := strings.Fields(code)
	return len(fields) > 0 && strings.EqualFold(fields[0], "host")
}

// hostNames returns the patterns on a block's Host line.
func hostNames(block string) []string {
	for _, line := range strings.Split(block, "\n") {
		code := line
		if i := strings.Index(code, "#"); i != -1 {
			code = code[:i]
		}
		fields := strings.Fields(code)
		if len(fields) > 0 && strings.EqualFold(fields[0], "host") {
			return fields[1:]
		}
	}
	return nil
}

func containsHost(hosts []string, name string) bool {
	for _, h := range hosts {
		if h == name {
			return true
		}
	}
	return false
}

// isManagedComment reports whether line is a mycolab-managed comment (the
// file header or a per-block profile tag) as opposed to foreign text.
func isManagedComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Contains(trimmed, "Managed by mycolab") || isProfileTag(line)
}

// isProfileTag reports whether line is a per-block '# Profile:' tag.
func isProfileTag(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "# Profile:")
}

// managedProfileName extracts the profile from a block's
// '# Profile: <name> | Session: ...' tag ("" when absent).
func managedProfileName(block string) string {
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "# Profile:")
		if !ok {
			continue
		}
		if name, _, _ := strings.Cut(rest, "|"); strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// validateSessionHost rejects session names that would break the generated
// ssh config or ProxyCommand: whitespace and '#' split or comment the Host
// line, '*?!' are Host patterns/negation, quotes and backslashes confuse
// parsing, and a leading '-' would parse as a flag in both `ssh` and the
// ProxyCommand's '-s <session>' argument.
func validateSessionHost(name string) error {
	if name == "" {
		return fmt.Errorf("session name must not be empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid session name %q: must not start with '-'", name)
	}
	if strings.ContainsAny(name, " \t\r\n#*?!'\"\\") {
		return fmt.Errorf("invalid session name %q: must not contain whitespace or any of #*?!'\"\\", name)
	}
	return nil
}

// dropMaster closes the multiplex master for host, if one is running.
// Best-effort: it reports whether a master was actually closed and stays
// quiet when none is running.
func dropMaster(host string) bool {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return false
	}
	if err := exec.Command(sshBin, "-O", "check", host).Run(); err != nil {
		return false
	}
	return exec.Command(sshBin, "-O", "exit", host).Run() == nil
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

// ensureSSHInclude makes sure the main ~/.ssh/config includes the
// mycolab-managed file from global scope (an Include inside a Host block is
// conditional and would not apply to the managed hosts).
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

// verifyManagedHost checks via `ssh -G` that the managed host actually
// resolves to the managed ProxyCommand. A missing ssh binary skips the check.
// MYCOLAB_SSH_CONFIG overrides the config file under test (ssh ignores $HOME
// when locating its own config, so tests point it at a fixture with -F).
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
		return fmt.Errorf("'ssh %s' is shadowed by another config block (effective ProxyCommand: %s). Remove the conflicting 'Host %s' block and re-run `mycolab new -s %s`", host, proxy, host, host)
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
	bindSSHSetupFlags(sshCmd)
	rootCmd.AddCommand(sshCmd)
}

// sshSetupFlagNames lists the opt-out flags bound by bindSSHSetupFlags.
var sshSetupFlagNames = []string{"no-prune", "no-tmux-sync", "no-env-sync", "no-hosts-fix", "no-tools"}

// bindSSHSetupFlags registers the ssh-setup opt-out flags on c. The same
// set is offered by 'mycolab new' (which runs the ssh setup after creating
// the session) and by 'server new' (forwarded to the server-side setup).
func bindSSHSetupFlags(c *cobra.Command) {
	c.Flags().Bool("no-prune", false, "skip removing managed Host entries whose session exists in no profile")
	c.Flags().Bool("no-tmux-sync", false, "skip pushing the bundled tmux.conf to "+tmuxConfRemotePath)
	c.Flags().Bool("no-env-sync", false, "skip syncing the runtime kernel env into sshd for ssh sessions")
	c.Flags().Bool("no-hosts-fix", false, "skip pinning localhost to IPv4 in the runtime /etc/hosts")
	c.Flags().Bool("no-tools", false, "skip installing base CLI tools (fd, rg, jq, nvim) on the runtime")
}
