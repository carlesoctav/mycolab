package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// loadLocalContainersAuth checks ~/.config/containers/auth.json or ~/.docker/config.json
// on the local machine for container registry credentials.
var loadLocalContainersAuth = func() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, nil
	}
	candidates := []string{
		filepath.Join(home, ".config", "containers", "auth.json"),
		filepath.Join(home, ".docker", "config.json"),
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, nil
}

func renderPrepareScript() string {
	tmuxB64 := base64.StdEncoding.EncodeToString([]byte(tmuxConf))
	terminfoB64 := base64.StdEncoding.EncodeToString([]byte(terminfoPayload()))

	var authB64 string
	if authData, _ := loadLocalContainersAuth(); len(authData) > 0 {
		authB64 = base64.StdEncoding.EncodeToString(authData)
	}

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
    if ! (command -v podman >/dev/null 2>&1 && [ -f /usr/local/bin/docker-compose ] && [ -f /usr/local/bin/podman-mode ] && [ -f /usr/local/bin/crane ] && command -v just >/dev/null 2>&1); then
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
    rm -f /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*ppa* 2>/dev/null || true
    apt-get update -qq -o Acquire::http::Timeout=10 -o Acquire::https::Timeout=10 && apt-get install -y -qq $APT_PKGS
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
        su - colab -c 'mkdir -p ~/.config/containers && printf "[containers]\ncgroups = \"enabled\"\ndefault_sysctls = []\nvolumes = [\"/proc:/proc\"]\n[engine]\nevents_logger = \"file\"\nruntime = \"crun\"\ncgroup_manager = \"cgroupfs\"\ncompose_warning_logs = false\n" > ~/.config/containers/containers.conf'

        mkdir -p /etc/containers
        if [ ! -f /etc/containers/containers.conf ]; then
            printf "[engine]\ncompose_warning_logs = false\n" > /etc/containers/containers.conf
        elif grep -q "compose_warning_logs" /etc/containers/containers.conf; then
            sed -i "s/compose_warning_logs\s*=.*/compose_warning_logs = false/" /etc/containers/containers.conf
        elif grep -q '\[engine\]' /etc/containers/containers.conf; then
            sed -i '/\[engine\]/a compose_warning_logs = false' /etc/containers/containers.conf
        else
            printf "\n[engine]\ncompose_warning_logs = false\n" >> /etc/containers/containers.conf
        fi

        if command -v nvidia-ctk &>/dev/null && [ -d /usr/lib64-nvidia ]; then
            mkdir -p /etc/cdi
            nvidia-ctk cdi generate --library-search-path=/usr/lib64-nvidia --output=/etc/cdi/nvidia.yaml 2>/dev/null
            chmod 644 /etc/cdi/nvidia.yaml
        fi

        if [ ! -f /usr/local/bin/docker-compose ]; then
            curl -fsSL -o /usr/local/bin/docker-compose https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-x86_64
            chmod 755 /usr/local/bin/docker-compose
        fi

        if [ ! -f /usr/local/bin/crane ]; then
            curl -fsSL https://github.com/google/go-containerregistry/releases/latest/download/go-containerregistry_Linux_x86_64.tar.gz | tar -xz -C /usr/local/bin crane
            chmod 755 /usr/local/bin/crane
        fi

        if ! command -v just >/dev/null 2>&1; then
            (command -v uv >/dev/null 2>&1 || pip install -q uv 2>/dev/null || true)
            UV_TOOL_BIN_DIR=/usr/local/bin uv tool install rust-just 2>/dev/null || (uv tool install rust-just && ln -sf ~/.local/bin/just /usr/local/bin/just) 2>/dev/null || true
        fi

        chmod 755 /root

        cat > /usr/local/bin/podman-mode <<'MODE_EOF'
#!/bin/bash
set -e

if [ "$(id -u)" -ne 0 ]; then
    exec sudo "$0" "$@"
fi

MODE_FILE="/etc/podman-mode"
CONF_FILE="/home/colab/.config/containers/storage.conf"

stop_service() {
    start-stop-daemon --stop --oknodo --pidfile /run/user/1001/podman.pid --user colab --exec /usr/bin/podman 2>/dev/null || true
    pkill -9 -u colab -f 'podman system service' 2>/dev/null || true
    rm -f /run/user/1001/podman/podman.sock /run/user/1001/podman.pid /run/podman-service.lock
}

restart_service() {
    export XDG_RUNTIME_DIR=/run/user/1001
    export USER=colab
    export HOME=/home/colab
    mkdir -p /run/user/1001/podman
    chown -R colab:colab /run/user/1001
    chmod 700 /run/user/1001
    env XDG_RUNTIME_DIR=/run/user/1001 REGISTRY_AUTH_FILE=/run/user/1001/containers/auth.json USER=colab HOME=/home/colab \
        start-stop-daemon --start --oknodo \
            --background \
            --make-pidfile \
            --pidfile /run/user/1001/podman.pid \
            --chuid colab:colab \
            --user colab \
            --exec /usr/bin/podman \
            --output /tmp/podman-service.log \
            -- system service --time=0 unix:///run/user/1001/podman/podman.sock
    for i in {1..30}; do
        [ -S /run/user/1001/podman/podman.sock ] && break
        sleep 0.1
    done
    ln -sf /run/user/1001/podman/podman.sock /var/run/docker.sock
}

cmd="${1:-status}"

case "$cmd" in
    tmpfs)
        echo "Switching podman to tmpfs mode (hybrid: disk images + RAM native overlay)..."
        stop_service
        mount -o remount,size=80% /dev/shm 2>/dev/null || true
        mkdir -p /dev/shm/podman-storage
        chown -R colab:colab /dev/shm/podman-storage
        chmod 700 /dev/shm/podman-storage
        mkdir -p /home/colab/.local/share/containers/storage
        chown -R colab:colab /home/colab/.local/share/containers/storage
        chmod -R a+rX /home/colab/.local/share/containers/storage 2>/dev/null || true

        mkdir -p "$(dirname "$CONF_FILE")"
        cat > "$CONF_FILE" << 'CONF_EOF'
[storage]
driver = "overlay"
graphroot = "/dev/shm/podman-storage"
runroot = "/run/user/1001/containers"

[storage.options]
additionalimagestores = [
  "/home/colab/.local/share/containers/storage"
]

[storage.options.overlay]
mount_program = ""
CONF_EOF
        chown colab:colab "$CONF_FILE"
        echo "tmpfs" > "$MODE_FILE"
        restart_service
        echo "Successfully switched to tmpfs mode."
        ;;

    overlay)
        echo "Switching podman to overlay mode (disk storage with fuse-overlayfs)..."
        stop_service
        mkdir -p /home/colab/.local/share/containers/storage
        chown -R colab:colab /home/colab/.local/share/containers/storage

        mkdir -p "$(dirname "$CONF_FILE")"
        cat > "$CONF_FILE" << 'CONF_EOF'
[storage]
driver = "overlay"
graphroot = "/home/colab/.local/share/containers/storage"
runroot = "/run/user/1001/containers"

[storage.options]
additionalimagestores = []

[storage.options.overlay]
mount_program = "/usr/bin/fuse-overlayfs"
CONF_EOF
        chown colab:colab "$CONF_FILE"
        echo "overlay" > "$MODE_FILE"
        restart_service
        echo "Successfully switched to overlay mode."
        ;;

    status)
        current_mode="tmpfs"
        [ -f "$MODE_FILE" ] && current_mode="$(cat "$MODE_FILE")"
        echo "=== PODMAN MODE STATUS ==="
        echo "Current Mode: $current_mode"
        if [ "$current_mode" = "tmpfs" ]; then
            echo "Description:  Hybrid Mode (Images stored on 226 GB disk, run/build in RAM tmpfs with native kernel overlay)"
        else
            echo "Description:  Pure Disk Mode (All images, containers, and builds on disk using fuse-overlayfs)"
        fi
        echo ""
        echo "--- Storage Configuration ---"
        grep -E 'graphroot|mount_program|additionalimagestores' "$CONF_FILE" 2>/dev/null || cat "$CONF_FILE" 2>/dev/null || echo "No storage.conf"
        echo ""
        echo "--- Storage Usage ---"
        echo "tmpfs (/dev/shm/podman-storage):                 $(du -sh /dev/shm/podman-storage 2>/dev/null | cut -f1 || echo '0')"
        echo "disk  (/home/colab/.local/share/containers/storage): $(du -sh /home/colab/.local/share/containers/storage 2>/dev/null | cut -f1 || echo '0')"
        echo ""
        echo "--- Podman Info ---"
        runuser -u colab -- /usr/bin/podman info 2>/dev/null | grep -E 'Backing Filesystem|Native Overlay Diff' || echo 'Service not responding'
        ;;

    *)
        echo "Usage: podman-mode {tmpfs|overlay|status}"
        exit 1
        ;;
esac
MODE_EOF
        chmod +x /usr/local/bin/podman-mode

        cat > /usr/local/bin/podman <<'INNER_EOF'
#!/bin/bash
DISK_FLAGS=(
    --root /home/colab/.local/share/containers/storage
    --runroot /run/user/1001/containers-disk
    --storage-opt overlay.mount_program=/usr/bin/fuse-overlayfs
)

MODE="tmpfs"
[ -f /etc/podman-mode ] && MODE="$(cat /etc/podman-mode 2>/dev/null || echo tmpfs)"

parse_image() {
    local skip_next=0
    for arg in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            skip_next=0
            continue
        fi
        case "$arg" in
            -p|--publish|-v|--volume|-e|--env|--name|--network|--net|-u|--user|-w|--workdir|\
            --entrypoint|--restart|--cpus|--memory|-m|--shm-size|--hostname|-h|-l|--label|\
            --add-host|--device|--env-file|--cap-add|--cap-drop|--security-opt|--log-opt|\
            --ulimit|--ipc|--pid|--log-driver|--health-cmd|--gpus|--mount|--platform|--pull)
                skip_next=1
                ;;
            --*=*)
                ;;
            -*)
                ;;
            *)
                echo "$arg"
                return 0
                ;;
        esac
    done
    return 1
}

is_pull_or_load=false
is_run_or_create=false
run_args=()

if [ "$MODE" = "tmpfs" ]; then
    if [ "${1:-}" = "pull" ] || [ "${1:-}" = "load" ]; then
        is_pull_or_load=true
    elif [ "${1:-}" = "image" ] && { [ "${2:-}" = "pull" ] || [ "${2:-}" = "load" ]; }; then
        is_pull_or_load=true
    elif [ "${1:-}" = "run" ] || [ "${1:-}" = "create" ]; then
        is_run_or_create=true
        run_args=("${@:2}")
    elif [ "${1:-}" = "container" ] && { [ "${2:-}" = "run" ] || [ "${2:-}" = "create" ]; }; then
        is_run_or_create=true
        run_args=("${@:3}")
    fi
fi

run_as_colab() {
    if [ "$(id -u)" -eq 0 ]; then
        runuser -u colab -- "$@"
    else
        "$@"
    fi
}

if [ "$(id -u)" -eq 0 ]; then
    chmod -R a+rX /tmp/tmp* 2>/dev/null || true
    export XDG_RUNTIME_DIR=/run/user/1001
    export HOME=/home/colab
    export USER=colab
    export REGISTRY_AUTH_FILE=/run/user/1001/containers/auth.json

    if [ -f /run/user/1001/containers/auth.json ]; then
        mkdir -p /home/colab/.config/containers /home/colab/.docker /root/.docker
        cp /run/user/1001/containers/auth.json /home/colab/.config/containers/auth.json
        cp /run/user/1001/containers/auth.json /home/colab/.docker/config.json
        chown -R colab:colab /home/colab/.config/containers /home/colab/.docker
        chmod 600 /home/colab/.config/containers/auth.json /home/colab/.docker/config.json
        cp /run/user/1001/containers/auth.json /root/.docker/config.json
        chmod 755 /root/.docker
        chmod 644 /root/.docker/config.json
    fi

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
        mkdir -p /run/user/1001/podman /dev/shm/podman-storage
        chown -R colab:colab /run/user/1001 /dev/shm/podman-storage
        chmod 700 /run/user/1001 /dev/shm/podman-storage
        mount -o remount,size=80% /dev/shm 2>/dev/null || true
        (
            flock 200
            if [ ! -S /run/user/1001/podman/podman.sock ] || ! curl -s --unix-socket /run/user/1001/podman/podman.sock http://localhost/_ping &>/dev/null; then
                if ! start-stop-daemon --status --pidfile /run/user/1001/podman.pid --user colab --exec /usr/bin/podman 2>/dev/null; then
                    rm -f /run/user/1001/podman/podman.sock /run/user/1001/podman.pid
                fi
                env XDG_RUNTIME_DIR=/run/user/1001 REGISTRY_AUTH_FILE=/run/user/1001/containers/auth.json USER=colab HOME=/home/colab \
                    start-stop-daemon --start --oknodo \
                        --background \
                        --make-pidfile \
                        --pidfile /run/user/1001/podman.pid \
                        --chuid colab:colab \
                        --user colab \
                        --exec /usr/bin/podman \
                        --output /tmp/podman-service.log \
                        -- system service --time=0 unix:///run/user/1001/podman/podman.sock
                for i in {1..30}; do
                    [ -S /run/user/1001/podman/podman.sock ] && break
                    sleep 0.1
                done
                ln -sf /run/user/1001/podman/podman.sock /var/run/docker.sock
            fi
        ) 200>/run/podman-service.lock
    fi
fi

# Route pull / load directly to 226 GB disk store in tmpfs mode
if [ "$is_pull_or_load" = "true" ]; then
    run_as_colab /usr/bin/podman "${DISK_FLAGS[@]}" "$@"
    rc=$?
    chmod -R a+rX /home/colab/.local/share/containers/storage 2>/dev/null || true
    exit $rc
fi

# For run/create: if image not present locally, pre-pull to 226 GB disk
if [ "$is_run_or_create" = "true" ]; then
    img=$(parse_image "${run_args[@]}" || true)
    if [ -n "$img" ]; then
        if ! run_as_colab /usr/bin/podman image exists "$img" 2>/dev/null; then
            echo "[mycolab] Pre-pulling '$img' to 226 GB disk store..."
            run_as_colab /usr/bin/podman "${DISK_FLAGS[@]}" pull "$img" || true
            chmod -R a+rX /home/colab/.local/share/containers/storage 2>/dev/null || true
        fi
    fi
fi

if [ "$(id -u)" -eq 0 ]; then
    exec runuser -u colab -- /usr/bin/podman "$@"
else
    exec /usr/bin/podman "$@"
fi
INNER_EOF
        chmod +x /usr/local/bin/podman
        /usr/local/bin/podman-mode tmpfs
        if ! grep -q "docker.io" /etc/containers/registries.conf 2>/dev/null; then
            echo "unqualified-search-registries = [\"docker.io\"]" >> /etc/containers/registries.conf
        fi
        ln -sf /usr/local/bin/podman /usr/local/bin/docker
        ln -sf /run/user/1001/podman/podman.sock /var/run/docker.sock
    fi

    mkdir -p /run/user/1001/containers && chown colab:colab /run/user/1001/containers 2>/dev/null || true
    chmod 700 /run/user/1001/containers 2>/dev/null || true
`)

	if authB64 != "" {
		sb.WriteString(`    mkdir -p /run/user/1001/containers /home/colab/.config/containers /home/colab/.docker /root/.docker
    echo '` + authB64 + `' | base64 -d > /run/user/1001/containers/auth.json
    chown colab:colab /run/user/1001/containers/auth.json
    chmod 600 /run/user/1001/containers/auth.json
    cp /run/user/1001/containers/auth.json /home/colab/.config/containers/auth.json
    cp /run/user/1001/containers/auth.json /home/colab/.docker/config.json
    chown -R colab:colab /home/colab/.config/containers /home/colab/.docker
    chmod 600 /home/colab/.config/containers/auth.json /home/colab/.docker/config.json
    cp /run/user/1001/containers/auth.json /root/.docker/config.json
    chmod 755 /root/.docker
    chmod 644 /root/.docker/config.json
`)
	}

	sb.WriteString("fi\n")
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
