package cmd

import (
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"ruff":  "'ruff'",
		"a'b":   `'a'"'"'b'`,
		"a; rm": `'a; rm'`,
		"$(id)": `'$(id)'`,
		"":      "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUvInstallStdin(t *testing.T) {
	stdin := uvInstallStdin([]string{"ruff"})
	if !strings.HasPrefix(stdin, "!uv tool install 'ruff'") {
		t.Errorf("single-package stdin = %q, want `!uv tool install 'ruff'...`", stdin)
	}
	if !strings.HasSuffix(stdin, "&& echo "+uvInstallMarker+"\n") {
		t.Errorf("stdin missing marker suffix:\n%s", stdin)
	}

	chained := uvInstallStdin([]string{"ruff", "typos"})
	if !strings.Contains(chained, "uv tool install 'ruff' && uv tool install 'typos'") {
		t.Errorf("multi-package stdin not chained with &&:\n%s", chained)
	}

	// A hostile package name must travel as one quoted argument: the only
	// && separators are the ones joining install steps and the marker.
	evil := uvInstallStdin([]string{"x' && evil"})
	want := "uv tool install " + shellQuote("x' && evil") + " && echo " + uvInstallMarker
	if !strings.Contains(evil, want) {
		t.Errorf("stdin missing single quoted argument:\n%s", evil)
	}
}
