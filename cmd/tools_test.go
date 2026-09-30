package cmd

import (
	"strings"
	"testing"
)

func TestToolsInstallStdin(t *testing.T) {
	stdin := toolsInstallStdin()
	if !strings.HasPrefix(stdin, "!(") {
		t.Errorf("stdin %q: want `!(...)` shell form", stdin[:20])
	}
	if !strings.HasSuffix(stdin, "&& echo "+toolsMarker+"\n") {
		t.Errorf("stdin missing marker suffix:\n%s", stdin)
	}
	for _, want := range []string{
		"command -v rg",
		"command -v jq",
		"command -v nvim",
		"command -v fd || command -v fdfind",
		"apt-get update",
		"DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl fd-find ripgrep jq",
		"libfuse2",
		nvimAppImageURL,
		"chmod +x /usr/local/bin/nvim",
		`ln -sf "$(command -v fdfind)" /usr/local/bin/fd`,
		"nvim --version",
	} {
		if !strings.Contains(stdin, want) {
			t.Errorf("stdin missing %q:\n%s", want, stdin)
		}
	}
	if strings.Contains(stdin, "webi.sh") {
		t.Errorf("stdin must not use webi:\n%s", stdin)
	}
	if strings.Contains(stdin, "sudo") {
		t.Errorf("stdin must not use sudo (exec runs as root):\n%s", stdin)
	}
}
