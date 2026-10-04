package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestParseDirMap(t *testing.T) {
	d, err := parseDirMap("/tmp/proj:/content/proj/")
	if err != nil || d.Local != "/tmp/proj" || d.Remote != "/content/proj" {
		t.Fatalf("got %+v, %v", d, err)
	}
	for _, bad := range []string{"nocolon", "/tmp:rel", ":/x", "/tmp:"} {
		if _, err := parseDirMap(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestParseMount(t *testing.T) {
	m, err := parseMount("me/data/sub:/content/data")
	if err != nil || m.Bucket != "me/data/sub" || m.Remote != "/content/data" {
		t.Fatalf("got %+v, %v", m, err)
	}
	for _, bad := range []string{"data:/x", "me/d;rm:/x", "me/d:rel"} {
		if _, err := parseMount(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestTmuxRunScript(t *testing.T) {
	j := &job{
		ID:       "test1234",
		Dirs:     []dirMap{{Remote: "/content/p"}},
		Command:  []string{"python", "train.py"},
		Sidecars: []sidecarSpec{{Name: "server", Command: "python server.py"}},
	}
	s := j.tmuxRunScript()
	for _, want := range []string{
		"tmux new-session -d -s mycolab -n main",
		"tmux new-window -t mycolab -n 'server'",
		"train.py",
		"python server.py",
		"/tmp/mycolab_run/test1234/done",
		"/tmp/mycolab_run/test1234/out.log",
		"tail -n +1 -f",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
}

func TestMountScript(t *testing.T) {
	s := mountSpec{Bucket: "me/data", Remote: "/content/data"}.mountScript()
	if !strings.Contains(s, "hf-mount start") || !strings.Contains(s, "bucket me/data '/content/data'") {
		t.Errorf("script: %q", s)
	}
}

func TestRsyncIgnoreFilters(t *testing.T) {
	tmp := t.TempDir()

	// 1. .gitignore does not exist
	filters := rsyncIgnoreFilters(tmp)
	if len(filters) != 0 {
		t.Errorf("expected no filters, got %v", filters)
	}

	// 2. .gitignore exists
	gitIgnorePath := tmp + "/.gitignore"
	if err := os.WriteFile(gitIgnorePath, []byte("node_modules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	filters = rsyncIgnoreFilters(tmp)
	if len(filters) != 1 || filters[0] != "--filter=:- .gitignore" {
		t.Errorf("expected gitignore filter, got %v", filters)
	}
}

