package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// The run pipeline. One orchestrator (execJob) does the same thing wherever
// it executes: locally for `mycolab run`, or on the always-on server for
// `mycolab server run` (which stages the dirs on the server and then invokes
// `mycolab run --staged` there over ssh).
//
//	new session -> mount hf buckets -> stage dirs -> rsync stage to colab
//	-> run the job over ssh (log captured) -> clean the stage dir
//
// A job's final step is just an argv executed on the runtime (job.Command).
// A future `server agent <name> -- <instruction file>` reuses everything
// above and only differs in how Command is built (the agent CLI invoked on
// the instruction file, which is staged like any other dir/file); tools and
// agent credentials are expected to already be on the runtime/server.

// dirMap maps a local (or already-staged) dir onto a runtime dir.
type dirMap struct{ Local, Remote string }

// mountSpec mounts an HF bucket at a runtime dir (via hf-mount).
type mountSpec struct{ Bucket, Remote string }

// sidecarSpec represents an auxiliary background command running in a tmux window.
type sidecarSpec struct{ Name, Command string }

// job is one `run` invocation.
type job struct {
	Session    string
	ID         string
	Dirs       []dirMap
	Mounts     []mountSpec
	Sidecars   []sidecarSpec
	Env        []string
	Command    []string
	Timeout    time.Duration
	Persistent bool
	Reuse      bool
	// Staged means Dirs[].Local are already copied into this job's stage
	// dir (done by `server run`), so staging is skipped.
	Staged bool
}

// runRoot is where stage dirs and logs live: .mycolab/run under the
// current directory, so each project keeps its own runs next to the code.
func runRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "", fmt.Errorf("unable to determine current directory: %w", err)
	}
	return filepath.Join(cwd, ".mycolab", "run"), nil
}

func (j *job) stageDir(root string) string { return filepath.Join(root, j.Session+"_"+j.ID) }
func (j *job) logPath(root string) string {
	return filepath.Join(root, j.Session+"_"+j.ID+".log")
}

func newRunID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// parseDirMap parses "local_dir:colab_dir". The colab dir must be absolute.
func parseDirMap(s string) (dirMap, error) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return dirMap{}, fmt.Errorf("invalid --dir %q (want local_dir:colab_dir)", s)
	}
	local, remote := s[:i], s[i+1:]
	if !strings.HasPrefix(remote, "/") {
		return dirMap{}, fmt.Errorf("invalid --dir %q: colab dir %q must be an absolute path", s, remote)
	}
	abs, err := filepath.Abs(local)
	if err != nil {
		return dirMap{}, err
	}
	return dirMap{Local: abs, Remote: strings.TrimRight(remote, "/")}, nil
}

// parseMount parses "hf_bucket:colab_dir" (bucket is user/name[/subpath]).
func parseMount(s string) (mountSpec, error) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return mountSpec{}, fmt.Errorf("invalid -v %q (want hf_bucket:colab_dir)", s)
	}
	bucket, remote := s[:i], s[i+1:]
	bucket = strings.TrimPrefix(bucket, "hf://buckets/")
	bucket = strings.TrimPrefix(bucket, "hf://")
	if !strings.Contains(bucket, "/") || strings.ContainsAny(bucket, " \t\r\n'\"\\$`;&|<>()") {
		return mountSpec{}, fmt.Errorf("invalid -v %q: bucket must look like user/name", s)
	}
	if !strings.HasPrefix(remote, "/") {
		return mountSpec{}, fmt.Errorf("invalid -v %q: colab dir %q must be an absolute path", s, remote)
	}
	return mountSpec{Bucket: bucket, Remote: strings.TrimRight(remote, "/")}, nil
}

// rsyncIgnoreFilters returns the rsync filter args for a source directory,
// respecting .gitignore if present.
func rsyncIgnoreFilters(dir string) []string {
	gitignore := filepath.Join(dir, ".gitignore")
	if st, err := os.Stat(gitignore); err == nil && !st.IsDir() {
		return []string{"--filter=:- .gitignore"}
	}
	return nil
}

// setupScript builds the remote shell script that prepares the run: it
// creates the run dir, kills any stale 'mycolab' tmux session, launches
// the main command in window 0 with an exit sentinel
// (/tmp/mycolab_run/<id>.done), and opens sidecar windows. It exits once
// everything is launched; log streaming is watcherScript's job, so a
// dropped tunnel can resume the stream without re-running setup.
func (j *job) setupScript() string {
	runDir := fmt.Sprintf("/tmp/mycolab_run/%s", j.ID)
	logFile := fmt.Sprintf("%s/out.log", runDir)
	doneFile := fmt.Sprintf("%s/done", runDir)

	workDir := ""
	if len(j.Dirs) > 0 {
		workDir = j.Dirs[0].Remote
	}

	var mainCmd string
	if len(j.Command) == 1 {
		mainCmd = j.Command[0]
	} else {
		q := make([]string, len(j.Command))
		for i, a := range j.Command {
			q[i] = shellQuote(a)
		}
		mainCmd = strings.Join(q, " ")
	}

	var envPrefix string
	if len(j.Env) > 0 {
		local := parseEnvList(j.Env)
		if len(local) > 0 {
			var exports []string
			for k, v := range local {
				exports = append(exports, fmt.Sprintf("%s=%s", k, shellQuote(v)))
			}
			sort.Strings(exports)
			envPrefix = "export " + strings.Join(exports, " ") + " && "
		}
	}

	if workDir != "" {
		mainCmd = "cd " + shellQuote(workDir) + " && " + mainCmd
	}
	if envPrefix != "" {
		mainCmd = envPrefix + mainCmd
	}

	steps := []string{
		fmt.Sprintf("mkdir -p %s", shellQuote(runDir)),
		fmt.Sprintf("rm -f %s %s", shellQuote(logFile), shellQuote(doneFile)),
		fmt.Sprintf("touch %s", shellQuote(logFile)),
		// Kill any stale tmux session named 'mycolab'.
		"tmux kill-session -t mycolab 2>/dev/null || true",
	}

	// Start window 0: main command.
	wrappedMain := fmt.Sprintf("( %s ) 2>&1 | tee %s; echo ${PIPESTATUS[0]} > %s",
		mainCmd, shellQuote(logFile), shellQuote(doneFile))
	steps = append(steps, fmt.Sprintf("tmux new-session -d -s mycolab -n main %s", shellQuote(wrappedMain)))

	// Start sidecar windows.
	for i, sc := range j.Sidecars {
		winName := sc.Name
		if winName == "" {
			winName = fmt.Sprintf("sidecar-%d", i+1)
		}
		cmd := sc.Command
		if workDir != "" {
			cmd = "cd " + shellQuote(workDir) + " && " + cmd
		}
		if envPrefix != "" {
			cmd = envPrefix + cmd
		}
		steps = append(steps, fmt.Sprintf("tmux new-window -t mycolab -n %s %s", shellQuote(winName), shellQuote(cmd)))
	}

	return strings.Join(steps, " && ")
}

// watcherScript builds the remote shell script that streams the run log
// and waits for completion: it tails out.log from 1-based byte offset
// fromByte (a resumed stream skips already-received bytes), waits for the
// .done sentinel (or the tmux session to vanish), then exits with the
// job's exit code. A remote 255 is mapped to 254 because ssh itself uses
// 255 for transport failures, and the supervisor tells the two apart.
func (j *job) watcherScript(fromByte int64) string {
	if fromByte < 1 {
		fromByte = 1
	}
	runDir := fmt.Sprintf("/tmp/mycolab_run/%s", j.ID)
	logFile := fmt.Sprintf("%s/out.log", runDir)
	doneFile := fmt.Sprintf("%s/done", runDir)
	return fmt.Sprintf(`( tail -c +%d -f %s &
TAIL_PID=$!
while [ ! -f %s ]; do
  if ! tmux has-session -t mycolab 2>/dev/null; then
    break
  fi
  sleep 1
done
sleep 2
kill $TAIL_PID 2>/dev/null || true
wait $TAIL_PID 2>/dev/null || true
if [ -f %s ]; then
  EXIT_CODE=$(cat %s)
  if [ "$EXIT_CODE" -eq 255 ]; then EXIT_CODE=254; fi
  exit $EXIT_CODE
else
  exit 1
fi )`, fromByte, shellQuote(logFile), shellQuote(doneFile), shellQuote(doneFile), shellQuote(doneFile))
}

// tmuxRunScript is the single-shot form (setup plus a from-start
// watcher), kept for tests; runPipeline runs the supervised split form.
func (j *job) tmuxRunScript() string {
	return j.setupScript() + " && " + j.watcherScript(1)
}

// mountScript mounts a bucket on the runtime. HF_TOKEN reaches ssh sessions
// via `mycolab env HF_TOKEN <token>`.
func (m mountSpec) mountScript() string {
	return fmt.Sprintf(`mkdir -p %s && hf-mount start ${HF_TOKEN:+--hf-token "$HF_TOKEN"} bucket %s %s`,
		shellQuote(m.Remote), m.Bucket, shellQuote(m.Remote))
}

// parseSidecar parses "[name:]command".
func parseSidecar(s string) sidecarSpec {
	if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], " ") {
		return sidecarSpec{Name: s[:i], Command: s[i+1:]}
	}
	return sidecarSpec{Command: s}
}

func bindRunFlags(c *cobra.Command) {
	c.Flags().StringArray("dir", nil, "local_dir:colab_dir to copy to the runtime (repeatable; the first is the working dir)")
	c.Flags().StringArrayP("volume", "v", nil, "hf_bucket:colab_dir to mount on the runtime with hf-mount (repeatable)")
	c.Flags().StringArrayP("sidecar", "S", nil, "auxiliary command to run in a tmux window (repeatable, format [name:]cmd)")
	c.Flags().Duration("timeout", 0, "stop the session (colab stop) if the command runs longer than this (e.g. 2h)")
	c.Flags().Bool("no-daemon", false, "run in the foreground and stream the log to this terminal")
	c.Flags().Bool("persistent", false, "keep the staged copy of the dirs after the run")
	c.Flags().Bool("reuse", false, "use the existing session instead of creating a new one")
	c.Flags().String("id", "", "run id (internal)")
	c.Flags().Bool("staged", false, "dirs are already staged (internal)")
	c.Flags().Bool("detached", false, "log to file only (internal)")
	for _, f := range []string{"id", "staged", "detached"} {
		_ = c.Flags().MarkHidden(f)
	}
}

func isEnvAssign(s string) bool {
	i := strings.Index(s, "=")
	if i <= 0 {
		return false
	}
	key := s[:i]
	for idx, r := range key {
		if idx == 0 && (r >= '0' && r <= '9') {
			return false
		}
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// jobFromFlags builds a job from run flags and the args after `--`.
func jobFromFlags(cmd *cobra.Command, args []string) (*job, error) {
	session, err := requireSession(cmd)
	if err != nil {
		return nil, err
	}
	dash := cmd.ArgsLenAtDash()
	if dash != 0 || len(args) == 0 {
		return nil, fmt.Errorf("missing command: put it after `--`, e.g. `mycolab %s -s %s --dir .:/content/proj -- python train.py`", cmd.Name(), session)
	}
	j := &job{Session: session, Command: args}
	j.Env, _ = cmd.Flags().GetStringArray("env")
	for len(j.Command) > 0 && isEnvAssign(j.Command[0]) {
		j.Env = append(j.Env, j.Command[0])
		j.Command = j.Command[1:]
	}
	if len(j.Command) == 0 {
		return nil, fmt.Errorf("missing command: put it after `--`, e.g. `mycolab %s -s %s --dir .:/content/proj -- python train.py`", cmd.Name(), session)
	}
	dirs, _ := cmd.Flags().GetStringArray("dir")
	for _, d := range dirs {
		m, err := parseDirMap(d)
		if err != nil {
			return nil, err
		}
		j.Dirs = append(j.Dirs, m)
	}
	vols, _ := cmd.Flags().GetStringArray("volume")
	for _, v := range vols {
		m, err := parseMount(v)
		if err != nil {
			return nil, err
		}
		j.Mounts = append(j.Mounts, m)
	}
	sidecars, _ := cmd.Flags().GetStringArray("sidecar")
	for _, s := range sidecars {
		j.Sidecars = append(j.Sidecars, parseSidecar(s))
	}
	j.Timeout, _ = cmd.Flags().GetDuration("timeout")
	if j.Timeout < 0 {
		return nil, fmt.Errorf("--timeout must be positive")
	}
	j.Persistent, _ = cmd.Flags().GetBool("persistent")
	j.Reuse, _ = cmd.Flags().GetBool("reuse")
	j.Staged, _ = cmd.Flags().GetBool("staged")
	j.ID, _ = cmd.Flags().GetString("id")
	if j.ID == "" {
		if j.ID, err = newRunID(); err != nil {
			return nil, err
		}
	}
	return j, nil
}

var runCmd = &cobra.Command{
	Use:   "run [flags] -- <command>",
	Short: "Create a session, copy dirs, and run a command on it (local orchestration)",
	Long: `Create a Colab session, mount HF buckets, copy local dirs to it and
run a command, capturing the log:

    mycolab run -s trainer --gpu L4 --dir ./proj:/content/proj \
        -v myuser/data:/content/data -- python train.py

The dirs are rsynced directly to the runtime respecting .gitignore patterns.
The log goes to ./.mycolab/run/<session>_<id>.log. By default the job is detached
(it prints the id and log path); --no-daemon streams the log here instead.
The ssh tunnel is supervised: drops re-establish over the multiplex master
and the log resumes, instead of failing the run.
--timeout stops the session if the command overruns. Accelerator flags
mirror 'colab new'. Buckets need hf and hf-mount on the runtime (installed
by 'mycolab new'; set HF_TOKEN with 'mycolab env HF_TOKEN <token>').
'mycolab server run' stages dirs on the server and runs everything there.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		j, err := jobFromFlags(cmd, args)
		if err != nil {
			return err
		}
		if _, err := requireActiveProfile(); err != nil {
			return err
		}
		root, err := runRoot()
		if err != nil {
			return err
		}
		noDaemon, _ := cmd.Flags().GetBool("no-daemon")
		detached, _ := cmd.Flags().GetBool("detached")
		if !noDaemon && !detached {
			return spawnDetached(j, root)
		}
		return execJob(cmd, j, root, detached)
	},
}

func init() {
	bindAcceleratorFlags(runCmd)
	bindSSHSetupFlags(runCmd)
	bindRunFlags(runCmd)
	rootCmd.AddCommand(runCmd)
}

// spawnDetached re-executes this command as a detached child that logs to
// the job's log file, then returns immediately.
func spawnDetached(j *job, root string) error {
	if err := os.MkdirAll(filepath.Dir(j.logPath(root)), 0o755); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string{}, os.Args[1:]...)
	extra := []string{"--detached", "--id", j.ID}
	for i, a := range args {
		if a == "--" {
			args = append(append(append([]string{}, args[:i]...), extra...), args[i:]...)
			extra = nil
			break
		}
	}
	args = append(args, extra...)
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	_ = c.Process.Release()
	fmt.Printf("Run %s started (session %s).\nLog: %s\nFollow: tail -f %s\n", j.ID, j.Session, j.logPath(root), j.logPath(root))
	return nil
}

// execJob runs the pipeline in the foreground. detached sends all output to
// the log file only; otherwise it is also streamed to the terminal.
func execJob(cmd *cobra.Command, j *job, root string, detached bool) error {
	if err := os.MkdirAll(filepath.Dir(j.logPath(root)), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(j.logPath(root), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	var out io.Writer = logFile
	if detached {
		os.Stdout, os.Stderr = logFile, logFile
	} else {
		out = io.MultiWriter(os.Stdout, logFile)
		fmt.Fprintf(os.Stdout, "Run %s (session %s), log: %s\n", j.ID, j.Session, j.logPath(root))
	}
	logf := func(format string, a ...any) {
		fmt.Fprintf(out, "[mycolab run %s] %s\n", j.ID, fmt.Sprintf(format, a...))
	}
	stage := j.stageDir(root)
	if !j.Persistent {
		defer func() {
			if _, err := os.Stat(stage); err == nil {
				_ = os.RemoveAll(stage)
				logf("cleaned %s", stage)
			}
		}()
	}

	err = runPipeline(cmd, j, out, logf)
	if err != nil {
		logf("failed: %v", err)
	} else {
		logf("done")
	}
	return err
}

func runPipeline(cmd *cobra.Command, j *job, out io.Writer, logf func(string, ...any)) error {
	colabBin, err := exec.LookPath("colab")
	if err != nil {
		return fmt.Errorf("colab binary not found in PATH")
	}
	if !j.Reuse {
		logf("creating session %s", j.Session)
		if err := runLogged(context.Background(), out, colabBin, colabNewArgs(cmd, j.Session)...); err != nil {
			return err
		}
		dropMaster(j.Session)
		if err := runSSHSetup(cmd, j.Session); err != nil {
			return err
		}
	} else if len(j.Env) > 0 {
		logf("syncing environment variables")
		syncRuntimeEnv(j.Session, true, j.Env)
	}
	// One multiplex master carries every ssh/rsync call below (plus any
	// interactive shell the user opens mid-run) over Colab's single
	// bridge slot; drops re-establish automatically from here on.
	if err := ensureMaster(context.Background(), j.Session, logf, setupMasterBudget); err != nil {
		return err
	}
	for _, m := range j.Mounts {
		logf("mounting hf bucket %s at %s", m.Bucket, m.Remote)
		if err := runLogged(context.Background(), out, "ssh", j.Session, m.mountScript()); err != nil {
			return fmt.Errorf("mount %s: %w", m.Bucket, err)
		}
	}
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil && len(j.Dirs) > 0 {
		return fmt.Errorf("rsync not found in PATH")
	}
	for _, d := range j.Dirs {
		logf("copying %s -> %s:%s", d.Local, j.Session, d.Remote)
		args := append([]string{"-az"}, rsyncIgnoreFilters(d.Local)...)
		args = append(args, "-e", "ssh",
			"--rsync-path", "mkdir -p "+shellQuote(d.Remote)+" && rsync",
			strings.TrimRight(d.Local, "/")+"/", j.Session+":"+d.Remote+"/")
		if err := runSSHLogged(context.Background(), out, logf, j.Session, rsyncBin, args...); err != nil {
			return fmt.Errorf("copy %s: %w", d.Local, err)
		}
	}

	ctx := context.Background()
	if j.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, j.Timeout)
		defer cancel()
	}
	logf("running in tmux session 'mycolab' (attach with `ssh %s -t tmux a -t mycolab`): %s", j.Session, strings.Join(j.Command, " "))
	if err := runSSHLogged(ctx, out, logf, j.Session, "ssh", j.Session, j.setupScript()); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	err = j.streamWithResume(ctx, out, logf)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		logf("timeout after %s; stopping session %s", j.Timeout, j.Session)
		_ = runLogged(context.Background(), out, "ssh", j.Session, "tmux kill-session -t mycolab 2>/dev/null || true")
		if serr := runLogged(context.Background(), out, colabBin, "stop", "-s", j.Session); serr != nil {
			logf("colab stop failed: %v", serr)
		}
		dropMaster(j.Session)
		return fmt.Errorf("timed out after %s (session stopped)", j.Timeout)
	}
	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

// runLogged runs bin with stdout/stderr going to out.
func runLogged(ctx context.Context, out io.Writer, bin string, args ...string) error {
	c := exec.CommandContext(ctx, bin, args...)
	c.Stdout = out
	c.Stderr = out
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", filepath.Base(bin), strings.Join(args, " "), err)
	}
	return nil
}
