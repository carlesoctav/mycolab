package cmd

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunColabTimeoutKillsHungCall(t *testing.T) {
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	start := time.Now()
	_, err = runColabTimeout(sleepBin, "", time.Second, "30")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("hung call: nil error, want context-deadline error")
	}
	if elapsed > 20*time.Second {
		t.Errorf("hung call ran %v, want kill near 1s timeout", elapsed)
	}
}

func TestRunColabTimeoutAppendsRemoteTimeout(t *testing.T) {
	echoBin, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo binary not available")
	}
	out, err := runColabTimeout(echoBin, "stdin-ignored", 10*time.Second, "exec", "-s", "sess")
	if err != nil {
		t.Fatalf("echo exec: %v", err)
	}
	for _, want := range []string{"exec", "-s", "sess", "--timeout", colabRemoteTimeout} {
		if !strings.Contains(out, want) {
			t.Errorf("exec args missing %q in %q", want, out)
		}
	}
	out, err = runColabTimeout(echoBin, "", 10*time.Second, "upload", "-s", "sess", "a", "b")
	if err != nil {
		t.Fatalf("echo upload: %v", err)
	}
	if strings.Contains(out, "--timeout") {
		t.Errorf("non-exec args gained --timeout in %q", out)
	}
}
