package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stubSSH installs a fake ssh on PATH driven by files under a state dir:
//
//	check_code   - exit code for `-O check` (default 0)
//	create_code  - exit code for `-fN` (default 0; also flips check to 0)
//	mode         - remote-command behavior: drop_once | remote_fail |
//	               always_drop | ok (default)
//	calls        - appended one-line summary per invocation
//	n            - remote-command attempt counter
//	script_N     - recorded remote script per attempt
//
// STUB_CREATE_STDERR (env) is printed to stderr by `-fN`.
func stubSSH(t *testing.T, state map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	st := filepath.Join(dir, "state")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(st, 0o755); err != nil {
		t.Fatal(err)
	}
	for k, v := range state {
		if err := os.WriteFile(filepath.Join(st, k), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
if [ "$1" = "-O" ] && [ "$2" = "check" ]; then
  echo "-O check $3" >> "$STUB_STATE/calls"
  code=$(cat "$STUB_STATE/check_code" 2>/dev/null || echo 0)
  exit "$code"
fi
if [ "$1" = "-O" ] && [ "$2" = "exit" ]; then
  echo "-O exit $3" >> "$STUB_STATE/calls"
  exit 0
fi
if [ "$1" = "-fN" ]; then
  echo "-fN $2" >> "$STUB_STATE/calls"
  code=$(cat "$STUB_STATE/create_code" 2>/dev/null || echo 0)
  if [ "$code" = "0" ]; then echo 0 > "$STUB_STATE/check_code"; fi
  if [ -n "$STUB_CREATE_STDERR" ]; then printf '%s' "$STUB_CREATE_STDERR" >&2; fi
  exit "$code"
fi
n=$(cat "$STUB_STATE/n" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$STUB_STATE/n"
echo "RUN $1 attempt=$n" >> "$STUB_STATE/calls"
printf '%s' "$2" > "$STUB_STATE/script_$n"
mode=$(cat "$STUB_STATE/mode" 2>/dev/null || echo ok)
case "$mode" in
  drop_once)
    if [ "$n" = "1" ]; then printf 'PART1'; exit 255; fi
    printf 'PART2'; exit 0
    ;;
  remote_fail)
    printf 'FAILED'; exit 3
    ;;
  always_drop)
    exit 255
    ;;
  *)
    printf 'HELLO'; exit 0
    ;;
esac
`
	path := filepath.Join(bin, "ssh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_STATE", st)
	return st
}

func stubFile(t *testing.T, st, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(st, name))
	if err != nil {
		t.Fatalf("stub file %s: %v", name, err)
	}
	return string(b)
}

func discardLogf(string, ...any) {}

func TestBackoffForAttempt(t *testing.T) {
	for in, want := range map[int]time.Duration{
		0: 2 * time.Second, 1: 2 * time.Second, 2: 4 * time.Second,
		3: 8 * time.Second, 4: 16 * time.Second, 5: 30 * time.Second,
		6: 30 * time.Second, 100: 30 * time.Second,
	} {
		if got := backoffForAttempt(in); got != want {
			t.Errorf("backoffForAttempt(%d) = %s, want %s", in, got, want)
		}
	}
}

func TestSSHExitCode(t *testing.T) {
	shBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	if got := sshExitCode(nil); got != 0 {
		t.Errorf("sshExitCode(nil) = %d, want 0", got)
	}
	for _, code := range []int{1, 3, 255} {
		err := exec.Command(shBin, "-c", "exit "+strconv.Itoa(code)).Run()
		if got := sshExitCode(err); got != code {
			t.Errorf("sshExitCode(exit %d) = %d", code, got)
		}
	}
	err = exec.Command("mycolab-test-no-such-binary-xyz").Run()
	if got := sshExitCode(err); got != -1 {
		t.Errorf("sshExitCode(launch failure) = %d, want -1", got)
	}
}

func TestCountingWriter(t *testing.T) {
	var buf bytes.Buffer
	cw := &countingWriter{w: &buf}
	if _, err := cw.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := cw.Write([]byte("cde")); err != nil {
		t.Fatal(err)
	}
	if cw.n != 5 || buf.String() != "abcde" {
		t.Errorf("n=%d buf=%q, want n=5 buf=abcde", cw.n, buf.String())
	}
}

func TestEnsureMasterReusesAlive(t *testing.T) {
	st := stubSSH(t, map[string]string{"check_code": "0"})
	if err := ensureMaster(context.Background(), "sess", discardLogf, time.Minute); err != nil {
		t.Fatalf("ensureMaster: %v", err)
	}
	calls := stubFile(t, st, "calls")
	if !strings.Contains(calls, "-O check sess") {
		t.Errorf("no check call:\n%s", calls)
	}
	if strings.Contains(calls, "-fN") {
		t.Errorf("live master must not trigger creation:\n%s", calls)
	}
}

func TestEnsureMasterCreatesWhenDead(t *testing.T) {
	st := stubSSH(t, map[string]string{"check_code": "1"})
	if err := ensureMaster(context.Background(), "sess", discardLogf, time.Minute); err != nil {
		t.Fatalf("ensureMaster: %v", err)
	}
	calls := stubFile(t, st, "calls")
	if !strings.Contains(calls, "-fN sess") {
		t.Errorf("no creation call:\n%s", calls)
	}
}

func TestEnsureMasterMissingSessionFast(t *testing.T) {
	st := stubSSH(t, map[string]string{"check_code": "1", "create_code": "255"})
	t.Setenv("STUB_CREATE_STDERR", `Error: session "sess" is not in profile "main"; refusing to auto-create it`)
	start := time.Now()
	err := ensureMaster(context.Background(), "sess", discardLogf, 2*time.Minute)
	if err == nil {
		t.Fatal("nil error, want session-missing failure")
	}
	if !strings.Contains(err.Error(), "not auto-created") {
		t.Errorf("error = %q, want auto-create refusal", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("missing session must fail fast, took %s", elapsed)
	}
	if got := strings.Count(stubFile(t, st, "calls"), "-fN sess"); got != 1 {
		t.Errorf("-fN calls = %d, want exactly 1 (no pointless retries)", got)
	}
}

func TestStreamWithResumeResumesAfterDrop(t *testing.T) {
	st := stubSSH(t, map[string]string{"check_code": "0", "mode": "drop_once"})
	j := &job{Session: "sess", ID: "abc123"}
	// out is deliberately shared for stdout+stderr, like production: the
	// supervisor must serialize the two copy goroutines (see lockedWriter).
	var out bytes.Buffer
	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, format) }
	if err := j.streamWithResume(context.Background(), &out, logf); err != nil {
		t.Fatalf("streamWithResume: %v", err)
	}
	if out.String() != "PART1PART2" {
		t.Errorf("streamed %q, want PART1PART2", out.String())
	}
	// PART1 is 5 bytes, so the resume must tail from byte 6.
	script2 := stubFile(t, st, "script_2")
	if !strings.Contains(script2, "tail -c +6 -f") {
		t.Errorf("resume script missing offset tail:\n%s", script2)
	}
	if strings.TrimSpace(stubFile(t, st, "n")) != "2" {
		t.Errorf("attempts = %s, want 2", stubFile(t, st, "n"))
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "dropped") {
		t.Errorf("no drop logged: %q", joined)
	}
}

func TestStreamWithResumeRemoteFailureNoRetry(t *testing.T) {
	st := stubSSH(t, map[string]string{"check_code": "0", "mode": "remote_fail"})
	j := &job{Session: "sess", ID: "abc123"}
	var out bytes.Buffer
	err := j.streamWithResume(context.Background(), &out, discardLogf)
	if err == nil {
		t.Fatal("nil error, want remote failure")
	}
	if !strings.Contains(err.Error(), "status 3") {
		t.Errorf("error = %q, want exit status 3", err)
	}
	if out.String() != "FAILED" {
		t.Errorf("streamed %q, want FAILED", out.String())
	}
	if got := strings.TrimSpace(stubFile(t, st, "n")); got != "1" {
		t.Errorf("attempts = %s, want 1 (remote failures must not retry)", got)
	}
}

func TestStreamWithResumeGivesUp(t *testing.T) {
	old := streamRetryBudget
	streamRetryBudget = 1200 * time.Millisecond
	t.Cleanup(func() { streamRetryBudget = old })
	st := stubSSH(t, map[string]string{"check_code": "0", "mode": "always_drop"})
	j := &job{Session: "sess", ID: "abc123"}
	var out bytes.Buffer
	err := j.streamWithResume(context.Background(), &out, discardLogf)
	if err == nil {
		t.Fatal("nil error, want give-up failure")
	}
	if !strings.Contains(err.Error(), "giving up") {
		t.Errorf("error = %q, want give-up", err)
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(stubFile(t, st, "n")))
	if convErr != nil || n < 2 {
		t.Errorf("attempts = %q, want >= 2", stubFile(t, st, "n"))
	}
}
