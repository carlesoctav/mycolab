package cmd

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGhosttyTerminfoEmbedded(t *testing.T) {
	if ghosttyTerminfo == "" {
		t.Fatal("embedded xterm-ghostty.terminfo is empty")
	}
	if !strings.Contains(ghosttyTerminfo, "xterm-ghostty") {
		t.Error("embedded xterm-ghostty.terminfo lost expected content (want `xterm-ghostty`)")
	}
}

func TestTerminfoPushStdinRoundTrip(t *testing.T) {
	stdin := terminfoPushStdin()
	if !strings.HasPrefix(stdin, "!mkdir -p ~/.terminfo 2>/dev/null; echo '") {
		t.Fatalf("stdin %q: want `!mkdir -p ~/.terminfo 2>/dev/null; echo '...` form", stdin[:35])
	}
	if !strings.HasSuffix(stdin, "&& echo "+terminfoMarker+"\n") {
		t.Fatalf("stdin missing marker suffix:\n%s", stdin)
	}
	start := strings.Index(stdin, "echo '") + len("echo '")
	end := strings.Index(stdin[start:], "'")
	if end < 0 {
		t.Fatalf("no closing quote in:\n%s", stdin)
	}
	raw, err := base64.StdEncoding.DecodeString(stdin[start : start+end])
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if !strings.Contains(string(raw), "xterm-ghostty") {
		t.Error("decoded payload does not contain xterm-ghostty")
	}
}
