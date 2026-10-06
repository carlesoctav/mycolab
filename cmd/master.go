package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SSH multiplexing plus supervision for `run`.
//
// Colab allows a single concurrent proxy connection per runtime, so every
// ssh/rsync invocation for a session shares one multiplex master (via the
// ControlMaster/ControlPath entry prepare.go writes). The master is also
// the liveness anchor: ServerAlive probes bound dead-peer detection, and
// the stream supervisor below re-establishes a dropped tunnel and resumes
// the log from the last received byte instead of failing the run.

const (
	// sshTransportExit is the exit code ssh itself uses for connection
	// failures, distinct from the remote command's 0-254 pass-through.
	// (The watcher maps a remote 255 to 254 so the two never collide.)
	sshTransportExit = 255
	// setupMasterBudget caps master (re-)establishment for short phases
	// (mounts, copies, job setup).
	setupMasterBudget = 2 * time.Minute
	// masterCreateTimeout bounds one master creation attempt: the
	// ProxyCommand handshake has no ssh-level timeout of its own.
	masterCreateTimeout = 90 * time.Second
)

// streamRetryBudget caps how long the log supervisor keeps re-establishing
// a dropped tunnel before giving up — well under Colab's ~20 minute idle
// prune, so a truly dead runtime still fails the run instead of hanging
// until --timeout. A variable (not a constant) so tests can shrink it.
var streamRetryBudget = 10 * time.Minute

// errSessionMissing marks failures caused by the session not existing in
// the profile: the bridge refuses to auto-create it, so retrying is
// pointless and callers must fail fast.
var errSessionMissing = errors.New("session does not exist (not auto-created)")

// masterAlive reports whether a multiplex master is running for host
// (ssh resolves the socket from the managed Host entry).
func masterAlive(host string) bool {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return false
	}
	return exec.Command(sshBin, "-O", "check", host).Run() == nil
}

// backoffForAttempt returns the delay before reconnect attempt n (1-based):
// exponential from 2s, capped at 30s.
func backoffForAttempt(n int) time.Duration {
	d := 2 * time.Second
	for i := 1; i < n && d < 30*time.Second; i++ {
		d *= 2
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// tryEnsureMaster performs one check-or-create round: reuse a live master,
// else clear stale state and start one (it becomes master via the managed
// ControlMaster auto entry, which also replaces a stale socket). A verify
// round follows creation, so losing the single bridge slot (HTTP 429 to a
// differently-wired holder) surfaces here and the caller backs off — while
// a winner on the shared socket is simply adopted on the next check.
func tryEnsureMaster(ctx context.Context, host string) error {
	if masterAlive(host) {
		return nil
	}
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh binary not found in PATH")
	}
	_ = exec.Command(sshBin, "-O", "exit", host).Run() // best-effort stale cleanup
	createCtx, cancel := context.WithTimeout(ctx, masterCreateTimeout)
	defer cancel()
	var errBuf strings.Builder
	c := exec.CommandContext(createCtx, sshBin, "-fN", host)
	c.Stderr = &errBuf
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if strings.Contains(msg, "refusing to auto-create") {
			return fmt.Errorf("start master for %q: %w (run `mycolab new -s %s` first)", host, errSessionMissing, host)
		}
		if msg != "" {
			return fmt.Errorf("start master for %q: %v (%s)", host, err, firstLine(msg))
		}
		return fmt.Errorf("start master for %q: %w", host, err)
	}
	if !masterAlive(host) {
		return fmt.Errorf("master for %q did not come up", host)
	}
	return nil
}

// ensureMaster reuses or creates the multiplex master for host, retrying
// with backoff until budget (or ctx) runs out. A missing session fails
// fast via errSessionMissing instead of burning the budget.
func ensureMaster(ctx context.Context, host string, logf func(string, ...any), budget time.Duration) error {
	deadline := time.Now().Add(budget)
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := tryEnsureMaster(ctx, host); err == nil {
			if attempt > 1 {
				logf("ssh master for %q re-established", host)
			}
			return nil
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(lastErr, errSessionMissing) {
			return lastErr
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no ssh master for %q after %s: %v", host, budget, lastErr)
		}
		d := backoffForAttempt(attempt)
		logf("ssh master for %q unavailable (%v); retrying in %s", host, lastErr, d)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
}

// runSSHLogged runs bin+args (an ssh/rsync invocation for session),
// retrying once after re-establishing the master when the first attempt
// fails. Mounts, copies and job setup are short phases, so one retry
// suffices; the long stream phase uses streamWithResume instead.
func runSSHLogged(ctx context.Context, out io.Writer, logf func(string, ...any), session, bin string, args ...string) error {
	err := runLogged(ctx, out, bin, args...)
	if err == nil || ctx.Err() != nil {
		return err
	}
	if merr := ensureMaster(ctx, session, logf, setupMasterBudget); merr != nil {
		if ctx.Err() != nil || errors.Is(merr, errSessionMissing) {
			return merr
		}
		logf("master re-establish failed: %v", merr)
		return err
	}
	logf("transport failed once; master re-established, retrying")
	return runLogged(ctx, out, bin, args...)
}

// countingWriter counts bytes written through to w.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// lockedWriter serializes writes. ssh attaches one copy goroutine per
// stream, and sharing a bare writer between them is unsafe: besides torn
// interleaves, io.Copy dispatches to methods like bytes.Buffer.ReadFrom,
// which reslices based on a stale length and can discard the other
// stream's bytes. Wrapping also hides such methods from io.Copy.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// sshExitCode extracts ssh's exit code from err: 0 on success, -1 when the
// process was signaled or never ran.
func sshExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func dropReason(code int) string {
	if code == -1 {
		return "ssh process killed"
	}
	return fmt.Sprintf("exit %d", code)
}

// streamAttempt runs one watcher from 1-based byte offset fromByte,
// streaming the remote log to out. It returns the bytes streamed by this
// attempt and ssh's exit code.
func (j *job) streamAttempt(ctx context.Context, out io.Writer, fromByte int64) (received int64, code int, err error) {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return 0, -1, err
	}
	safe := &lockedWriter{w: out}
	cw := &countingWriter{w: safe}
	c := exec.CommandContext(ctx, sshBin, j.Session, j.watcherScript(fromByte))
	c.Stdout = cw
	c.Stderr = safe
	err = c.Run()
	return cw.n, sshExitCode(err), err
}

// streamWithResume streams the remote log, surviving tunnel drops: on an
// ssh transport failure (exit 255, or a killed ssh) it re-establishes the
// master and re-runs the watcher from the next unread byte. A remote
// failure (any other non-zero exit — the job's own code) ends the stream
// without retry. The master is deliberately left running at the end:
// ControlPersist reaps it, and closing it here would kill interactive
// shells the user attached mid-run.
func (j *job) streamWithResume(ctx context.Context, out io.Writer, logf func(string, ...any)) error {
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("ssh binary not found in PATH")
	}
	var received int64
	var firstFailure time.Time
	for attempt := 1; ; attempt++ {
		n, code, err := j.streamAttempt(ctx, out, received+1)
		received += n
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if code != sshTransportExit && code != -1 {
			return fmt.Errorf("remote command for run %s exited with status %d", j.ID, code)
		}
		if firstFailure.IsZero() {
			firstFailure = time.Now()
		}
		remaining := streamRetryBudget - time.Since(firstFailure)
		if remaining <= 0 {
			return fmt.Errorf("ssh connection to %q down for over %s; giving up (the job may still run on the runtime; reattach with `ssh %s -t tmux a -t mycolab`)", j.Session, streamRetryBudget, j.Session)
		}
		logf("ssh to %q dropped (%s) after %d log bytes; re-establishing", j.Session, dropReason(code), received)
		d := backoffForAttempt(attempt)
		if d > remaining {
			d = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		if rest := remaining - d; rest > 0 {
			if merr := ensureMaster(ctx, j.Session, logf, rest); merr != nil {
				if ctx.Err() != nil || errors.Is(merr, errSessionMissing) {
					return merr
				}
				logf("master re-establish failed: %v", merr)
			}
		}
	}
}
