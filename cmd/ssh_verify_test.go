package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyManagedHost(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh binary not available")
	}
	dir := t.TempDir()
	managed := filepath.Join(dir, "colab_config")
	main := filepath.Join(dir, "config")
	write := func(mainContent, managedContent string) {
		t.Helper()
		if err := os.WriteFile(managed, []byte(managedContent), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(main, []byte(mainContent), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MYCOLAB_SSH_CONFIG", main)

	write(
		"Include "+managed+"\nHost other\n    HostName x\n",
		"Host sess1\n    ProxyCommand mycolab ssh -s sess1\n",
	)
	if err := verifyManagedHost("sess1", "sess1"); err != nil {
		t.Fatalf("matching session: %v", err)
	}
	if err := verifyManagedHost("sess1", "sess2"); err == nil {
		t.Fatal("mismatched session: nil error, want shadow error")
	}

	write(
		"Host sess1\n    HostName x\n",
		"Host sess1\n    ProxyCommand mycolab ssh -s sess1\n",
	)
	if err := verifyManagedHost("sess1", "sess1"); err == nil {
		t.Fatal("missing ProxyCommand: nil error, want error")
	}
}

func TestManagedProfileName(t *testing.T) {
	block := colabHostBlock("main", "sess1")
	if got := managedProfileName(block); got != "main" {
		t.Errorf("managedProfileName(generated) = %q, want main", got)
	}
	if got := managedProfileName("Host sess1\n    HostName x\n"); got != "" {
		t.Errorf("managedProfileName(no tag) = %q, want empty", got)
	}
	if got := managedProfileName(""); got != "" {
		t.Errorf("managedProfileName(empty) = %q, want empty", got)
	}
}

func TestColabHostBlock(t *testing.T) {
	block := colabHostBlock("main", "sess1")
	for _, want := range []string{
		"# Profile: main | Session: sess1\n",
		"Host sess1\n",
		"ProxyCommand mycolab ssh -s sess1\n",
		"ForwardAgent yes\n",
		"AddKeysToAgent yes\n",
		"ControlMaster auto\n",
		"ControlPath ~/.ssh/cm-sess1-%C\n",
		"ControlPersist 10m\n",
		"ServerAliveInterval 60\n",
		"ServerAliveCountMax 3\n",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("colabHostBlock missing %q:\n%s", want, block)
		}
	}
}

func TestVerifyManagedHostRejectsLegacyProxy(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh binary not available")
	}
	dir := t.TempDir()
	managed := filepath.Join(dir, "colab_config")
	main := filepath.Join(dir, "config")
	if err := os.WriteFile(managed, []byte("Host sess1\n    ProxyCommand colab ssh --proxy-mode -s sess1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("Include "+managed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MYCOLAB_SSH_CONFIG", main)
	if err := verifyManagedHost("sess1", "sess1"); err == nil {
		t.Fatal("legacy colab proxy: nil error, want rejection (re-run prepare to migrate)")
	}
}
