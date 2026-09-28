package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"try-agent":     "try-agent",
		"My Project 2":  "my-project-2",
		"a/b\\c":        "a-b-c",
		"...":           "sync",
		"":              "sync",
		"resnet50_v1.2": "resnet50_v1.2",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScaffoldLsyncd(t *testing.T) {
	dir := t.TempDir()
	target := "/content/proj"
	if err := scaffoldLsyncd(dir, target, "colab", false); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "lsyncd.conf.lua"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`source    = "` + dir + `"`,
		`host      = "colab"`,
		`targetdir = "` + target + `"`,
		`"/tmp/lsyncd-` + slugify(filepath.Base(dir)),
		`".git/"`,
		`".venv/"`,
		"insist     = true",
		"nodaemon   = true",
	} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("lsyncd.conf.lua missing %q:\n%s", want, conf)
		}
	}
	doc, err := os.ReadFile(filepath.Join(dir, "LSYNCD.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{dir, target, "mycolab ssh", "colab exec", "Golden rule", "tmux", "capture-pane"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("LSYNCD.md missing %q", want)
		}
	}

	// Second run without --force must refuse to overwrite.
	if err := scaffoldLsyncd(dir, target, "colab", false); err == nil {
		t.Error("second scaffold without force: nil error, want overwrite refusal")
	}
	// With force it overwrites cleanly.
	if err := scaffoldLsyncd(dir, target, "colab", true); err != nil {
		t.Errorf("scaffold with force: %v", err)
	}
}

func TestScaffoldLsyncdErrors(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldLsyncd(filepath.Join(dir, "missing"), "/content/x", "colab", false); err == nil {
		t.Error("missing source: nil error, want error")
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldLsyncd(file, "/content/x", "colab", false); err == nil {
		t.Error("file source: nil error, want error")
	}
	if err := scaffoldLsyncd(dir, "relative/path", "colab", false); err == nil {
		t.Error("relative target: nil error, want error")
	}
	if err := scaffoldLsyncd(dir, "/content/x", "", false); err == nil {
		t.Error("empty host: nil error, want error")
	}
}
