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
	if err := scaffoldLsyncd(dir, target, "trainer", false); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	m, err := parseLsyncdConf(dir)
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
	if _, err := parseLsyncdConf(t.TempDir()); err == nil {
		t.Error("missing config: nil error, want error")
	}
	dir := t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "lsyncd.conf.lua"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sync { source = \"/x\" }\n")
	if _, err := parseLsyncdConf(dir); err == nil {
		t.Error("missing fields: nil error, want error")
	}
	write("sync { source = \"/x\",\n host = \"h\",\n targetdir = \"relative\" }\n")
	if _, err := parseLsyncdConf(dir); err == nil {
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
