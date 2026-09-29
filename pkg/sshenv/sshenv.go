// Package sshenv captures a Colab runtime's kernel environment and renders
// it as an OpenSSH sshd SetEnv block, so `ssh` sessions (which the
// runtime's sshd spawns with a near-empty env) see the same accelerators
// and tools as `colab console` / `colab exec`.
package sshenv

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

// deniedExact lists variables that must never be forced onto ssh sessions:
// managed by sshd or the shell per-session, terminal-specific, or shell
// startup indirection (BASH_ENV/ENV would make non-interactive shells
// source startup files and could corrupt scp/rsync-style channels).
var deniedExact = map[string]bool{
	"SHLVL": true, "_": true, "PWD": true, "OLDPWD": true,
	"SHELL": true, "HOME": true, "USER": true, "LOGNAME": true, "MAIL": true,
	"TERM": true, "TERMCAP": true,
	"ENV": true, "BASH_ENV": true, "CDPATH": true, "IFS": true,
	"PS1": true, "PS2": true, "LS_COLORS": true,
	"TMUX": true, "TMUX_PANE": true, "STY": true, "WINDOW": true,
	"TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true,
	"JPY_PARENT_PID": true, "_PYVIZ_COMMS_INSTALLED": true,
}

// Denied reports whether name must be excluded from the forced set.
// The SSH_* family is per-connection (notably SSH_AUTH_SOCK, whose path
// embeds the connection's own temp dir) and must never be pinned.
func Denied(name string) bool {
	if deniedExact[name] {
		return true
	}
	return strings.HasPrefix(name, "SSH_")
}

// ParseNulEnv parses NUL-separated KEY=VALUE records as produced by
// `env -0`. Malformed records (no '=') are skipped.
func ParseNulEnv(data []byte) map[string]string {
	vars := map[string]string{}
	for _, rec := range strings.Split(string(data), "\x00") {
		if rec == "" {
			continue
		}
		name, val, ok := strings.Cut(rec, "=")
		if !ok || name == "" {
			continue
		}
		vars[name] = val
	}
	return vars
}

// ExtractBase64 pulls a base64 payload out of `colab exec` output. Exec
// mixes `[colab]` notices (and remote stderr) into stdout, and
// occasionally renders rich spinners/panels instead of plain output, so
// only strict base64-alphabet lines are kept and the rest is dropped.
func ExtractBase64(output string) ([]byte, error) {
	var sb strings.Builder
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if isBase64Line(line) {
			sb.WriteString(line)
		}
	}
	if sb.Len() == 0 {
		return nil, fmt.Errorf("no base64 payload in capture")
	}
	raw, err := base64.StdEncoding.DecodeString(sb.String())
	if err != nil {
		return nil, fmt.Errorf("capture is not base64: %w", err)
	}
	return raw, nil
}

// DecodeCapture decodes `colab exec` output of `env -0 | base64 -w0`
// into a variable map.
func DecodeCapture(output string) (map[string]string, error) {
	raw, err := ExtractBase64(output)
	if err != nil {
		return nil, err
	}
	vars := ParseNulEnv(raw)
	if len(vars) == 0 {
		return nil, fmt.Errorf("capture decoded to zero variables")
	}
	return vars, nil
}

// RenderSetEnv renders vars as a single sshd `SetEnv` directive. sshd uses
// first-obtained value per keyword, so all vars must ride ONE directive —
// one directive per var would silently keep only the first. Values are
// double-quoted with backslash/quote escapes. Values containing newlines
// cannot be represented on one line and are skipped (returned).
func RenderSetEnv(vars map[string]string) (directive string, skipped []string) {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		val := vars[name]
		if strings.Contains(val, "\n") {
			skipped = append(skipped, name)
			continue
		}
		pairs = append(pairs, name+"="+quoteValue(val))
	}
	if len(pairs) == 0 {
		return "", skipped
	}
	return "SetEnv " + strings.Join(pairs, " "), skipped
}

// isBase64Line reports whether line is a plausible base64 payload line:
// non-empty, alphabet-only, padding only at the end.
func isBase64Line(line string) bool {
	if line == "" {
		return false
	}
	core := strings.TrimRight(line, "=")
	if core == "" || len(line)-len(core) > 2 {
		return false
	}
	for _, r := range core {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' ||
			r >= '0' && r <= '9' || r == '+' || r == '/') {
			return false
		}
	}
	return true
}

func quoteValue(val string) string {
	val = strings.ReplaceAll(val, "\\", "\\\\")
	val = strings.ReplaceAll(val, "\"", "\\\"")
	return "\"" + val + "\""
}

// InstallerMarker is echoed by InstallerScript on success; the caller
// looks for it in the (notice-mixed) exec output.
const InstallerMarker = "ENV_SYNC_OK"

// Remote staging paths used by the installer.
const (
	RemoteBlockPath     = "/tmp/mycolab_setenv_block"
	RemoteInstallerPath = "/tmp/mycolab_sshd_install.sh"
)

// InstallerScript installs a mycolab-managed SetEnv block into the
// runtime's sshd. It runs as root via `colab exec`: the previous managed
// block is removed, the new one appended, the config validated with
// `sshd -t` (rollback on failure), and the listener SIGHUPed so new
// connections pick it up. Established sessions are unaffected.
const InstallerScript = `#!/bin/bash
# Install mycolab-managed SetEnv block into the runtime sshd.
# Runs as root via ` + "`colab exec`" + `.
set -u
BLOCK="${1:-/tmp/mycolab_setenv_block}"
CONF=/etc/ssh/sshd_config
cp "$CONF" /tmp/sshd_config.bak.mycolab
python3 - "$CONF" <<'PYEOF'
import sys
path = sys.argv[1]
start, end = "# BEGIN mycolab-env", "# END mycolab-env"
out, skip = [], False
for line in open(path).read().split("\n"):
    if line.startswith(start):
        skip = True
        continue
    if line.startswith(end):
        skip = False
        continue
    if not skip:
        out.append(line)
open(path, "w").write("\n".join(out).rstrip("\n") + "\n")
PYEOF
{
  echo "# BEGIN mycolab-env (managed by mycolab; do not edit)"
  cat "$BLOCK"
  echo "# END mycolab-env"
} >> "$CONF"
if ! sshd -t -f "$CONF" 2>/tmp/sshd_t.err; then
  cp /tmp/sshd_config.bak.mycolab "$CONF"
  echo "INVALID CONFIG, rolled back:"
  cat /tmp/sshd_t.err
  exit 1
fi
N=$(pgrep -c -f 'sshd.*\[listener\]' || true)
if [ "$N" != "1" ]; then
  echo "found $N sshd listeners, expected 1; config written but NOT reloaded"
  exit 1
fi
pkill -HUP -f 'sshd.*\[listener\]'
echo ENV_SYNC_OK
`
