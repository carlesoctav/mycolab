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

func TestRemoteScript(t *testing.T) {
	j := &job{Dirs: []dirMap{{Remote: "/content/p"}}, Command: []string{"python a.py | tee o"}}
	if got := j.remoteScript(); got != "cd '/content/p' && python a.py | tee o" {
		t.Errorf("shell string: %q", got)
	}
	j = &job{Command: []string{"python", "a b.py"}}
	if got := j.remoteScript(); got != "'python' 'a b.py'" {
		t.Errorf("argv: %q", got)
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

