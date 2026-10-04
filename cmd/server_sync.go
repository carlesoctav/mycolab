package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

// Server state sync: the laptop's profiles are the source of truth and the
// server holds mirror profiles (same names, no user-facing management).
// Every server op pushes the active profile's token, runs against the
// mirror, then pulls merged sessions back. Multi-account support falls out
// of this: `mycolab use <profile>` selects which token gets pushed, so the
// server always acts as the active account.

// remoteSessionsFile returns the server-side mirror sessions path for home
// (absolute: remote shells cannot be trusted to expand ~ everywhere).
func remoteSessionsFile(home, name string) string {
	return home + "/.config/mycolab/" + name + ".json"
}

// remoteTokenFile returns the server-side mirror token path.
func remoteTokenFile(home, name string) string {
	return home + "/.config/mycolab/" + name + ".token.json"
}

// remoteHomeCache memoizes remote homes per server for the process: one CLI
// run targets one account per server.
var remoteHomeCache = map[string]string{}

// cachedRemoteHome returns the server account's home directory.
func cachedRemoteHome(server string) (string, error) {
	if home, ok := remoteHomeCache[server]; ok {
		return home, nil
	}
	out, err := runOnServerOutput(server, []string{"sh", "-c", `printf %s "$HOME"`})
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(out)
	if home == "" || !strings.HasPrefix(home, "/") {
		return "", fmt.Errorf("server %q: cannot determine home directory (got %q)", server, out)
	}
	remoteHomeCache[server] = home
	return home, nil
}

// requireActiveProfile returns the active local profile: the account every
// server op syncs and acts as.
func requireActiveProfile() (string, error) {
	current, err := profile.GetCurrent()
	if err != nil {
		return "", err
	}
	if current == "" {
		return "", fmt.Errorf("no active profile (use `mycolab use <profile_name>` first)")
	}
	return current, nil
}

// ensureServerProfile makes the mirror profile exist and active on the
// server: `mycolab use <name>` when present, else `mycolab add <name>` +
// `use <name>`. A pre-existing slot sessions file is backed up once
// (never overwritten) before any op touches it. The name is embedded
// bare inside double quotes: profile names are charset-validated, so no
// metacharacters can smuggle through.
func ensureServerProfile(server, name string) error {
	script := fmt.Sprintf("cp -n \"$HOME/.config/mycolab/%s.json\" \"$HOME/.config/mycolab/%s.json.server-bak\" 2>/dev/null; "+
		"mycolab use %s 2>/dev/null || { mycolab add %s && mycolab use %s; }",
		name, name, shellQuote(name), shellQuote(name), shellQuote(name))
	if _, err := runOnServerOutput(server, []string{"sh", "-c", script}); err != nil {
		return fmt.Errorf("prepare server profile %q: %w", name, err)
	}
	return nil
}

// pushServerState ensures the mirror profile on the server and pushes the
// active profile's token into it. Sessions are never pushed: the server
// file is written by server-side colab ops and merged back on pull.
func pushServerState(server, name string) error {
	if err := ensureServerProfile(server, name); err != nil {
		return err
	}
	tokenPath, err := profile.TokenPath(name)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(tokenPath)
	if err != nil || len(bytes.TrimSpace(content)) == 0 {
		return fmt.Errorf("profile %q is not logged in yet (run any `colab ...` command locally to log in)", name)
	}
	home, err := cachedRemoteHome(server)
	if err != nil {
		return err
	}
	return pushRemoteFile(server, remoteTokenFile(home, name), content)
}

// pushRemoteFile writes content to remotePath on server via stdin.
func pushRemoteFile(server, remotePath string, content []byte) error {
	sshBin, err := findSSH()
	if err != nil {
		return fmt.Errorf("ssh binary not found in PATH")
	}
	script := fmt.Sprintf("cat > %s", shellQuote(remotePath))
	c := exec.Command(sshBin, serverSSHArgs(server, []string{"sh", "-c", script}, false)...)
	c.Stdin = bytes.NewReader(content)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("push to server %q failed: %w: %s", server, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// pullServerSessions fetches the mirror profile's sessions file and merges
// it into the local profile. A missing server file (nothing created yet)
// is a no-op.
func pullServerSessions(server, name string) error {
	home, err := cachedRemoteHome(server)
	if err != nil {
		return err
	}
	out, err := runOnServerOutput(server, []string{"cat", remoteSessionsFile(home, name)})
	if err != nil {
		if strings.Contains(err.Error(), "No such file") {
			return nil
		}
		return err
	}
	localPath, err := profile.ProfilePath(name)
	if err != nil {
		return err
	}
	local, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	merged, stats, err := mergeSessions(local, []byte(out))
	if err != nil {
		return err
	}
	if len(stats.moved) > 0 {
		backup := localPath + ".mycolab-sync.bak"
		if err := os.WriteFile(backup, local, 0o644); err != nil {
			return err
		}
		fmt.Printf("Note: server sessions %s moved to a different runtime; previous local file backed up to %s\n",
			strings.Join(stats.moved, ", "), backup)
	}
	if err := os.WriteFile(localPath, merged, 0o644); err != nil {
		return err
	}
	for _, s := range stats.added {
		fmt.Printf("Synced new session %q from server.\n", s)
	}
	return nil
}

// mergeStats reports what a merge changed: added names, and moved names
// (same session, different endpoint — the local entry tracked another
// runtime). Same-endpoint refreshes apply silently.
type mergeStats struct {
	added []string
	moved []string
}

// mergeSessions merges the server slot's sessions into the local profile
// file. Server entries win by name; local-only entries are preserved; the
// machine-local keep_alive_pid is stripped from incoming entries.
func mergeSessions(local, remote []byte) ([]byte, mergeStats, error) {
	var stats mergeStats
	localMap, err := decodeSessions(local)
	if err != nil {
		return nil, stats, fmt.Errorf("local: %w", err)
	}
	remoteMap, err := decodeSessions(remote)
	if err != nil {
		return nil, stats, fmt.Errorf("server: %w", err)
	}
	for name, entry := range remoteMap {
		entry, err = stripKeepAlivePID(entry)
		if err != nil {
			return nil, stats, fmt.Errorf("server session %q: %w", name, err)
		}
		old, ok := localMap[name]
		if !ok {
			stats.added = append(stats.added, name)
		} else if sessionEndpoint(old) != sessionEndpoint(entry) {
			stats.moved = append(stats.moved, name)
		}
		localMap[name] = entry
	}
	sort.Strings(stats.added)
	sort.Strings(stats.moved)
	merged, err := encodeSessions(localMap)
	if err != nil {
		return nil, stats, err
	}
	return merged, stats, nil
}

// decodeSessions parses a sessions file (blank input means no sessions).
func decodeSessions(raw []byte) (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("sessions file is not valid JSON: %w", err)
	}
	return m, nil
}

// encodeSessions renders sessions in colab's 2-space indented format.
func encodeSessions(m map[string]json.RawMessage) ([]byte, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// stripKeepAlivePID drops the machine-local keep-alive pid from a session
// entry: a pid from another machine is meaningless and must never be
// trusted (or signaled) here.
func stripKeepAlivePID(entry json.RawMessage) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(entry, &m); err != nil {
		return nil, fmt.Errorf("session entry is not valid JSON: %w", err)
	}
	delete(m, "keep_alive_pid")
	return json.Marshal(m)
}

// sessionEndpoint extracts an entry's endpoint ("" when absent).
func sessionEndpoint(entry json.RawMessage) string {
	var v struct {
		Endpoint string `json:"endpoint"`
	}
	if json.Unmarshal(entry, &v) != nil {
		return ""
	}
	return v.Endpoint
}

// runLocalWiring runs the local Host-entry wiring for session with all
// runtime prep skipped: prep already ran on the server against the same
// runtime, so this only writes/refreshes the direct local entry.
func runLocalWiring(session string) error {
	cmd := &cobra.Command{Use: "server-wiring"}
	bindSSHSetupFlags(cmd)
	for _, f := range sshSetupFlagNames {
		_ = cmd.Flags().Set(f, "true")
	}
	return runSSHSetup(cmd, session)
}
