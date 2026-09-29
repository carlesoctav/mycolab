package cmd

import (
	"strings"
	"testing"
)

func TestFixHostsLocalhostIPv4(t *testing.T) {
	broken := "127.0.0.1\tlocalhost\n::1\tlocalhost ip6-localhost ip6-loopback\nff02::1\tip6-allnodes\n"
	fixed, changed := FixHostsLocalhostIPv4(broken)
	if !changed {
		t.Fatal("broken hosts: changed=false, want true")
	}
	if strings.Contains(fixed, "::1\tlocalhost") || strings.Contains(fixed, "::1 localhost") {
		t.Errorf("localhost still on ::1 line:\n%s", fixed)
	}
	for _, want := range []string{"127.0.0.1", "localhost", "ip6-localhost", "ff02::1\tip6-allnodes\n"} {
		if !strings.Contains(fixed, want) {
			t.Errorf("fixed hosts missing %q:\n%s", want, fixed)
		}
	}
}

func TestFixHostsLocalhostIPv4Idempotent(t *testing.T) {
	ok := "127.0.0.1\tlocalhost\n::1\tip6-localhost ip6-loopback\n"
	fixed, changed := FixHostsLocalhostIPv4(ok)
	if changed || fixed != ok {
		t.Errorf("already-fixed hosts rewritten: changed=%v:\n%s", changed, fixed)
	}
}

func TestFixHostsLocalhostIPv4Comments(t *testing.T) {
	in := "# leading comment\n::1 localhost # v6 loopback\n127.0.0.1 mybox # v4\n"
	fixed, changed := FixHostsLocalhostIPv4(in)
	if !changed {
		t.Fatal("changed=false, want true")
	}
	for _, want := range []string{"# leading comment", "# v6 loopback", "# v4", "127.0.0.1 mybox localhost"} {
		if !strings.Contains(fixed, want) {
			t.Errorf("fixed hosts missing %q:\n%s", want, fixed)
		}
	}
	if strings.Contains(fixed, "::1 localhost") {
		t.Errorf("localhost still on ::1 line:\n%s", fixed)
	}
}

func TestFixHostsLocalhostIPv4BareV6(t *testing.T) {
	fixed, changed := FixHostsLocalhostIPv4("::1 localhost\n")
	if !changed {
		t.Fatal("changed=false, want true")
	}
	if !strings.Contains(fixed, "::1 ip6-localhost ip6-loopback") {
		t.Errorf("bare ::1 not given fallback names:\n%s", fixed)
	}
	if !strings.Contains(fixed, "127.0.0.1 localhost") {
		t.Errorf("missing 127.0.0.1 line not prepended:\n%s", fixed)
	}
}
