package cmd

import (
	"strings"
	"testing"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

func serverTestCmd(value string) *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().String("server", "", "")
	if value != "" {
		_ = c.Flags().Set("server", value)
	}
	return c
}

func stubKnownHosts(t *testing.T, hosts []string) {
	t.Helper()
	orig := loadKnownSSHHosts
	loadKnownSSHHosts = func() ([]string, error) { return hosts, nil }
	t.Cleanup(func() { loadKnownSSHHosts = orig })
}

func TestServerSSHArgs(t *testing.T) {
	args := serverSSHArgs("free", []string{"mycolab", "list"}, false)
	if len(args) != 2 || args[0] != "free" {
		t.Fatalf("serverSSHArgs = %v, want [free <remote>]", args)
	}
	if !strings.HasPrefix(args[1], remotePATHPrefix+" ") {
		t.Fatalf("remote %q missing PATH prefix", args[1])
	}
	if !strings.Contains(args[1], "'mycolab' 'list'") {
		t.Fatalf("remote %q missing quoted command", args[1])
	}

	tty := serverSSHArgs("free", []string{"mycolab", "use"}, true)
	if len(tty) != 3 || tty[0] != "-t" || tty[1] != "free" {
		t.Fatalf("serverSSHArgs(tty) = %v, want [-t free <remote>]", tty)
	}

	// A hostile session name must travel as one quoted word: no new
	// shell separators may appear in the remote command.
	evil := serverSSHArgs("free", []string{"mycolab", "tool", "-s", "x' && evil", "muse"}, false)[1]
	if !strings.Contains(evil, shellQuote("x' && evil")) {
		t.Fatalf("remote %q missing single quoted argument", evil)
	}
	if strings.Contains(strings.ReplaceAll(evil, shellQuote("x' && evil"), ""), "&&") {
		t.Fatalf("remote %q leaks shell separators", evil)
	}
}

func TestResolveServerHost(t *testing.T) {
	newHome := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
	}

	t.Run("flagWins", func(t *testing.T) {
		newHome(t)
		stubKnownHosts(t, []string{"free", "other"})
		if err := profile.SetServer("other"); err != nil {
			t.Fatal(err)
		}
		got, err := resolveServerHost(serverTestCmd("free"))
		if err != nil || got != "free" {
			t.Fatalf("resolveServerHost(flag) = %q, %v; want free, nil", got, err)
		}
	})

	t.Run("storedUsed", func(t *testing.T) {
		newHome(t)
		stubKnownHosts(t, []string{"free"})
		if err := profile.SetServer("free"); err != nil {
			t.Fatal(err)
		}
		got, err := resolveServerHost(serverTestCmd(""))
		if err != nil || got != "free" {
			t.Fatalf("resolveServerHost(stored) = %q, %v; want free, nil", got, err)
		}
	})

	t.Run("noneSelected", func(t *testing.T) {
		newHome(t)
		stubKnownHosts(t, []string{"free"})
		if _, err := resolveServerHost(serverTestCmd("")); err == nil {
			t.Fatal("resolveServerHost() with no server = nil error, want error")
		}
	})

	t.Run("unknownFlagRejected", func(t *testing.T) {
		newHome(t)
		stubKnownHosts(t, []string{"free"})
		if _, err := resolveServerHost(serverTestCmd("nope")); err == nil {
			t.Fatal("resolveServerHost(unknown flag) = nil error, want error")
		}
	})

	t.Run("storedButRemovedFromConfig", func(t *testing.T) {
		newHome(t)
		stubKnownHosts(t, []string{"free"})
		if err := profile.SetServer("gone"); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveServerHost(serverTestCmd("")); err == nil {
			t.Fatal("resolveServerHost(stale stored) = nil error, want error")
		}
	})
}

func TestValidateServerHost(t *testing.T) {
	if err := validateServerHost([]string{"free"}, "free"); err != nil {
		t.Fatalf("validateServerHost(known) = %v, want nil", err)
	}
	err := validateServerHost([]string{"free"}, "nope")
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("validateServerHost(unknown) = %v, want error naming the host", err)
	}
	// The name lands in a generated ProxyCommand message and in argv, so
	// quotes, whitespace and shell metacharacters are rejected even when
	// the ssh config declares such a host.
	for _, bad := range []string{"", "-leading-dash", "has space", "quo'te", `dq"uote`, "dol$ar", "back`tick", "semi;colon", "hash#tag", `back\slash`, "star*", "q?", "bang!", "pi|pe", "am&per", "par(en)", "lt<gt"} {
		if err := validateServerHost([]string{bad}, bad); err == nil {
			t.Errorf("validateServerHost(%q) = nil, want error", bad)
		}
	}
}
