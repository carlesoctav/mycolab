package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLsyncdConfRoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := "/content/proj"
	if err := scaffoldLsyncd(dir, target, "trainer", "trainer", false); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	m, err := parseLsyncdConf(filepath.Join(dir, "trainer.conf.lua"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Source != dir {
		t.Errorf("Source = %q, want %q", m.Source, dir)
	}
	if m.Host != "trainer" {
		t.Errorf("Host = %q, want trainer", m.Host)
	}
	if m.Target != target {
		t.Errorf("Target = %q, want %q", m.Target, target)
	}
	for _, want := range []string{".git/", ".venv/", "__pycache__/", "*.pyc"} {
		found := false
		for _, e := range m.Excludes {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Excludes missing %q: %v", want, m.Excludes)
		}
	}
}

func TestParseLsyncdConfErrors(t *testing.T) {
	if _, err := parseLsyncdConf(filepath.Join(t.TempDir(), "trainer.conf.lua")); err == nil {
		t.Error("missing config: nil error, want error")
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "trainer.conf.lua")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(conf, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sync { source = \"/x\" }\n")
	if _, err := parseLsyncdConf(conf); err == nil {
		t.Error("missing fields: nil error, want error")
	}
	write("sync { source = \"/x\",\n host = \"h\",\n targetdir = \"relative\" }\n")
	if _, err := parseLsyncdConf(conf); err == nil {
		t.Error("relative targetdir: nil error, want error")
	}
}

func TestPullArgs(t *testing.T) {
	m := &lsyncdMapping{Source: "/local/proj", Host: "trainer", Target: "/content/proj", Excludes: []string{".git/", ".venv/"}}
	args := pullArgs(m, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-avz",
		"--exclude=.git/",
		"--exclude=.venv/",
		"-e ssh",
		"trainer:/content/proj/",
		"/local/proj/",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("pullArgs missing %q: %v", want, args)
		}
	}
	for _, arg := range args {
		if arg == "--delete" || strings.HasPrefix(arg, "--delete=") {
			t.Errorf("pullArgs must never delete: %v", args)
		}
	}
	if dry := pullArgs(m, true); !strings.Contains(strings.Join(dry, " "), "--dry-run") {
		t.Errorf("dry-run missing --dry-run: %v", dry)
	}
}

func TestPullFromRemoteSelectsConf(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldLsyncd(dir, "/content/proj", "trainer", "trainer", false); err != nil {
		t.Fatalf("scaffold trainer: %v", err)
	}
	if err := scaffoldLsyncd(dir, "/content/other", "eval", "eval", false); err != nil {
		t.Fatalf("scaffold eval: %v", err)
	}
	// Fake rsync that records its argv, so conf selection is exercised
	// without touching the network.
	bin := t.TempDir()
	argsFile := filepath.Join(bin, "rsync.args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(filepath.Join(bin, "rsync"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := pullFromRemote(dir, "", true); err == nil {
		t.Error("ambiguous default: nil error, want multiple-configs error")
	} else if !strings.Contains(err.Error(), "multiple lsyncd configs") {
		t.Errorf("ambiguous default error = %q, want multiple-configs error", err)
	}
	if err := pullFromRemote(dir, "trainer", true); err != nil {
		t.Errorf("pull bare name (dry-run) = %v, want nil", err)
	}
	if err := pullFromRemote(dir, "missing", true); err == nil {
		t.Error("missing conf: nil error, want error")
	}
	// The explicit conf drives the transfer, not its sibling.
	if err := pullFromRemote(dir, "eval.conf.lua", false); err != nil {
		t.Fatalf("pull eval.conf.lua = %v, want nil", err)
	}
	argv, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(argv), "eval:/content/other/") {
		t.Errorf("rsync argv missing eval mapping: %q", argv)
	}
	if strings.Contains(string(argv), "trainer") {
		t.Errorf("rsync argv leaked the unselected conf: %q", argv)
	}
}
