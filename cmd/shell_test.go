package cmd

import "testing"

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
