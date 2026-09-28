package cmd

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestTmuxConfEmbedded(t *testing.T) {
	if tmuxConf == "" {
		t.Fatal("embedded tmux.conf is empty")
	}
	if !strings.Contains(tmuxConf, "mouse on") {
		t.Error("embedded tmux.conf lost expected content (want `mouse on`)")
	}
}

func TestTmuxPushStdinRoundTrip(t *testing.T) {
	stdin := tmuxPushStdin()
	if !strings.HasPrefix(stdin, "!echo '") {
		t.Fatalf("stdin %q: want `!echo '...` shell form", stdin[:20])
	}
	if !strings.Contains(stdin, tmuxConfRemotePath) || !strings.HasSuffix(stdin, "&& echo "+tmuxPushMarker+"\n") {
		t.Fatalf("stdin missing remote path or marker:\n%s", stdin)
	}
	start := len("!echo '")
	end := strings.Index(stdin[start:], "'")
	if end < 0 {
		t.Fatalf("no closing quote in:\n%s", stdin)
	}
	raw, err := base64.StdEncoding.DecodeString(stdin[start : start+end])
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if string(raw) != tmuxConf {
		t.Error("decoded payload differs from embedded tmux.conf")
	}
}
