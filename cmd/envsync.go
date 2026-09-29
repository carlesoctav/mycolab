package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/carlesoctav/mycolab/pkg/sshenv"
)

// colab exec occasionally renders rich spinners/panels instead of command
// output (or returns before the kernel answers); every exec-based step
// below retries a few times to ride out those flakes.
const (
	colabExecAttempts   = 3
	colabExecRetryDelay = 3 * time.Second
	// colabCallTimeout bounds every colab subprocess call: the kernel
	// client has hung indefinitely before, and a stuck push step must
	// never wedge `mycolab ssh` forever.
	colabCallTimeout = 120 * time.Second
	// colabRemoteTimeout bounds execution on the runtime itself; passed
	// through to `colab exec` (which defaults to 30s).
	colabRemoteTimeout = "100"
)

// runColab runs the colab CLI with the given stdin, returning combined output.
func runColab(colabBin, stdin string, args ...string) (string, error) {
	return runColabTimeout(colabBin, stdin, colabCallTimeout, args...)
}

func runColabTimeout(colabBin, stdin string, timeout time.Duration, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "exec" {
		args = append(args, "--timeout", colabRemoteTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, colabBin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if os.Getenv("MYCOLAB_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "DEBUG colab %v stdin=%.60q out=%.400q err=%v\n", args, stdin, out.String(), err)
	}
	return out.String(), err
}

// runColabForMarker runs until marker appears in the output.
func runColabForMarker(colabBin, stdin, marker string, args ...string) (string, error) {
	var last string
	var err error
	for i := 0; i < colabExecAttempts; i++ {
		if i > 0 {
			time.Sleep(colabExecRetryDelay)
		}
		var out string
		out, err = runColab(colabBin, stdin, args...)
		if err == nil && strings.Contains(out, marker) {
			return out, nil
		}
		last = out
	}
	if err != nil {
		return last, err
	}
	return last, fmt.Errorf("marker %q not found", marker)
}

// captureKernelEnv dumps the runtime's kernel env via the exec door.
func captureKernelEnv(colabBin, session string) (map[string]string, error) {
	var err error
	for i := 0; i < colabExecAttempts; i++ {
		if i > 0 {
			time.Sleep(colabExecRetryDelay)
		}
		var out string
		out, err = runColab(colabBin, "!env -0 | base64 -w0\n", "exec", "-s", session)
		if err != nil {
			continue
		}
		var vars map[string]string
		vars, err = sshenv.DecodeCapture(out)
		if err == nil {
			return vars, nil
		}
	}
	return nil, err
}

func init() {
	sshCmd.Flags().Bool("no-env-sync", false, "skip syncing the runtime kernel env into sshd for ssh sessions")
}

// syncRuntimeEnv captures the session runtime's kernel environment (via the
// exec door, which carries the full container env) and installs it as a
// managed sshd SetEnv block, so `ssh` sessions see the same accelerators
// and tools as `colab console` / `colab exec`. Best-effort by design:
// failures warn, never fail `mycolab ssh`.
func syncRuntimeEnv(session string, sessionKnown bool) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping env sync (re-run `mycolab ssh -s %s` once it does).\n", session, session)
		return
	}
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		fmt.Println("Note: skipping env sync (colab binary not found).")
		return
	}
	vars, err := captureKernelEnv(colabBin, session)
	if err != nil {
		fmt.Printf("Note: env capture failed after %d attempts (%v); ssh sessions keep the bare sshd env.\n", colabExecAttempts, err)
		return
	}
	kept := map[string]string{}
	for k, v := range vars {
		if !sshenv.Denied(k) {
			kept[k] = v
		}
	}
	directive, skipped := sshenv.RenderSetEnv(kept)
	if directive == "" {
		fmt.Println("Note: no env vars to sync after filtering.")
		return
	}
	dir, err := os.MkdirTemp("", "mycolab-env")
	if err != nil {
		fmt.Printf("Note: env sync failed staging files (%v).\n", err)
		return
	}
	defer os.RemoveAll(dir)
	blockPath := filepath.Join(dir, "setenv_block")
	instPath := filepath.Join(dir, "sshd_install.sh")
	if err := os.WriteFile(blockPath, []byte(directive+"\n"), 0o644); err != nil {
		fmt.Printf("Note: env sync failed staging files (%v).\n", err)
		return
	}
	if err := os.WriteFile(instPath, []byte(sshenv.InstallerScript), 0o644); err != nil {
		fmt.Printf("Note: env sync failed staging files (%v).\n", err)
		return
	}
	if _, err := runColab(colabBin, "", "upload", "-s", session, blockPath, sshenv.RemoteBlockPath); err != nil {
		fmt.Printf("Note: env block upload failed (%v).\n", err)
		return
	}
	if _, err := runColab(colabBin, "", "upload", "-s", session, instPath, sshenv.RemoteInstallerPath); err != nil {
		fmt.Printf("Note: env installer upload failed (%v).\n", err)
		return
	}
	instOut, err := runColabForMarker(colabBin, "!bash "+sshenv.RemoteInstallerPath+"\n", sshenv.InstallerMarker, "exec", "-s", session)
	if err != nil {
		fmt.Printf("Note: env install failed (%v). Last output:\n%s\n", err, instOut)
		return
	}
	fmt.Printf("Synced %d env vars to sshd", len(kept)-len(skipped))
	if len(skipped) > 0 {
		fmt.Printf(" (%d skipped: newline values)", len(skipped))
	}
	fmt.Printf("; reconnect `ssh %s` to pick them up.\n", session)
}
