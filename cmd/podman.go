package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// podmanSetupScript is the bash script that installs and configures rootless podman
// for use inside Colab runtimes.
const podmanSetupScript = `set -e
if command -v podman >/dev/null 2>&1 && [ -f /usr/local/bin/docker-compose ]; then
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
    fi

    exec runuser -u colab -- /usr/bin/podman "$@"
else
    exec /usr/bin/podman "$@"
fi
INNER_EOF
chmod +x /usr/local/bin/podman
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
	out, err := runPodmanSetup(session, podmanSetupScript)
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
