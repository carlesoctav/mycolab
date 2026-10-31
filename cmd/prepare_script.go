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
    if ! (command -v podman >/dev/null 2>&1 && [ -f /usr/local/bin/docker-compose ]); then
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
        APT_PKGS="$APT_PKGS podman fuse-overlayfs slirp4netns uidmap conmon crun iptables curl"
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
        mkdir -p /dev/net; [ -e /dev/net/tun ] || mknod /dev/net/tun c 10 200; chmod 666 /dev/net/tun
        chmod 666 /dev/fuse 2>/dev/null || true
        id colab &>/dev/null || useradd -m -s /bin/bash colab
        grep -q '^colab:100000:' /etc/subuid || echo 'colab:100000:65536' >> /etc/subuid
        grep -q '^colab:100000:' /etc/subgid || echo 'colab:100000:65536' >> /etc/subgid
        mkdir -p /run/user/1001/podman && chown -R colab:colab /run/user/1001 && chmod 700 /run/user/1001

        rm -f /usr/local/bin/crun-noprocpatch
        su - colab -c 'mkdir -p ~/.config/containers && printf "[containers]\ncgroups = \"enabled\"\ndefault_sysctls = []\nvolumes = [\"/proc:/proc\"]\n[engine]\nevents_logger = \"file\"\nruntime = \"crun\"\ncgroup_manager = \"cgroupfs\"\n" > ~/.config/containers/containers.conf && printf "[storage]\ndriver = \"overlay\"\n[storage.options.overlay]\nmount_program = \"/usr/bin/fuse-overlayfs\"\n" > ~/.config/containers/storage.conf'

        if command -v nvidia-ctk &>/dev/null && [ -d /usr/lib64-nvidia ]; then
            mkdir -p /etc/cdi
            nvidia-ctk cdi generate --library-search-path=/usr/lib64-nvidia --output=/etc/cdi/nvidia.yaml 2>/dev/null
            chmod 644 /etc/cdi/nvidia.yaml
        fi

        if [ ! -f /usr/local/bin/docker-compose ]; then
            curl -fsSL -o /usr/local/bin/docker-compose https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-x86_64
            chmod 755 /usr/local/bin/docker-compose
        fi

        pkill -f 'podman system service' 2>/dev/null || true
        rm -f /run/user/1001/podman/podman.sock
        runuser -u colab -- env XDG_RUNTIME_DIR=/run/user/1001 nohup /usr/bin/podman system service --time=0 unix:///run/user/1001/podman/podman.sock >/tmp/podman-service.log 2>&1 &

        chmod 755 /root

        cat > /usr/local/bin/podman <<'INNER_EOF'
#!/bin/bash
if [ "$(id -u)" -eq 0 ]; then
    chmod -R a+rX /tmp/tmp* 2>/dev/null || true
    export XDG_RUNTIME_DIR=/run/user/1001
    export HOME=/home/colab
    export USER=colab

    # Ensure host destination is writable by colab for podman cp
    for arg in "$@"; do
        if [ "$arg" = "cp" ]; then
            dest="${@: -1}"
            if [ "$dest" != "-" ] && [[ "$dest" != *:* ]]; then
                dest_dir="$dest"
                [ ! -d "$dest_dir" ] && dest_dir="$(dirname "$dest")"
                mkdir -p "$dest_dir" 2>/dev/null || true
                chown -R colab:colab "$dest_dir" 2>/dev/null || true
                chmod -R a+rwx "$dest_dir" 2>/dev/null || true
                [ -f "$dest" ] && chown colab:colab "$dest" 2>/dev/null || true
                [ -f "$dest" ] && chmod a+rw "$dest" 2>/dev/null || true
            fi
            break
        fi
    done

    # Auto-restart service if socket is missing or dead
    if [ ! -S /run/user/1001/podman/podman.sock ] || ! curl -s --unix-socket /run/user/1001/podman/podman.sock http://localhost/_ping &>/dev/null; then
        mkdir -p /run/user/1001/podman
        chown -R colab:colab /run/user/1001
        rm -f /run/user/1001/podman/podman.sock
        runuser -u colab -- env XDG_RUNTIME_DIR=/run/user/1001 nohup /usr/bin/podman system service --time=0 unix:///run/user/1001/podman/podman.sock >/tmp/podman-service.log 2>&1 &
        for i in {1..30}; do
            [ -S /run/user/1001/podman/podman.sock ] && break
            sleep 0.1
        done
        ln -sf /run/user/1001/podman/podman.sock /var/run/docker.sock
    fi

    exec runuser -u colab -- /usr/bin/podman "$@"
else
    exec /usr/bin/podman "$@"
fi
INNER_EOF
        chmod +x /usr/local/bin/podman
        if ! grep -q "docker.io" /etc/containers/registries.conf 2>/dev/null; then
            echo "unqualified-search-registries = [\"docker.io\"]" >> /etc/containers/registries.conf
        fi
        ln -sf /usr/local/bin/podman /usr/local/bin/docker
        ln -sf /run/user/1001/podman/podman.sock /var/run/docker.sock
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
