package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVerifyColabHost(t *testing.T) {
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
		"Host colab\n    ProxyCommand colab ssh --proxy-mode -s sess1\n",
	)
	if err := verifyColabHost("sess1"); err != nil {
		t.Fatalf("matching session: %v", err)
	}
	if err := verifyColabHost("sess2"); err == nil {
		t.Fatal("mismatched session: nil error, want shadow error")
	}

	write(
		"Host colab\n    HostName x\n",
		"Host colab\n    ProxyCommand colab ssh --proxy-mode -s sess1\n",
	)
	if err := verifyColabHost("sess1"); err == nil {
		t.Fatal("missing ProxyCommand: nil error, want error")
	}
}
