package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPrepareScript(t *testing.T) {
	s := renderPrepareScript()
	for _, want := range []string{
		"/etc/hosts.bak.mycolab",
		"/root/.tmux.conf",
		"tic -x",
		"command -v rg",
		"curl fd-find ripgrep jq nfs-common tmux libfuse2",
		nvimAppImageURL,
		"podman fuse-overlayfs slirp4netns uidmap conmon crun iptables curl",
		"useradd -m -s /bin/bash colab",
		"/dev/net/tun",
		"docker-compose",
		`mount_program = \"/usr/bin/fuse-overlayfs\"`,
		`compose_warning_logs = false`,
		`unqualified-search-registries = [\"docker.io\"]`,
		"ln -sf /usr/local/bin/podman /usr/local/bin/docker",
		"ln -sf /run/user/1001/podman/podman.sock /var/run/docker.sock",
		"/run/user/1001/containers",
		`chown -R colab:colab "$dest_dir"`,
		"start-stop-daemon --start",
		"start-stop-daemon --status",
		"/run/user/1001/podman.pid",
		"flock 200",
		"/run/podman-service.lock",
		`/usr/bin/podman "$@"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("renderPrepareScript missing %q", want)
		}
	}
}

func TestRunPrepareScript(t *testing.T) {
	orig := runPrepareScript
	defer func() { runPrepareScript = orig }()

	var calledSession, calledScript string
	var calledArgs []string
	runPrepareScript = func(session, script string, args ...string) ([]byte, error) {
		calledSession = session
		calledScript = script
		calledArgs = args
		return []byte("ok"), nil
	}

	out, err := runPrepareScript("test-sess", "echo hello", "--podman")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "ok" {
		t.Errorf("got %q, want 'ok'", string(out))
	}
	if calledSession != "test-sess" || calledScript != "echo hello" || len(calledArgs) != 1 || calledArgs[0] != "--podman" {
		t.Errorf("unexpected call arguments: session=%q script=%q args=%v", calledSession, calledScript, calledArgs)
	}
}

func TestRenderPrepareScriptWithAuth(t *testing.T) {
	orig := loadLocalContainersAuth
	defer func() { loadLocalContainersAuth = orig }()

	fakeAuth := []byte(`{"auths":{"example.com":{"auth":"dGVzdA=="}}}`)
	loadLocalContainersAuth = func() ([]byte, error) {
		return fakeAuth, nil
	}

	s := renderPrepareScript()
	if !strings.Contains(s, "/run/user/1001/containers/auth.json") {
		t.Errorf("renderPrepareScript missing /run/user/1001/containers/auth.json")
	}
	if !strings.Contains(s, "chown colab:colab /run/user/1001/containers/auth.json") {
		t.Errorf("renderPrepareScript missing chown for auth.json")
	}
	if !strings.Contains(s, "chmod 600 /run/user/1001/containers/auth.json") {
		t.Errorf("renderPrepareScript missing chmod for auth.json")
	}
}

func TestLoadLocalContainersAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Case 1: no files present
	data, err := loadLocalContainersAuth()
	if err != nil {
		t.Fatalf("loadLocalContainersAuth failed: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data, got %q", string(data))
	}

	// Case 2: ~/.docker/config.json exists
	dockerDir := filepath.Join(tmp, ".docker")
	if err := os.MkdirAll(dockerDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	dockerContent := []byte(`{"auths":{"docker.io":{"auth":"docker123"}}}`)
	if err := os.WriteFile(filepath.Join(dockerDir, "config.json"), dockerContent, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	data, err = loadLocalContainersAuth()
	if err != nil {
		t.Fatalf("loadLocalContainersAuth failed: %v", err)
	}
	if string(data) != string(dockerContent) {
		t.Errorf("got %q, want %q", string(data), string(dockerContent))
	}

	// Case 3: ~/.config/containers/auth.json takes precedence
	containersDir := filepath.Join(tmp, ".config", "containers")
	if err := os.MkdirAll(containersDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	containersContent := []byte(`{"auths":{"ghcr.io":{"auth":"ghcr123"}}}`)
	if err := os.WriteFile(filepath.Join(containersDir, "auth.json"), containersContent, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	data, err = loadLocalContainersAuth()
	if err != nil {
		t.Fatalf("loadLocalContainersAuth failed: %v", err)
	}
	if string(data) != string(containersContent) {
		t.Errorf("got %q, want %q", string(data), string(containersContent))
	}
}
