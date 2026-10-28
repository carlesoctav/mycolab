package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestPodmanSetupScriptContent(t *testing.T) {
	for _, want := range []string{
		"apt-get install -y -qq podman",
		"fuse-overlayfs slirp4netns uidmap conmon crun iptables curl",
		"/dev/net/tun",
		"useradd -m -s /bin/bash colab",
		"docker-compose",
		`mount_program = \"/usr/bin/fuse-overlayfs\"`,
		`cgroups = \"enabled\"`,
		`driver = \"overlay\"`,
		"PODMAN_ALREADY_INSTALLED",
		"PODMAN_OK",
	} {
		if !strings.Contains(podmanSetupScript, want) {
			t.Errorf("podmanSetupScript missing expected snippet: %q", want)
		}
	}

	// Crucial: wrapper must call /usr/bin/podman, NOT bare podman,
	// to prevent infinite recursion via /usr/local/bin/podman.
	if strings.Contains(podmanSetupScript, "exec /usr/local/bin/podman") ||
		strings.Contains(podmanSetupScript, "exec podman") {
		t.Errorf("podmanSetupScript must not call bare podman or /usr/local/bin/podman in wrapper")
	}
}

func TestPushPodmanUnknownSession(t *testing.T) {
	orig := runPodmanSetup
	defer func() { runPodmanSetup = orig }()

	called := false
	runPodmanSetup = func(session, script string) ([]byte, error) {
		called = true
		return nil, nil
	}

	pushPodman("sess", false)
	if called {
		t.Error("pushPodman called runPodmanSetup for unknown session")
	}
}

func TestPushPodmanSuccess(t *testing.T) {
	orig := runPodmanSetup
	defer func() { runPodmanSetup = orig }()

	var calledSession, calledScript string
	runPodmanSetup = func(session, script string) ([]byte, error) {
		calledSession = session
		calledScript = script
		return []byte("PODMAN_OK\n"), nil
	}

	pushPodman("test-sess", true)
	if calledSession != "test-sess" {
		t.Errorf("calledSession = %q, want 'test-sess'", calledSession)
	}
	if calledScript != podmanSetupScript {
		t.Errorf("calledScript mismatch")
	}
}

func TestPushPodmanFailure(t *testing.T) {
	orig := runPodmanSetup
	defer func() { runPodmanSetup = orig }()

	runPodmanSetup = func(session, script string) ([]byte, error) {
		return []byte("error: apt failed\n"), errors.New("exit status 1")
	}

	// Should not panic, logs warning and returns cleanly
	pushPodman("test-sess", true)
}
