package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConf(t *testing.T, dir, name string) string {
	t.Helper()
	conf := filepath.Join(dir, name)
	if err := os.WriteFile(conf, []byte("-- test"), 0o644); err != nil {
		t.Fatal(err)
	}
	return conf
}

func TestResolveLsyncdConfig(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		if _, err := resolveLsyncdConfig(t.TempDir(), ""); err == nil {
			t.Error("nil error, want no-config error")
		}
	})
	t.Run("legacy default", func(t *testing.T) {
		dir := t.TempDir()
		want := writeConf(t, dir, "lsyncd.conf.lua")
		got, err := resolveLsyncdConfig(dir, "")
		if err != nil || got != want {
			t.Errorf("resolve = %q, %v; want %q, nil", got, err, want)
		}
	})
	t.Run("single new-style default", func(t *testing.T) {
		dir := t.TempDir()
		want := writeConf(t, dir, "trainer.conf.lua")
		got, err := resolveLsyncdConfig(dir, "")
		if err != nil || got != want {
			t.Errorf("resolve = %q, %v; want %q, nil", got, err, want)
		}
	})
	t.Run("legacy wins over new-style", func(t *testing.T) {
		dir := t.TempDir()
		want := writeConf(t, dir, "lsyncd.conf.lua")
		writeConf(t, dir, "trainer.conf.lua")
		got, err := resolveLsyncdConfig(dir, "")
		if err != nil || got != want {
			t.Errorf("resolve = %q, %v; want %q, nil", got, err, want)
		}
	})
	t.Run("multiple require explicit conf", func(t *testing.T) {
		dir := t.TempDir()
		writeConf(t, dir, "trainer.conf.lua")
		writeConf(t, dir, "eval.conf.lua")
		_, err := resolveLsyncdConfig(dir, "")
		if err == nil {
			t.Fatal("nil error, want multiple-configs error")
		}
		for _, want := range []string{"multiple lsyncd configs", "trainer.conf.lua", "eval.conf.lua"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err, want)
			}
		}
	})
	t.Run("explicit file", func(t *testing.T) {
		dir := t.TempDir()
		want := writeConf(t, dir, "eval.conf.lua")
		got, err := resolveLsyncdConfig(dir, "eval.conf.lua")
		if err != nil || got != want {
			t.Errorf("resolve = %q, %v; want %q, nil", got, err, want)
		}
	})
	t.Run("explicit bare name", func(t *testing.T) {
		dir := t.TempDir()
		want := writeConf(t, dir, "eval.conf.lua")
		got, err := resolveLsyncdConfig(dir, "eval")
		if err != nil || got != want {
			t.Errorf("resolve = %q, %v; want %q, nil", got, err, want)
		}
	})
	t.Run("explicit absolute path", func(t *testing.T) {
		dir := t.TempDir()
		want := writeConf(t, dir, "eval.conf.lua")
		got, err := resolveLsyncdConfig(t.TempDir(), want)
		if err != nil || got != want {
			t.Errorf("resolve = %q, %v; want %q, nil", got, err, want)
		}
	})
	t.Run("explicit missing", func(t *testing.T) {
		if _, err := resolveLsyncdConfig(t.TempDir(), "trainer"); err == nil {
			t.Error("nil error, want no-config error")
		}
	})
	t.Run("explicit directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveLsyncdConfig(dir, "sub"); err == nil {
			t.Error("nil error, want no-config error for a directory")
		}
	})
	t.Run("ignores directories in glob", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "fake.conf.lua"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveLsyncdConfig(dir, ""); err == nil {
			t.Error("nil error, want no-config error when only a directory matches")
		}
	})
}

func TestSyncLiveErrors(t *testing.T) {
	if err := syncLive(t.TempDir(), ""); err == nil {
		t.Error("missing config: nil error, want error")
	}

	dir := t.TempDir()
	writeConf(t, dir, "trainer.conf.lua")
	old := findLsyncd
	t.Cleanup(func() { findLsyncd = old })
	findLsyncd = func() (string, error) { return "", errors.New("not found") }
	if err := syncLive(dir, "trainer"); err == nil {
		t.Error("missing lsyncd: nil error, want error")
	} else if got := err.Error(); !strings.Contains(got, "lsyncd not found") {
		t.Errorf("missing lsyncd error = %q, want lsyncd-not-found message", got)
	}
}

func TestSyncLiveRunsBinary(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "trainer.conf.lua")
	old := findLsyncd
	t.Cleanup(func() { findLsyncd = old })
	findLsyncd = func() (string, error) { return "/bin/echo", nil }
	if err := syncLive(dir, "trainer.conf.lua"); err != nil {
		t.Errorf("syncLive(explicit, echo stub) = %v, want nil", err)
	}
	if err := syncLive(dir, "trainer"); err != nil {
		t.Errorf("syncLive(bare name, echo stub) = %v, want nil", err)
	}
	if err := syncLive(dir, ""); err != nil {
		t.Errorf("syncLive(default, echo stub) = %v, want nil", err)
	}
}
