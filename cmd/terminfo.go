package cmd

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

//go:embed xterm-ghostty.terminfo
var ghosttyTerminfo string

const terminfoMarker = "TERMINFO_OK"

// terminfoPayload gathers terminfo entries to upload: the embedded
// xterm-ghostty definition, plus any local custom definition for $TERM
// if infocmp is available locally.
func terminfoPayload() string {
	var sb strings.Builder
	sb.WriteString(ghosttyTerminfo)
	sb.WriteString("\n")

	localTerm := os.Getenv("TERM")
	if localTerm != "" && localTerm != "xterm-ghostty" && localTerm != "ghostty" &&
		localTerm != "xterm-256color" && localTerm != "xterm" && localTerm != "dumb" {
		if infocmpBin, err := exec.LookPath("infocmp"); err == nil {
			if out, err := exec.Command(infocmpBin, "-x", localTerm).Output(); err == nil && len(out) > 0 {
				sb.WriteString(string(out))
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func terminfoPushStdin() string {
	payload := terminfoPayload()
	enc := base64.StdEncoding.EncodeToString([]byte(payload))
	return fmt.Sprintf("!mkdir -p ~/.terminfo 2>/dev/null; echo '%s' | base64 -d | tic -x - 2>/dev/null && echo %s\n", enc, terminfoMarker)
}

// pushTerminfo uploads terminal terminfo definitions (like xterm-ghostty)
// to the runtime so full-screen tools like tmux, htop, and nvim work seamlessly over SSH.
func pushTerminfo(session string, sessionKnown bool) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping terminfo sync.\n", session)
		return
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		fmt.Println("Note: skipping terminfo sync (colab binary not found).")
		return
	}
	if _, err := runColabForMarker(colabBin, terminfoPushStdin(), terminfoMarker, "exec", "-s", session); err != nil {
		fmt.Printf("Note: terminfo sync failed (%v); compile manually via `infocmp -x xterm-ghostty | ssh %s 'tic -x -'`.\n", err, session)
		return
	}
	fmt.Println("Synced terminal terminfo (xterm-ghostty) to runtime.")
}
