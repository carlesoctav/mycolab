package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func accelTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	bindAcceleratorFlags(c)
	return c
}

func TestColabNewArgs(t *testing.T) {
	got := colabNewArgs(accelTestCmd(), "trainer")
	if len(got) != 3 || got[0] != "new" || got[1] != "-s" || got[2] != "trainer" {
		t.Fatalf("colabNewArgs(plain) = %v, want [new -s trainer]", got)
	}
	full := accelTestCmd()
	_ = full.Flags().Set("gpu", "L4")
	_ = full.Flags().Set("high-mem", "true")
	got = colabNewArgs(full, "trainer")
	want := []string{"new", "-s", "trainer", "--gpu", "L4", "--high-mem"}
	if len(got) != len(want) {
		t.Fatalf("colabNewArgs(full) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("colabNewArgs(full) = %v, want %v", got, want)
		}
	}
}

// The ssh setup flags must exist on every command that runs the setup:
// 'prepare' itself (hidden; regression: they used to be registered in
// scattered inits), 'new', 'server new', and 'connect'.
func TestSSHSetupFlagsPresent(t *testing.T) {
	for name, c := range map[string]*cobra.Command{
		"prepare":    prepareCmd,
		"new":        newCmd,
		"server new": serverNewCmd,
		"connect":    connectCmd,
	} {
		for _, f := range sshSetupFlagNames {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%s: missing --%s flag", name, f)
			}
		}
	}
	for name, c := range map[string]*cobra.Command{
		"prepare": prepareCmd,
		"new":     newCmd,
		"connect": connectCmd,
	} {
		if c.Flags().Lookup("volume") == nil {
			t.Errorf("%s: missing --volume flag", name)
		}
	}
	for name, c := range map[string]*cobra.Command{"new": newCmd, "server new": serverNewCmd} {
		for _, f := range []string{"gpu", "tpu", "high-mem"} {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%s: missing --%s flag", name, f)
			}
		}
	}
}

func TestSSHSetupPassthrough(t *testing.T) {
	c := &cobra.Command{Use: "test"}
	bindSSHSetupFlags(c)
	if got := sshSetupPassthrough(c); len(got) != 0 {
		t.Fatalf("sshSetupPassthrough(defaults) = %v, want empty", got)
	}
	_ = c.Flags().Set("no-env-sync", "true")
	_ = c.Flags().Set("no-tools", "true")
	_ = c.Flags().Set("podman", "true")
	_ = c.Flags().Set("no-all", "true")
	got := sshSetupPassthrough(c)
	want := []string{"--no-env-sync", "--no-tools", "--podman", "--no-all"}
	if len(got) != len(want) {
		t.Fatalf("sshSetupPassthrough = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sshSetupPassthrough = %v, want %v", got, want)
		}
	}
}

func TestVolumeFlagsPresent(t *testing.T) {
	for name, c := range map[string]*cobra.Command{
		"new":        newCmd,
		"server new": serverNewCmd,
	} {
		f := c.Flags().Lookup("volume")
		if f == nil {
			t.Errorf("%s: missing --volume flag", name)
		} else if f.Shorthand != "v" {
			t.Errorf("%s: --volume shorthand = %q, want -v", name, f.Shorthand)
		}
	}
}

func TestForwardFlagsPresent(t *testing.T) {
	for _, flagName := range []string{"local-forward", "remote-forward"} {
		f := newCmd.Flags().Lookup(flagName)
		if f == nil {
			t.Errorf("newCmd: missing --%s flag", flagName)
		}
	}
	if newCmd.Flags().ShorthandLookup("L") == nil {
		t.Errorf("newCmd: missing -L shorthand")
	}
	if newCmd.Flags().ShorthandLookup("R") == nil {
		t.Errorf("newCmd: missing -R shorthand")
	}
}

func TestRunColabNewWithRetrySucceedsAfterFailures(t *testing.T) {
	origAttempt := runColabNewAttempt
	origDelay := colabNewRetryDelay
	t.Cleanup(func() {
		runColabNewAttempt = origAttempt
		colabNewRetryDelay = origDelay
	})

	attempts := 0
	var delayCalls []int
	runColabNewAttempt = func(ctx context.Context, colabBin string, args []string) error {
		attempts++
		if attempts < 3 {
			return errors.New("timed out")
		}
		return nil
	}
	colabNewRetryDelay = func(n int) time.Duration {
		delayCalls = append(delayCalls, n)
		return time.Millisecond
	}

	if err := runColabNewWithRetry(context.Background(), "colab", []string{"new", "-s", "trainer"}); err != nil {
		t.Fatalf("runColabNewWithRetry() = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(delayCalls) != 2 || delayCalls[0] != 1 || delayCalls[1] != 2 {
		t.Fatalf("delayCalls = %v, want [1 2]", delayCalls)
	}
}

func TestRunColabNewWithRetryStopsOnCancel(t *testing.T) {
	origAttempt := runColabNewAttempt
	origDelay := colabNewRetryDelay
	t.Cleanup(func() {
		runColabNewAttempt = origAttempt
		colabNewRetryDelay = origDelay
	})

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	runColabNewAttempt = func(ctx context.Context, colabBin string, args []string) error {
		attempts++
		return errors.New("failed")
	}
	colabNewRetryDelay = func(n int) time.Duration {
		cancel()
		return time.Hour
	}

	err := runColabNewWithRetry(ctx, "colab", []string{"new", "-s", "trainer"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runColabNewWithRetry() = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
