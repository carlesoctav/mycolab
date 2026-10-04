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
	if err := scaffoldLsyncd(dir, target, "trainer", "trainer", false); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "trainer.conf.lua"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`source    = "` + dir + `"`,
		`host      = "trainer"`,
		`targetdir = "` + target + `"`,
		`"/tmp/lsyncd-` + slugify(filepath.Base(dir)) + `-trainer`,
		`mycolab sync trainer.conf.lua`,
		`".git/"`,
		`".venv/"`,
		"insist     = true",
		"nodaemon   = true",
	} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("trainer.conf.lua missing %q:\n%s", want, conf)
		}
	}
	doc, err := os.ReadFile(filepath.Join(dir, "LSYNCD.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{dir, target, "mycolab new", "mycolab pull trainer.conf.lua", "mycolab sync trainer.conf.lua", "colab exec", "Golden rule", "tmux", "capture-pane", "Multiple sessions"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("LSYNCD.md missing %q", want)
		}
	}

	// Second run for the same session without --force must refuse to overwrite.
	if err := scaffoldLsyncd(dir, target, "trainer", "trainer", false); err == nil {
		t.Error("second scaffold without force: nil error, want overwrite refusal")
	}
	// With force it overwrites cleanly.
	if err := scaffoldLsyncd(dir, target, "trainer", "trainer", true); err != nil {
		t.Errorf("scaffold with force: %v", err)
	}
}

func TestScaffoldLsyncdHostOverride(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldLsyncd(dir, "/content/proj", "custom", "trainer", false); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "trainer.conf.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conf), `host      = "custom"`) {
		t.Errorf("trainer.conf.lua missing host override:\n%s", conf)
	}
}

func TestScaffoldMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldLsyncd(dir, "/content/proj", "trainer", "trainer", false); err != nil {
		t.Fatalf("scaffold trainer: %v", err)
	}
	docBefore, err := os.ReadFile(filepath.Join(dir, "LSYNCD.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := scaffoldLsyncd(dir, "/content/proj", "eval", "eval", false); err != nil {
		t.Fatalf("scaffold eval in the same dir: %v", err)
	}
	for session := range map[string]bool{"trainer": true, "eval": true} {
		conf, err := os.ReadFile(filepath.Join(dir, session+".conf.lua"))
		if err != nil {
			t.Fatalf("read %s.conf.lua: %v", session, err)
		}
		if !strings.Contains(string(conf), `host      = "`+session+`"`) {
			t.Errorf("%s.conf.lua missing its host:\n%s", session, conf)
		}
	}
	// The shared doc is kept from the first scaffold, not clobbered.
	docAfter, err := os.ReadFile(filepath.Join(dir, "LSYNCD.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(docAfter) != string(docBefore) {
		t.Error("second scaffold without force rewrote LSYNCD.md, want it kept")
	}
}

func TestScaffoldLsyncdErrors(t *testing.T) {
	dir := t.TempDir()
	if err := scaffoldLsyncd(filepath.Join(dir, "missing"), "/content/x", "trainer", "trainer", false); err == nil {
		t.Error("missing source: nil error, want error")
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldLsyncd(file, "/content/x", "trainer", "trainer", false); err == nil {
		t.Error("file source: nil error, want error")
	}
	if err := scaffoldLsyncd(dir, "relative/path", "trainer", "trainer", false); err == nil {
		t.Error("relative target: nil error, want error")
	}
	if err := scaffoldLsyncd(dir, "/content/x", "", "trainer", false); err == nil {
		t.Error("empty host: nil error, want error")
	}
	for _, name := range []string{"", ".", "..", "-trainer", "a/b", `a\b`, "has space", "we#ird", "quo'te"} {
		if err := scaffoldLsyncd(dir, "/content/x", "trainer", name, false); err == nil {
			t.Errorf("name %q: nil error, want invalid-name error", name)
		}
	}
}
