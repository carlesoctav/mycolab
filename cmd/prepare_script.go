package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func renderPrepareScript() string {
	tmuxB64 := base64.StdEncoding.EncodeToString([]byte(tmuxConf))
	terminfoB64 := base64.StdEncoding.EncodeToString([]byte(terminfoPayload()))

	var sb strings.Builder
	sb.WriteString(`set -e

with_podman=false
skip_tmux=false
skip_terminfo=false
skip_hosts=false
skip_tools=false

for arg in "$@"; do
  case "$arg" in
    --podman)
      with_podman=true
      ;;
    --no-tmux-sync)
      skip_tmux=true
      ;;
    --no-terminfo)
      skip_terminfo=true
      ;;
    --no-hosts-fix)
      skip_hosts=true
      ;;
    --no-tools)
      skip_tools=true
      ;;
  esac
done

if [ "$skip_hosts" != "true" ]; then
    python3 -c '
import os
path = "/etc/hosts"
bak = "/etc/hosts.bak.mycolab"
try:
    with open(path, "r") as f:
        content = f.read()
    lines = content.splitlines()
    has_v4 = False
    changed = False
    out = []
    for line in lines:
        code, _, comment = line.partition("#")
        parts = code.split()
        if not parts:
            out.append(line)
            continue
        if parts[0] == "::1":
            kept = [parts[0]] + [p for p in parts[1:] if p != "localhost"]
            if len(kept) == 1:
                kept.extend(["ip6-localhost", "ip6-loopback"])
            if len(kept) != len(parts):
                changed = True
                new_code = " ".join(kept)
                line = (new_code + (" #" + comment if comment else "")).rstrip()
        elif parts[0] == "127.0.0.1":
            has_v4 = True
            if "localhost" not in parts[1:]:
                changed = True
                new_code = code.rstrip() + " localhost"
                line = (new_code + (" #" + comment if comment else "")).rstrip()
        out.append(line)
    if not has_v4:
        out.insert(0, "127.0.0.1 localhost")
        changed = True
    if changed:
        if not os.path.exists(bak):
            with open(bak, "w") as f:
                f.write(content)
        with open(path, "w") as f:
            f.write("\n".join(out) + "\n")
except Exception:
    pass
'
fi

if [ "$skip_tmux" != "true" ]; then
    echo '` + tmuxB64 + `' | base64 -d > /root/.tmux.conf
fi

if [ "$skip_terminfo" != "true" ]; then
    mkdir -p ~/.terminfo
    echo '` + terminfoB64 + `' | base64 -d | tic -x - 2>/dev/null || true
fi

need_base=false
if [ "$skip_tools" != "true" ]; then
    if ! (command -v rg && command -v jq && command -v nvim && (command -v fd || command -v fdfind) && command -v mount.nfs && command -v tmux) >/dev/null 2>&1; then
        need_base=true
    fi
fi

need_podman=false
if [ "$with_podman" = "true" ]; then
    if ! (command -v podman-colab >/dev/null 2>&1 && command -v podman >/dev/null 2>&1); then
        need_podman=true
    fi
fi

if [ "$need_base" = "true" ] || [ "$need_podman" = "true" ]; then
    export DEBIAN_FRONTEND=noninteractive
    APT_PKGS=""
    if [ "$need_base" = "true" ]; then
        APT_PKGS="$APT_PKGS curl fd-find ripgrep jq nfs-common tmux libfuse2"
    fi
    if [ "$need_podman" = "true" ]; then
        APT_PKGS="$APT_PKGS podman fuse-overlayfs slirp4netns uidmap conmon crun iptables"
    fi
    apt-get update -qq && apt-get install -y -qq $APT_PKGS
fi

if [ "$need_base" = "true" ]; then
    curl -fsSL -o /usr/local/bin/nvim ` + nvimAppImageURL + `
    chmod +x /usr/local/bin/nvim
    (command -v fd >/dev/null 2>&1 || ln -sf "$(command -v fdfind)" /usr/local/bin/fd)
fi

if [ "$with_podman" = "true" ]; then
    if [ "$need_podman" = "true" ]; then
        mkdir -p /dev/net
        if [ ! -e /dev/net/tun ]; then
            mknod /dev/net/tun c 10 200
            chmod 666 /dev/net/tun
        fi

        id colab &>/dev/null || useradd -m -s /bin/bash colab
        grep -q '^colab:100000:' /etc/subuid || echo 'colab:100000:65536' >> /etc/subuid
        grep -q '^colab:100000:' /etc/subgid || echo 'colab:100000:65536' >> /etc/subgid
        mkdir -p /run/user/1001 && chown colab:colab /run/user/1001 && chmod 700 /run/user/1001

        cat > /usr/local/bin/crun-noprocpatch <<'EOF'
#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
bundle = None
for i, a in enumerate(args):
    if a == "--bundle" and i + 1 < len(args): bundle = args[i + 1]
    elif a.startswith("--bundle="): bundle = a.split("=", 1)[1]
if bundle:
    p = os.path.join(bundle, "config.json")
    if os.path.exists(p):
        try:
            cfg = json.load(open(p)); changed = False
            for m in cfg.get("mounts", []):
                d = m.get("destination", "")
                if d == "/proc" and m.get("type") == "proc":
                    m.clear(); m.update({"destination": "/proc", "type": "bind", "source": "/proc", "options": ["rbind", "nosuid", "nodev", "noexec", "relatime"]}); changed = True
                elif d == "/sys" and m.get("type") == "sysfs":
                    m.clear(); m.update({"destination": "/sys", "type": "bind", "source": "/sys", "options": ["rbind", "ro", "nosuid", "nodev", "noexec", "relatime"]}); changed = True
            if changed: json.dump(cfg, open(p, "w"))
        except Exception: pass
os.execv("/usr/bin/crun", ["crun"] + args)
EOF
        chmod +x /usr/local/bin/crun-noprocpatch

        su - colab -c 'mkdir -p ~/.config/containers && printf "[containers]\ncgroups = \"disabled\"\ndefault_sysctls = []\n[engine]\nevents_logger = \"file\"\nruntime = \"crun\"\n[engine.runtimes]\ncrun = [\"/usr/local/bin/crun-noprocpatch\"]\n" > ~/.config/containers/containers.conf && printf "[storage]\ndriver = \"vfs\"\n" > ~/.config/containers/storage.conf'

        cat > /usr/local/bin/podman-colab <<'EOF'
#!/bin/bash
printf -v args '%q ' "$@"
exec su - colab -c "export XDG_RUNTIME_DIR=/run/user/1001; exec /usr/bin/podman $args"
EOF
        chmod +x /usr/local/bin/podman-colab
        ln -sf /usr/local/bin/podman-colab /usr/local/bin/podman
    fi
fi
`)
	return sb.String()
}

var runPrepareScript = func(session, script string, args ...string) ([]byte, error) {
	sshBin, err := findSSH()
	if err != nil {
		return nil, fmt.Errorf("ssh binary not found in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var cmdArgs []string
	if alt := os.Getenv("MYCOLAB_SSH_CONFIG"); alt != "" {
		cmdArgs = append(cmdArgs, "-F", alt)
	}
	cmdArgs = append(cmdArgs, session, "bash -s --")
	cmdArgs = append(cmdArgs, args...)
	c := exec.CommandContext(ctx, sshBin, cmdArgs...)
	c.Stdin = strings.NewReader(script)
	return c.CombinedOutput()
}
