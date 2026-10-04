package cmd

import (
	"testing"

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
// 'ssh' itself (hidden; regression: they used to be registered in
// scattered inits), 'new', and 'server new'.
func TestSSHSetupFlagsPresent(t *testing.T) {
	for name, c := range map[string]*cobra.Command{
		"ssh":        sshCmd,
		"new":        newCmd,
		"server new": serverNewCmd,
	} {
		for _, f := range []string{"no-prune", "no-tmux-sync", "no-env-sync", "no-hosts-fix", "no-tools"} {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%s: missing --%s flag", name, f)
			}
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
	got := sshSetupPassthrough(c)
	if len(got) != 2 || got[0] != "--no-env-sync" || got[1] != "--no-tools" {
		t.Fatalf("sshSetupPassthrough = %v, want [--no-env-sync --no-tools]", got)
	}
}
