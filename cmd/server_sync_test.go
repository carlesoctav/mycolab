package cmd

import (
	"strings"
	"testing"
)

func TestRemoteSlotPaths(t *testing.T) {
	if got := remoteSessionsFile("/home/u", "main"); got != "/home/u/.config/mycolab/main.json" {
		t.Fatalf("remoteSessionsFile = %q", got)
	}
	if got := remoteTokenFile("/home/u", "ggwp"); got != "/home/u/.config/mycolab/ggwp.token.json" {
		t.Fatalf("remoteTokenFile = %q", got)
	}
}

func TestMergeSessions(t *testing.T) {
	local := []byte(`{"keep": {"endpoint": "e1", "token": "t1"}, "move": {"endpoint": "old", "token": "t2"}, "refresh": {"endpoint": "e3", "token": "t3"}}`)
	remote := []byte(`{"move": {"endpoint": "new", "token": "t4", "keep_alive_pid": "123"}, "refresh": {"endpoint": "e3", "token": "t9"}, "added": {"endpoint": "e4"}}`)
	merged, stats, err := mergeSessions(local, remote)
	if err != nil {
		t.Fatalf("mergeSessions = %v", err)
	}
	text := string(merged)
	// Local-only entry preserved, server entries win by name, pid stripped.
	for _, want := range []string{`"keep"`, `"added"`, `"endpoint": "new"`, `"token": "t9"`} {
		if !strings.Contains(text, want) {
			t.Errorf("merged missing %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{`"endpoint": "old"`, "keep_alive_pid"} {
		if strings.Contains(text, gone) {
			t.Errorf("merged still has %q:\n%s", gone, text)
		}
	}
	if len(stats.added) != 1 || stats.added[0] != "added" {
		t.Errorf("added = %v, want [added]", stats.added)
	}
	if len(stats.moved) != 1 || stats.moved[0] != "move" {
		t.Errorf("moved = %v, want [move]", stats.moved)
	}
}

func TestMergeSessionsEdges(t *testing.T) {
	// Blank inputs merge to an empty object.
	merged, stats, err := mergeSessions([]byte(""), []byte(""))
	if err != nil {
		t.Fatalf("mergeSessions(blank) = %v", err)
	}
	if string(merged) != "{}\n" {
		t.Fatalf("mergeSessions(blank) = %q, want {}\\n", merged)
	}
	if len(stats.added) != 0 || len(stats.moved) != 0 {
		t.Fatalf("mergeSessions(blank) stats = %+v, want empty", stats)
	}
	// Invalid JSON on either side is an error, never a silent wipe.
	if _, _, err := mergeSessions([]byte("{bad"), []byte("{}")); err == nil {
		t.Error("mergeSessions(bad local) = nil error, want error")
	}
	if _, _, err := mergeSessions([]byte("{}"), []byte("{bad")); err == nil {
		t.Error("mergeSessions(bad remote) = nil error, want error")
	}
	if _, _, err := mergeSessions([]byte("{}"), []byte(`{"x": 42}`)); err == nil {
		t.Error("mergeSessions(non-object entry) = nil error, want error")
	}
}
