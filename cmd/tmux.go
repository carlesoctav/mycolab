package cmd

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os/exec"
)

//go:embed tmux.conf
var tmuxConf string

// tmuxConfRemotePath is where the bundled config lands on the runtime
// (ssh sessions run as root, so root's tmux servers pick it up).
const tmuxConfRemotePath = "/root/.tmux.conf"

// tmuxPushMarker is echoed by the remote command so we can confirm the
// write went through (exec mixes notices into stdout, so look for this).
const tmuxPushMarker = "TMUXCONF_OK"

// tmuxPushStdin builds the `colab exec` stdin that writes the bundled
// tmux.conf to the remote. The file rides base64-encoded on a single line
// so multi-line content survives exec's text channel intact.
func tmuxPushStdin() string {
	enc := base64.StdEncoding.EncodeToString([]byte(tmuxConf))
	return fmt.Sprintf("!echo '%s' | base64 -d > %s && echo %s\n", enc, tmuxConfRemotePath, tmuxPushMarker)
}

// pushTmuxConf uploads the bundled tmux.conf to the session's runtime.
// Best-effort by design: the Host block is already written at this point,
// so any failure only warns and never fails `mycolab new`.
func pushTmuxConf(session string, sessionKnown bool) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping tmux.conf push.\n", session)
		return
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		fmt.Println("Note: skipping tmux.conf push (colab binary not found).")
		return
	}
	if _, err := runColabForMarker(colabBin, tmuxPushStdin(), tmuxPushMarker, "exec", "-s", session); err != nil {
		fmt.Printf("Note: tmux.conf push failed (%v); upload by hand: `colab upload -s %s <file> %s`.\n", err, session, tmuxConfRemotePath)
		return
	}
	fmt.Printf("Pushed tmux.conf to %s (new tmux servers pick it up).\n", tmuxConfRemotePath)
}
