package tools

import (
	"strings"
	"testing"
)

func TestLookup(t *testing.T) {
	spec, err := Lookup("muse")
	if err != nil || spec.Name != "muse" {
		t.Errorf("Lookup(muse) = %+v, %v; want muse spec, nil", spec, err)
	}
	if _, err := Lookup("MUSE"); err != nil {
		t.Errorf("Lookup(MUSE) = %v, want nil (case-insensitive)", err)
	}
	if _, err := Lookup("nope"); err == nil {
		t.Error("Lookup(nope) = nil error, want error")
	} else if !strings.Contains(err.Error(), "muse") {
		t.Errorf("Lookup(nope) error %q does not list supported tools", err)
	}
}

func TestMuseSpec(t *testing.T) {
	if Muse.ConfigDir != "muse" {
		t.Errorf("Muse.ConfigDir = %q, want muse", Muse.ConfigDir)
	}
	for _, want := range []string{"auth.json", "settings.json", "trust.json"} {
		found := false
		for _, f := range Muse.ConfigFiles {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Muse.ConfigFiles missing %q: %v", want, Muse.ConfigFiles)
		}
	}
	if !strings.Contains(Muse.InstallCmd, "https://dev.meta.ai/install.sh") {
		t.Errorf("Muse.InstallCmd missing upstream installer: %q", Muse.InstallCmd)
	}
}

func TestInstallStdin(t *testing.T) {
	stdin := Muse.InstallStdin("MARKER_OK")
	if !strings.HasPrefix(stdin, "!") {
		t.Errorf("stdin %q: want `!` shell form", stdin[:20])
	}
	if !strings.HasSuffix(stdin, "&& echo MARKER_OK\n") {
		t.Errorf("stdin missing marker suffix:\n%s", stdin)
	}
	for _, want := range []string{
		"mkdir -p /root/.config/muse",
		"(command -v muse || (curl -fsSL https://dev.meta.ai/install.sh | bash",
		"ln -sf /root/.local/bin/muse /usr/local/bin/muse",
		"command -v muse >/dev/null",
	} {
		if !strings.Contains(stdin, want) {
			t.Errorf("stdin missing %q:\n%s", want, stdin)
		}
	}

	// A spec without post-install or config dir yields a bare install chain.
	bare := Spec{Name: "x", CheckBinary: "xbin", InstallCmd: "install-x"}.InstallStdin("M")
	if strings.Contains(bare, "mkdir") || strings.Contains(bare, "ln -sf") {
		t.Errorf("bare spec stdin has unexpected steps:\n%s", bare)
	}
	if !strings.Contains(bare, "(command -v xbin || (install-x))") {
		t.Errorf("bare spec stdin malformed:\n%s", bare)
	}
}
