package cmd

import (
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
		`chown -R colab:colab "$dest_dir"`,
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
