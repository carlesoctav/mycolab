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

// podmanSetupScript is the bash script that installs and configures rootless podman
// for use inside Colab runtimes.
const podmanSetupScript = `set -e
if command -v podman >/dev/null 2>&1 && [ -f /usr/local/bin/docker-compose ] && [ -f /usr/local/bin/podman-mode ] && [ -f /usr/local/bin/crane ] && command -v just >/dev/null 2>&1; then
    echo "PODMAN_ALREADY_INSTALLED"
    exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq && apt-get install -y -qq podman fuse-overlayfs slirp4netns uidmap conmon crun iptables curl

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
    env XDG_RUNTIME_DIR=/run/user/1001 USER=colab HOME=/home/colab \
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
    --runroot /run/user/1001/containers
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
                env XDG_RUNTIME_DIR=/run/user/1001 USER=colab HOME=/home/colab \
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
mkdir -p /run/user/1001/containers && chown colab:colab /run/user/1001/containers 2>/dev/null || true
chmod 700 /run/user/1001/containers 2>/dev/null || true
echo "PODMAN_OK"
`

// runPodmanSetup executes podmanSetupScript over SSH. Stubbable for tests.
var runPodmanSetup = func(session, script string) ([]byte, error) {
	sshBin, err := findSSH()
	if err != nil {
		return nil, fmt.Errorf("ssh binary not found in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var args []string
	if alt := os.Getenv("MYCOLAB_SSH_CONFIG"); alt != "" {
		args = append(args, "-F", alt)
	}
	args = append(args, session, "bash -s")
	c := exec.CommandContext(ctx, sshBin, args...)
	c.Stdin = strings.NewReader(script)
	return c.CombinedOutput()
}

// pushPodman installs and configures rootless podman on the runtime.
// Best-effort by design: failures warn, never fail prepare or new.
func pushPodman(session string, sessionKnown bool) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping podman install.\n", session)
		return
	}
	script := podmanSetupScript
	if authData, _ := loadLocalContainersAuth(); len(authData) > 0 {
		authB64 := base64.StdEncoding.EncodeToString(authData)
		script += fmt.Sprintf("\nmkdir -p /run/user/1001/containers /root/.docker\necho '%s' | base64 -d > /run/user/1001/containers/auth.json\nchown colab:colab /run/user/1001/containers/auth.json\nchmod 600 /run/user/1001/containers/auth.json\necho '%s' | base64 -d > /root/.docker/config.json\nchmod 600 /root/.docker/config.json\n", authB64, authB64)
	}
	out, err := runPodmanSetup(session, script)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			fmt.Printf("Note: podman install failed (%v). Output:\n%s\n", err, msg)
		} else {
			fmt.Printf("Note: podman install failed (%v).\n", err)
		}
		return
	}
	fmt.Println("Podman ready on runtime.")
}
