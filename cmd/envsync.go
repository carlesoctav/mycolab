package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/carlesoctav/mycolab/pkg/profile"
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
	// never wedge `mycolab new` forever.
	colabCallTimeout = 120 * time.Second
	// colabRemoteTimeout bounds execution on the runtime itself; passed
	// through to `colab exec` (which defaults to 30s).
	colabRemoteTimeout = "100"
	// execLongCallTimeout and execLongRemoteTimeout bound the slow steps
	// (apt installs, tool downloads) that outlast the defaults above.
	execLongCallTimeout   = 360 * time.Second
	execLongRemoteTimeout = "300"
)

// runColab runs the colab CLI with the given stdin, returning combined output.
func runColab(colabBin, stdin string, args ...string) (string, error) {
	return runColabTimeout(colabBin, stdin, colabCallTimeout, args...)
}

func runColabTimeout(colabBin, stdin string, timeout time.Duration, args ...string) (string, error) {
	return runColabTimeouts(colabBin, stdin, timeout, colabRemoteTimeout, args...)
}

func runColabTimeouts(colabBin, stdin string, timeout time.Duration, remoteTimeout string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "exec" {
		args = append(args, "--timeout", remoteTimeout)
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
	return runColabForMarkerTimeouts(colabBin, stdin, marker, colabCallTimeout, colabRemoteTimeout, args...)
}

// runColabForMarkerTimeouts is runColabForMarker with explicit local and
// remote timeouts, for steps (like apt installs) that outlast the defaults.
func runColabForMarkerTimeouts(colabBin, stdin, marker string, timeout time.Duration, remoteTimeout string, args ...string) (string, error) {
	var last string
	var err error
	for i := 0; i < colabExecAttempts; i++ {
		if i > 0 {
			time.Sleep(colabExecRetryDelay)
		}
		var out string
		out, err = runColabTimeouts(colabBin, stdin, timeout, remoteTimeout, args...)
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

// runEnvSyncScript executes a python script on session over SSH to sync the environment.
// A variable so tests can stub it.
var runEnvSyncScript = func(session, script string) ([]byte, error) {
	sshBin, err := findSSH()
	if err != nil {
		return nil, fmt.Errorf("ssh binary not found in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var args []string
	if alt := os.Getenv("MYCOLAB_SSH_CONFIG"); alt != "" {
		args = append(args, "-F", alt)
	}
	args = append(args, session, "python3 -")
	c := exec.CommandContext(ctx, sshBin, args...)
	c.Stdin = strings.NewReader(script)
	return c.CombinedOutput()
}

// syncRuntimeEnv captures the session runtime's environment directly from
// /proc/1/environ and jupyter-server over SSH, merges custom and local env vars
// (such as HF_TOKEN), and installs them as a managed sshd SetEnv block, so
// `ssh` sessions see accelerators and tokens. Best-effort by design:
// failures warn, never fail `mycolab new`.
func syncRuntimeEnv(session string, sessionKnown bool, extraEnvs []string) {
	if !sessionKnown {
		fmt.Printf("Note: session %q does not exist yet; skipping env sync.\n", session)
		return
	}
	custom, err := profile.CustomEnv()
	if err != nil {
		fmt.Printf("Note: could not read custom env (%v).\n", err)
		custom = map[string]string{}
	}
	if custom == nil {
		custom = map[string]string{}
	}
	if hfToken := os.Getenv("HF_TOKEN"); hfToken != "" {
		custom["HF_TOKEN"] = hfToken
	}

	toSync := make(map[string]string)
	for k, v := range custom {
		toSync[k] = v
	}
	if len(custom) > 0 {
		fmt.Printf("sync %d custom env: %s\n", len(custom), strings.Join(profile.CustomEnvNames(custom), ", "))
	}
	local := parseEnvList(extraEnvs)
	if len(local) > 0 {
		for k, v := range local {
			toSync[k] = v
		}
		localNames := make([]string, 0, len(local))
		for k := range local {
			localNames = append(localNames, k)
		}
		sort.Strings(localNames)
		fmt.Printf("sync %d local env: %s\n", len(local), strings.Join(localNames, ", "))
	}

	script, err := sshenv.BuildInVMSyncScript(toSync)
	if err != nil {
		fmt.Printf("Note: failed to generate env sync script (%v).\n", err)
		return
	}

	out, err := runEnvSyncScript(session, script)
	if err != nil || !strings.Contains(string(out), sshenv.InstallerMarker) {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			fmt.Printf("Note: env sync failed (%v). Output:\n%s\n", err, msg)
		} else {
			fmt.Printf("Note: env sync failed (%v).\n", err)
		}
		return
	}

	count := len(toSync)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, sshenv.InstallerMarker) {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				if n, err := strconv.Atoi(fields[1]); err == nil {
					count = n
				}
			}
			break
		}
	}

	fmt.Printf("Synced %d env vars to sshd; reconnect `ssh %s` to pick them up.\n", count, session)
}


// parseEnvList parses KEY=VAL or KEY (looked up in local env) entries.
func parseEnvList(entries []string) map[string]string {
	out := make(map[string]string)
	for _, entry := range entries {
		if i := strings.Index(entry, "="); i >= 0 {
			k := entry[:i]
			v := entry[i+1:]
			if k != "" {
				out[k] = v
			}
		} else if entry != "" {
			if v, ok := os.LookupEnv(entry); ok {
				out[entry] = v
			} else {
				fmt.Printf("Note: local env %q is not set; skipping.\n", entry)
			}
		}
	}
	return out
}
