package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// bridgeHome builds an isolated HOME holding profile "main" with the given
// sessions.json content, activated via the colab-cli symlink, plus a stub
// colab binary on PATH.
func bridgeHome(t *testing.T, sessions string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, ".config", "mycolab")
	cli := filepath.Join(home, ".config", "colab-cli")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cli, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cfg, "main.json")
	if err := os.WriteFile(target, []byte(sessions), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(cli, "sessions.json")); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "colab"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestResolveBridgeSession(t *testing.T) {
	bridgeHome(t, `{"trainer": {"accelerator": "L4", "variant": "GPU", "endpoint": "e"}}`)
	got, err := resolveBridgeSession("trainer")
	if err != nil {
		t.Fatalf("existing session: %v", err)
	}
	if !strings.HasSuffix(got, string(os.PathSeparator)+"colab") {
		t.Errorf("colab binary = %q", got)
	}
	if _, err := resolveBridgeSession("ghost"); err == nil {
		t.Fatal("missing session: nil error, want refusal")
	} else if !strings.Contains(err.Error(), "refusing to auto-create") {
		t.Errorf("missing session error = %q, want auto-create refusal", err)
	}
}

func TestResolveBridgeSessionNoProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := resolveBridgeSession("trainer"); err == nil {
		t.Fatal("nil error, want no-active-profile error")
	} else if !strings.Contains(err.Error(), "no active profile") {
		t.Errorf("error = %q", err)
	}
}

func TestRunBridgeDetachesSession(t *testing.T) {
	t.Cleanup(func() { signal.Reset(syscall.SIGHUP) })
	dir := t.TempDir()
	outFile := filepath.Join(dir, "ids.txt")
	stub := filepath.Join(dir, "colab")
	script := fmt.Sprintf("#!/bin/sh\nps -o pid=,pgid= -p $$ > %q\n", outFile)
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runBridge(stub, "trainer"); err != nil {
		t.Fatalf("runBridge() = %v, want nil", err)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		t.Fatalf("ps output = %q, want 2 fields", string(raw))
	}
	childPID, childPGID := fields[0], fields[1]
	if childPID != childPGID {
		t.Fatalf("child pid=%s pgid=%s, want own process group (Setsid)", childPID, childPGID)
	}
	if childPGID == fmt.Sprintf("%d", syscall.Getpgrp()) {
		t.Fatalf("child pgid=%s matches parent pgid, want detached", childPGID)
	}
}
