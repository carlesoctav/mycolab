package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSyncConfig(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveSyncConfig(dir); err == nil {
		t.Error("missing config: nil error, want error")
	}
	conf := filepath.Join(dir, "lsyncd.conf.lua")
	if err := os.WriteFile(conf, []byte("-- test"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSyncConfig(dir)
	if err != nil || got != conf {
		t.Errorf("resolveSyncConfig = %q, %v; want %q, nil", got, err, conf)
	}
}

func TestSyncLiveErrors(t *testing.T) {
	if err := syncLive(t.TempDir()); err == nil {
		t.Error("missing config: nil error, want error")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lsyncd.conf.lua"), []byte("-- test"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := findLsyncd
	t.Cleanup(func() { findLsyncd = old })
	findLsyncd = func() (string, error) { return "", errors.New("not found") }
	if err := syncLive(dir); err == nil {
		t.Error("missing lsyncd: nil error, want error")
	} else if got := err.Error(); !strings.Contains(got, "lsyncd not found") {
		t.Errorf("missing lsyncd error = %q, want lsyncd-not-found message", got)
	}
}

func TestSyncLiveRunsBinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lsyncd.conf.lua"), []byte("-- test"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := findLsyncd
	t.Cleanup(func() { findLsyncd = old })
	findLsyncd = func() (string, error) { return "/bin/echo", nil }
	if err := syncLive(dir); err != nil {
		t.Errorf("syncLive(echo stub) = %v, want nil", err)
	}
}
