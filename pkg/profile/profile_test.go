package profile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlesoctav/mycolab/pkg/profile"
)

func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"work", "personal", "a-1_B"} {
		if err := profile.ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "has space", "a/b", "a.json", ".hidden", "ünïcödé"} {
		if err := profile.ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", bad)
		}
	}
}

func TestCreateListExists(t *testing.T) {
	tempHome(t)
	if names, err := profile.List(); err != nil || len(names) != 0 {
		t.Fatalf("List() on fresh home = %v, %v; want empty, nil", names, err)
	}
	path, err := profile.Create("work")
	if err != nil {
		t.Fatalf("Create(work) = %v", err)
	}
	if _, err := profile.Create("work"); err == nil {
		t.Fatal("Create(work) twice = nil, want error")
	}
	if _, err := profile.Create("bad name"); err == nil {
		t.Fatal("Create(bad name) = nil, want error")
	}
	if _, err := profile.Create("personal"); err != nil {
		t.Fatalf("Create(personal) = %v", err)
	}
	names, err := profile.List()
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(names) != 2 || names[0] != "personal" || names[1] != "work" {
		t.Fatalf("List() = %v, want [personal work]", names)
	}
	if ok, _ := profile.Exists("work"); !ok {
		t.Fatal("Exists(work) = false, want true")
	}
	if ok, _ := profile.Exists("nope"); ok {
		t.Fatal("Exists(nope) = true, want false")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "{}\n" {
		t.Fatalf("profile file = %q, %v; want %q", content, err, "{}\n")
	}
	// Token sidecar exists but is empty (logged out).
	if ok, _ := profile.HasToken("work"); ok {
		t.Fatal("HasToken(work) on fresh profile = true, want false")
	}
	tokenPath, _ := profile.TokenPath("work")
	if fi, err := os.Stat(tokenPath); err != nil || fi.Size() != 0 {
		t.Fatalf("token file stat = %v, %v; want empty file", fi, err)
	}
}

func TestSetAndGetCurrent(t *testing.T) {
	home := tempHome(t)
	if cur, err := profile.GetCurrent(); err != nil || cur != "" {
		t.Fatalf("GetCurrent() fresh = %q, %v; want empty", cur, err)
	}
	if _, err := profile.Create("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Create("personal"); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.SetCurrent("work"); err != nil {
		t.Fatalf("SetCurrent(work) = %v", err)
	}
	if cur, _ := profile.GetCurrent(); cur != "work" {
		t.Fatalf("GetCurrent() = %q, want work", cur)
	}
	link, _ := profile.ActiveSessionsPath()
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("sessions.json is not a symlink: %v", err)
	}
	want := filepath.Join(home, ".config", "mycolab", "work.json")
	if target != want {
		t.Fatalf("sessions.json -> %q, want %q", target, want)
	}
	tokenLink, _ := profile.ActiveTokenPath()
	tokenTarget, err := os.Readlink(tokenLink)
	if err != nil {
		t.Fatalf("token.json is not a symlink: %v", err)
	}
	if want := filepath.Join(home, ".config", "mycolab", "work.token.json"); tokenTarget != want {
		t.Fatalf("token.json -> %q, want %q", tokenTarget, want)
	}
	// Switching repoints both links.
	if _, err := profile.SetCurrent("personal"); err != nil {
		t.Fatal(err)
	}
	if cur, _ := profile.GetCurrent(); cur != "personal" {
		t.Fatalf("GetCurrent() = %q, want personal", cur)
	}
	// Unknown profile errors and keeps the old one active.
	if _, err := profile.SetCurrent("nope"); err == nil {
		t.Fatal("SetCurrent(nope) = nil, want error")
	}
	if cur, _ := profile.GetCurrent(); cur != "personal" {
		t.Fatalf("GetCurrent() after failed switch = %q, want personal", cur)
	}
}

func TestSetCurrentBacksUpRealFiles(t *testing.T) {
	home := tempHome(t)
	colabDir := filepath.Join(home, ".config", "colab-cli")
	if err := os.MkdirAll(colabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacySessions := `{"old": {"name": "old", "token": "x", "url": "u", "endpoint": "e"}}`
	if err := os.WriteFile(filepath.Join(colabDir, "sessions.json"), []byte(legacySessions), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(colabDir, "token.json"), []byte(`{"token": "t"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Create("work"); err != nil {
		t.Fatal(err)
	}
	backups, err := profile.SetCurrent("work")
	if err != nil {
		t.Fatalf("SetCurrent = %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("backups = %v, want 2 entries", backups)
	}
	restored, err := os.ReadFile(filepath.Join(colabDir, "sessions.json.bak"))
	if err != nil || string(restored) != legacySessions {
		t.Fatalf("sessions.json.bak = %q, %v; want legacy content", restored, err)
	}
	// A second backup of the same path must not clobber the first.
	if err := os.Remove(filepath.Join(colabDir, "sessions.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(colabDir, "sessions.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.SetCurrent("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(colabDir, "sessions.json.bak.1")); err != nil {
		t.Fatalf("expected numbered backup sessions.json.bak.1: %v", err)
	}
	restored, _ = os.ReadFile(filepath.Join(colabDir, "sessions.json.bak"))
	if string(restored) != legacySessions {
		t.Fatal("original .bak was clobbered by second backup")
	}
}

func TestGetCurrentIgnoresUnmanaged(t *testing.T) {
	home := tempHome(t)
	colabDir := filepath.Join(home, ".config", "colab-cli")
	if err := os.MkdirAll(colabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Real file (never managed): no current profile.
	if err := os.WriteFile(filepath.Join(colabDir, "sessions.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cur, _ := profile.GetCurrent(); cur != "" {
		t.Fatalf("GetCurrent() with real file = %q, want empty", cur)
	}
	// Symlink pointing outside mycolab dir: unmanaged too.
	if err := os.Remove(filepath.Join(colabDir, "sessions.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/elsewhere.json", filepath.Join(colabDir, "sessions.json")); err != nil {
		t.Fatal(err)
	}
	if cur, _ := profile.GetCurrent(); cur != "" {
		t.Fatalf("GetCurrent() with foreign symlink = %q, want empty", cur)
	}
}

func TestSessions(t *testing.T) {
	tempHome(t)
	if _, err := profile.Create("work"); err != nil {
		t.Fatal(err)
	}
	if n, _ := profile.SessionCount("work"); n != 0 {
		t.Fatalf("SessionCount(empty) = %d, want 0", n)
	}
	path, _ := profile.ProfilePath("work")
	body := `{
		"trainer": {"name": "trainer", "token": "t", "url": "u", "endpoint": "e1", "variant": "GPU", "accelerator": "A100"},
		"cpu1": {"name": "cpu1", "token": "t", "url": "u", "endpoint": "e2", "variant": "DEFAULT", "accelerator": "NONE"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sessions, err := profile.Sessions("work")
	if err != nil {
		t.Fatalf("Sessions() = %v", err)
	}
	if len(sessions) != 2 || sessions[0].Name != "cpu1" || sessions[1].Name != "trainer" {
		t.Fatalf("Sessions() = %+v, want sorted [cpu1 trainer]", sessions)
	}
	if sessions[1].Accelerator != "A100" || sessions[1].Endpoint != "e1" {
		t.Fatalf("trainer session = %+v, want A100/e1", sessions[1])
	}
	if n, _ := profile.SessionCount("work"); n != 2 {
		t.Fatalf("SessionCount() = %d, want 2", n)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Sessions("work"); err == nil {
		t.Fatal("Sessions() with invalid JSON = nil, want error")
	}
}

func TestHasToken(t *testing.T) {
	tempHome(t)
	if _, err := profile.Create("work"); err != nil {
		t.Fatal(err)
	}
	tokenPath, _ := profile.TokenPath("work")
	if err := os.WriteFile(tokenPath, []byte(`{"access_token": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, _ := profile.HasToken("work"); !ok {
		t.Fatal("HasToken() after login = false, want true")
	}
}

func TestServerSelect(t *testing.T) {
	tempHome(t)
	if got, _ := profile.GetServer(); got != "" {
		t.Fatalf("GetServer() on fresh home = %q, want empty", got)
	}
	if err := profile.SetServer(""); err == nil {
		t.Fatal("SetServer(\"\") = nil, want error")
	}
	if err := profile.SetServer("free"); err != nil {
		t.Fatalf("SetServer(free) = %v", err)
	}
	if got, _ := profile.GetServer(); got != "free" {
		t.Fatalf("GetServer() = %q, want free", got)
	}
	if err := profile.SetServer("other"); err != nil {
		t.Fatalf("SetServer(other) = %v", err)
	}
	if got, _ := profile.GetServer(); got != "other" {
		t.Fatalf("GetServer() after reselect = %q, want other", got)
	}
}
