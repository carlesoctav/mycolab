package cmd

import (
	"os"
	"path/filepath"
	"strings"
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
