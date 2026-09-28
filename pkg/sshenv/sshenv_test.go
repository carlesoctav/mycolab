package sshenv

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDenied(t *testing.T) {
	for _, name := range []string{
		"SHLVL", "_", "PWD", "OLDPWD", "SHELL", "HOME", "USER", "TERM",
		"ENV", "BASH_ENV", "TMUX", "SSH_CLIENT", "SSH_AUTH_SOCK", "SSH_TTY",
	} {
		if !Denied(name) {
			t.Errorf("Denied(%q) = false, want true", name)
		}
	}
	for _, name := range []string{
		"LD_LIBRARY_PATH", "PATH", "PYTHONPATH", "TPU_WORKER_ID",
		"PJRT_DEVICE", "XRT_TPU_CONFIG", "COLAB_GPU", "LANG",
	} {
		if Denied(name) {
			t.Errorf("Denied(%q) = true, want false", name)
		}
	}
}

func TestParseNulEnv(t *testing.T) {
	vars := ParseNulEnv([]byte("A=1\x00B=x=y\x00EMPTY=\x00NOEQUALS\x00\x00"))
	want := map[string]string{"A": "1", "B": "x=y", "EMPTY": ""}
	if len(vars) != len(want) {
		t.Fatalf("parsed %d vars, want %d: %v", len(vars), len(want), vars)
	}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
}

func TestDecodeCapture(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("LD_LIBRARY_PATH=/usr/lib64-nvidia\x00PATH=/a:/b\x00"))
	out := "[colab] Using unique session 's1'.\n\n" + raw + "\n"
	vars, err := DecodeCapture(out)
	if err != nil {
		t.Fatalf("DecodeCapture: %v", err)
	}
	if vars["LD_LIBRARY_PATH"] != "/usr/lib64-nvidia" || vars["PATH"] != "/a:/b" {
		t.Fatalf("decoded vars wrong: %v", vars)
	}
	if _, err := DecodeCapture("[colab] nothing else\n"); err == nil {
		t.Error("notice-only output: nil error, want error")
	}
	if _, err := DecodeCapture("!!!not-base64!!!\n"); err == nil {
		t.Error("garbage output: nil error, want error")
	}
}

func TestDecodeCaptureIgnoresRichNoise(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("A=1\x00"))
	// Rich spinner/panel frames, notices, and remote stderr must not
	// corrupt the payload; only the base64 line survives.
	out := "╭──────────╮\n[colab] Using session 's1'.\n⠋ waiting\n" + raw + "\nsome stderr words\n"
	vars, err := DecodeCapture(out)
	if err != nil {
		t.Fatalf("DecodeCapture with noise: %v", err)
	}
	if vars["A"] != "1" {
		t.Fatalf("decoded vars wrong: %v", vars)
	}
}

func TestRenderSetEnv(t *testing.T) {
	directive, skipped := RenderSetEnv(map[string]string{
		"B":     "two words",
		"A":     `q"uote\slash`,
		"MULTI": "line1\nline2",
	})
	if len(skipped) != 1 || skipped[0] != "MULTI" {
		t.Fatalf("skipped = %v, want [MULTI]", skipped)
	}
	if strings.Count(directive, "\n") > 0 || !strings.HasPrefix(directive, "SetEnv ") {
		t.Fatalf("not a single SetEnv line: %q", directive)
	}
	// Sorted (A before B) and quoted with escapes.
	want := `SetEnv A="q\"uote\\slash" B="two words"`
	if directive != want {
		t.Errorf("directive = %q, want %q", directive, want)
	}
	if directive, _ := RenderSetEnv(map[string]string{}); directive != "" {
		t.Errorf("empty vars: directive = %q, want empty", directive)
	}
}

func TestInstallerScript(t *testing.T) {
	for _, want := range []string{
		InstallerMarker, "sshd -t", "# BEGIN mycolab-env", "# END mycolab-env",
		"sshd_config.bak.mycolab", "pkill -HUP",
	} {
		if !strings.Contains(InstallerScript, want) {
			t.Errorf("installer missing %q", want)
		}
	}
}
