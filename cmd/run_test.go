package cmd

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseDirMap(t *testing.T) {
	d, err := parseDirMap("/tmp/proj:/content/proj/")
	if err != nil || d.Local != "/tmp/proj" || d.Remote != "/content/proj" {
		t.Fatalf("got %+v, %v", d, err)
	}
	for _, bad := range []string{"nocolon", "/tmp:rel", ":/x", "/tmp:"} {
		if _, err := parseDirMap(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestParseMount(t *testing.T) {
	m, err := parseMount("me/data/sub:/content/data")
	if err != nil || m.Bucket != "me/data/sub" || m.Remote != "/content/data" {
		t.Fatalf("got %+v, %v", m, err)
	}
	for _, bad := range []string{"data:/x", "me/d;rm:/x", "me/d:rel"} {
		if _, err := parseMount(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestTmuxRunScript(t *testing.T) {
	j := &job{
		ID:       "test1234",
		Dirs:     []dirMap{{Remote: "/content/p"}},
		Command:  []string{"python", "train.py"},
		Sidecars: []sidecarSpec{{Name: "server", Command: "python server.py"}},
	}
	s := j.tmuxRunScript()
	for _, want := range []string{
		"tmux new-session -d -s mycolab -n main",
		"tmux new-window -t mycolab -n 'server'",
		"train.py",
		"python server.py",
		"/tmp/mycolab_run/test1234/done",
		"/tmp/mycolab_run/test1234/out.log",
		"tail -c +1 -f",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
}

func TestTmuxRunScriptWithEnv(t *testing.T) {
	j := &job{
		ID:       "test1234",
		Dirs:     []dirMap{{Remote: "/content/p"}},
		Env:      []string{"FOO=bar", "BAZ=hello world"},
		Command:  []string{"python", "train.py"},
		Sidecars: []sidecarSpec{{Name: "server", Command: "python server.py"}},
	}
	s := j.tmuxRunScript()
	for _, want := range []string{
		"export BAZ=",
		"hello world",
		"FOO=",
		"bar",
		"python server.py",
		"/content/p",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
}

func TestIsEnvAssign(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"FOO=bar", true},
		{"A_B_C=123", true},
		{"_HIDDEN=val", true},
		{"VLLM_ALLOW_RUNTIME_LORA_UPDATING=1", true},
		{"1INVALID=foo", false},
		{"=no_key", false},
		{"no_equal", false},
		{"--flag=val", false},
		{"-e", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isEnvAssign(c.in); got != c.want {
			t.Errorf("isEnvAssign(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMountScript(t *testing.T) {
	s := mountSpec{Bucket: "me/data", Remote: "/content/data"}.mountScript()
	if !strings.Contains(s, "hf-mount start --fuse") || !strings.Contains(s, "bucket me/data '/content/data'") {
		t.Errorf("script: %q", s)
	}
}

func TestRsyncIgnoreFilters(t *testing.T) {
	tmp := t.TempDir()

	// 1. .gitignore does not exist
	filters := rsyncIgnoreFilters(tmp)
	if len(filters) != 0 {
		t.Errorf("expected no filters, got %v", filters)
	}

	// 2. .gitignore exists
	gitIgnorePath := tmp + "/.gitignore"
	if err := os.WriteFile(gitIgnorePath, []byte("node_modules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	filters = rsyncIgnoreFilters(tmp)
	if len(filters) != 1 || filters[0] != "--filter=:- .gitignore" {
		t.Errorf("expected gitignore filter, got %v", filters)
	}
}

func TestJobLogPaths(t *testing.T) {
	j := &job{Session: "trainer", ID: "abc123"}
	if got := j.logPath("/root"); got != "/root/trainer_abc123.log" {
		t.Errorf("logPath = %q", got)
	}
	if got := j.stageDir("/root"); got != "/root/trainer_abc123" {
		t.Errorf("stageDir = %q", got)
	}
}

func TestSetupAndWatcherScripts(t *testing.T) {
	j := &job{
		ID:       "test1234",
		Dirs:     []dirMap{{Remote: "/content/p"}},
		Command:  []string{"python", "train.py"},
		Sidecars: []sidecarSpec{{Name: "server", Command: "python server.py"}},
	}
	setup := j.setupScript()
	for _, want := range []string{
		"tmux new-session -d -s mycolab -n main",
		"tmux new-window -t mycolab -n 'server'",
		"/tmp/mycolab_run/test1234/done",
		"train.py",
	} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup missing %q:\n%s", want, setup)
		}
	}
	if strings.Contains(setup, "tail -c") {
		t.Errorf("setup must not stream (a resume would re-run setup):\n%s", setup)
	}
	w := j.watcherScript(6)
	for _, want := range []string{
		"tail -c +6 -f",
		"/tmp/mycolab_run/test1234/out.log",
		"/tmp/mycolab_run/test1234/done",
		`if [ "$EXIT_CODE" -eq 255 ]; then EXIT_CODE=254; fi`,
	} {
		if !strings.Contains(w, want) {
			t.Errorf("watcher missing %q:\n%s", w, want)
		}
	}
	if strings.Contains(j.watcherScript(0), "tail -c +0") {
		t.Errorf("watcher must clamp offsets to >= 1")
	}
	if !strings.Contains(j.tmuxRunScript(), "tail -c +1 -f") {
		t.Errorf("single-shot form must stream from the start")
	}
}

func TestJobForwardFlags(t *testing.T) {
	newCmd := func(runFn func(cmd *cobra.Command, args []string)) *cobra.Command {
		c := &cobra.Command{
			Use: "run [flags] -- <command>",
			Run: runFn,
		}
		c.Flags().StringP("session", "s", "", "")
		bindRunFlags(c)
		return c
	}

	t.Run("shortFlagsRepeatable", func(t *testing.T) {
		var jobGot *job
		var errGot error
		c := newCmd(func(cmd *cobra.Command, args []string) {
			jobGot, errGot = jobFromFlags(cmd, args)
		})
		c.SetArgs([]string{
			"-s", "test-sess",
			"-L", "8000:localhost:8000",
			"-L", "8001:localhost:8001",
			"-R", "8002:localhost:8002",
			"--", "python", "train.py",
		})
		if err := c.Execute(); err != nil {
			t.Fatalf("c.Execute(): %v", err)
		}
		if errGot != nil {
			t.Fatalf("jobFromFlags: %v", errGot)
		}
		wantL := []string{"8000:localhost:8000", "8001:localhost:8001"}
		if !reflect.DeepEqual(jobGot.LocalForwards, wantL) {
			t.Errorf("LocalForwards = %v, want %v", jobGot.LocalForwards, wantL)
		}
		wantR := []string{"8002:localhost:8002"}
		if !reflect.DeepEqual(jobGot.RemoteForwards, wantR) {
			t.Errorf("RemoteForwards = %v, want %v", jobGot.RemoteForwards, wantR)
		}
		wantArgs := []string{"-L", "8000:localhost:8000", "-L", "8001:localhost:8001", "-R", "8002:localhost:8002"}
		if !reflect.DeepEqual(jobGot.forwardArgs(), wantArgs) {
			t.Errorf("forwardArgs() = %v, want %v", jobGot.forwardArgs(), wantArgs)
		}
	})

	t.Run("longFlagsAndAliases", func(t *testing.T) {
		var jobGot *job
		var errGot error
		c := newCmd(func(cmd *cobra.Command, args []string) {
			jobGot, errGot = jobFromFlags(cmd, args)
		})
		c.SetArgs([]string{
			"-s", "test-sess",
			"--local-forward", "9000:localhost:9000",
			"--L", "9001:localhost:9001",
			"--remote-forward", "9002:localhost:9002",
			"--R", "9003:localhost:9003",
			"--", "echo", "hi",
		})
		if err := c.Execute(); err != nil {
			t.Fatalf("c.Execute(): %v", err)
		}
		if errGot != nil {
			t.Fatalf("jobFromFlags: %v", errGot)
		}
		wantL := []string{"9000:localhost:9000", "9001:localhost:9001"}
		if !reflect.DeepEqual(jobGot.LocalForwards, wantL) {
			t.Errorf("LocalForwards = %v, want %v", jobGot.LocalForwards, wantL)
		}
		wantR := []string{"9002:localhost:9002", "9003:localhost:9003"}
		if !reflect.DeepEqual(jobGot.RemoteForwards, wantR) {
			t.Errorf("RemoteForwards = %v, want %v", jobGot.RemoteForwards, wantR)
		}
	})

	t.Run("emptyForwardRejected", func(t *testing.T) {
		var errGot error
		c := newCmd(func(cmd *cobra.Command, args []string) {
			_, errGot = jobFromFlags(cmd, args)
		})
		c.SetArgs([]string{"-s", "test-sess", "-L", "", "--", "echo"})
		if err := c.Execute(); err != nil {
			t.Fatalf("c.Execute(): %v", err)
		}
		if errGot == nil {
			t.Error("expected error for empty -L, got nil")
		}

		c2 := newCmd(func(cmd *cobra.Command, args []string) {
			_, errGot = jobFromFlags(cmd, args)
		})
		c2.SetArgs([]string{"-s", "test-sess", "-R", "   ", "--", "echo"})
		if err := c2.Execute(); err != nil {
			t.Fatalf("c2.Execute(): %v", err)
		}
		if errGot == nil {
			t.Error("expected error for empty -R, got nil")
		}
	})
}

func TestBuildServerRunRemoteArgsForwards(t *testing.T) {
	c := &cobra.Command{Use: "run"}
	bindRunFlags(c)
	bindAcceleratorFlags(c)
	bindSSHSetupFlags(c)
	j := &job{
		Session:        "trainer",
		ID:             "xyz789",
		LocalForwards:  []string{"8000:localhost:8000", "8001:localhost:8001"},
		RemoteForwards: []string{"8002:localhost:8002"},
		Command:        []string{"python", "train.py"},
	}
	remote := buildServerRunRemoteArgs(c, j, nil)
	joined := strings.Join(remote, " ")
	for _, want := range []string{
		"-L 8000:localhost:8000",
		"-L 8001:localhost:8001",
		"-R 8002:localhost:8002",
		"-- python train.py",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("remote args missing %q:\n%s", want, joined)
		}
	}
}


