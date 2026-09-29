package cmd

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/carlesoctav/mycolab/pkg/sshenv"
)

func init() {
	sshCmd.Flags().Bool("no-hosts-fix", false, "skip pinning localhost to IPv4 in the runtime /etc/hosts")
}

// FixHostsLocalhostIPv4 rewrites hosts content so plain `localhost`
// resolves to 127.0.0.1: the `localhost` token is dropped from the ::1
// line (falling back to ip6-localhost/ip6-loopback when nothing else is
// left) and ensured on the 127.0.0.1 line (prepended when missing).
// Untouched lines — including comments — are preserved byte-exact.
// changed is false when the content already has the desired state.
func FixHostsLocalhostIPv4(content string) (fixed string, changed bool) {
	lines := strings.Split(content, "\n")
	hasV4 := false
	for i, line := range lines {
		code, comment := splitComment(line)
		fields := strings.Fields(code)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "::1":
			kept := fields[:1:1]
			for _, f := range fields[1:] {
				if f != "localhost" {
					kept = append(kept, f)
				}
			}
			if len(kept) == 1 {
				kept = append(kept, "ip6-localhost", "ip6-loopback")
			}
			if len(kept) != len(fields) {
				lines[i] = joinComment(strings.Join(kept, " "), comment)
				changed = true
			}
		case "127.0.0.1":
			hasV4 = true
			found := false
			for _, f := range fields[1:] {
				if f == "localhost" {
					found = true
					break
				}
			}
			if !found {
				lines[i] = joinComment(strings.TrimRight(code, " \t")+" localhost", comment)
				changed = true
			}
		}
	}
	if !hasV4 {
		lines = append([]string{"127.0.0.1 localhost"}, lines...)
		changed = true
	}
	return strings.Join(lines, "\n"), changed
}

func splitComment(line string) (code, comment string) {
	if i := strings.Index(line, "#"); i != -1 {
		return line[:i], line[i:]
	}
	return line, ""
}

func joinComment(code, comment string) string {
	if comment == "" {
		return strings.TrimRight(code, " \t")
	}
	return strings.TrimRight(code, " \t") + " " + comment
}

// pushLocalhostFix pins the runtime's localhost to IPv4 by fixing
// /etc/hosts (a backup is kept at /etc/hosts.bak.mycolab). Some runtimes
// list localhost under ::1 first, and clients that only try the first
// resolver result then hang against nothing. Best-effort like the other
// `mycolab ssh` push steps.
func pushLocalhostFix(session string, sessionKnown bool) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping localhost fix (re-run `mycolab ssh -s %s` once it does).\n", session, session)
		return
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		fmt.Println("Note: skipping localhost fix (colab binary not found).")
		return
	}
	var current []byte
	for i := 0; i < colabExecAttempts; i++ {
		if i > 0 {
			time.Sleep(colabExecRetryDelay)
		}
		var out string
		out, err = runColab(colabBin, "!cat /etc/hosts | base64 -w0\n", "exec", "-s", session)
		if err != nil {
			continue
		}
		var raw []byte
		raw, err = sshenv.ExtractBase64(out)
		if err == nil && strings.Contains(string(raw), "127.0.0.1") {
			current = raw
			break
		}
		err = fmt.Errorf("hosts read unusable")
	}
	if current == nil {
		fmt.Printf("Note: localhost fix skipped, cannot read /etc/hosts (%v).\n", err)
		return
	}
	fixed, changed := FixHostsLocalhostIPv4(string(current))
	if !changed {
		fmt.Println("localhost already resolves to IPv4 (no /etc/hosts change).")
		return
	}
	if _, err := runColabForMarker(colabBin,
		"!test -e /etc/hosts.bak.mycolab || cp /etc/hosts /etc/hosts.bak.mycolab; echo HOSTS_BAK_OK\n",
		"HOSTS_BAK_OK", "exec", "-s", session); err != nil {
		fmt.Printf("Note: localhost fix skipped, backup failed (%v).\n", err)
		return
	}
	enc := base64.StdEncoding.EncodeToString([]byte(fixed))
	if _, err := runColabForMarker(colabBin,
		fmt.Sprintf("!echo '%s' | base64 -d > /etc/hosts && echo HOSTS_OK\n", enc),
		"HOSTS_OK", "exec", "-s", session); err != nil {
		fmt.Printf("Note: localhost fix write failed (%v).\n", err)
		return
	}
	for i := 0; i < colabExecAttempts; i++ {
		if i > 0 {
			time.Sleep(colabExecRetryDelay)
		}
		if out, err := runColab(colabBin, "!getent hosts localhost\n", "exec", "-s", session); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "127.0.0.1") {
					fmt.Println("Pinned localhost to 127.0.0.1 in /etc/hosts (backup at /etc/hosts.bak.mycolab).")
					return
				}
			}
		}
	}
	fmt.Println("Note: localhost fix written but getent does not show 127.0.0.1; restore with `!cp /etc/hosts.bak.mycolab /etc/hosts` via `colab exec` if needed.")
}
