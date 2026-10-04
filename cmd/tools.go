package cmd

import (
	"fmt"
	"os/exec"
)

// Base CLI tools installed on the runtime by `mycolab new`: fd and rg
// (replacing the webi.sh installs now that webi is down), jq, and the
// latest stable neovim via AppImage (same approach as
// ubuntu-tasks/nvim.sh, minus its trailing interactive `nvim` call, and
// tracking the stable release instead of a pinned version). The target is
// Ubuntu and exec runs as root, so plain apt-get/mv are used (no sudo).
const (
	nvimAppImageURL = "https://github.com/neovim/neovim/releases/download/stable/nvim-linux-x86_64.appimage"
	toolsMarker     = "BASETOOLS_OK"
)

// toolsInstallStdin builds the `colab exec` stdin that ensures the base
// tools exist. When rg, jq, nvim and fd/fdfind are all already present it
// is a no-op; otherwise apt installs the packages (libfuse2 best-effort,
// for running the AppImage), the nvim AppImage lands in /usr/local/bin,
// fdfind is aliased to fd, and nvim itself is executed as a final gate
// before the marker.
func toolsInstallStdin() string {
	return "!(command -v rg && command -v jq && command -v nvim && (command -v fd || command -v fdfind)) >/dev/null 2>&1 || " +
		"(apt-get update -qq && " +
		"DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl fd-find ripgrep jq && " +
		"(DEBIAN_FRONTEND=noninteractive apt-get install -y -qq libfuse2 || true) && " +
		"curl -fsSL -o /usr/local/bin/nvim " + nvimAppImageURL + " && " +
		"chmod +x /usr/local/bin/nvim && " +
		"(command -v fd >/dev/null 2>&1 || ln -sf \"$(command -v fdfind)\" /usr/local/bin/fd) && " +
		"nvim --version >/dev/null 2>&1) && echo " + toolsMarker + "\n"
}

// pushBaseTools installs the base CLI tools on the session's runtime.
// Best-effort by design: the Host block is already written at this point,
// so any failure only warns and never fails `mycolab new`.
func pushBaseTools(session string, sessionKnown bool) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping base tools install.\n", session)
		return
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		fmt.Println("Note: skipping base tools install (colab binary not found).")
		return
	}
	if _, err := runColabForMarkerTimeouts(colabBin, toolsInstallStdin(), toolsMarker, execLongCallTimeout, execLongRemoteTimeout, "exec", "-s", session); err != nil {
		fmt.Printf("Note: base tools install failed (%v); install by hand: `apt-get install -y fd-find ripgrep jq` + nvim AppImage %s.\n", err, nvimAppImageURL)
		return
	}
	fmt.Println("Base tools ready on runtime (fd, rg, jq, nvim).")
}
