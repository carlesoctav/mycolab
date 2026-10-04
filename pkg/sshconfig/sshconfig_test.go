package sshconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlesoctav/mycolab/pkg/sshconfig"
)

func TestEnsureGlobalInclude(t *testing.T) {
	const line = "Include ~/.ssh/colab_config"
	cases := []struct {
		name    string
		content string
		want    string
		changed bool
	}{
		{
			"alreadyGlobal",
			"Include ~/.ssh/colab_config\nHost a\n    User u\n",
			"Include ~/.ssh/colab_config\nHost a\n    User u\n",
			false,
		},
		{
			// An Include after the last Host block is scoped to it and
			// never applies to other hosts: it must move to the top.
			"scopedOnlyMovesToTop",
			"Host a\n    User u\nInclude ~/.ssh/colab_config\n",
			"Include ~/.ssh/colab_config\nHost a\n    User u\n",
			true,
		},
		{
			"missingInsertedBeforeFirstHost",
			"# comment\nServerAliveInterval 60\nHost a\n    User u\n",
			"# comment\nServerAliveInterval 60\nInclude ~/.ssh/colab_config\nHost a\n    User u\n",
			true,
		},
		{
			"missingNoBlocks",
			"ServerAliveInterval 60\n",
			"ServerAliveInterval 60\nInclude ~/.ssh/colab_config\n",
			true,
		},
		{
			"emptyFile",
			"",
			"Include ~/.ssh/colab_config\n",
			true,
		},
		{
			"commentedDoesNotCount",
			"# Include ~/.ssh/colab_config\nHost a\n",
			"# Include ~/.ssh/colab_config\nInclude ~/.ssh/colab_config\nHost a\n",
			true,
		},
		{
			"globalWinsScopedDupLeftAlone",
			"Include ~/.ssh/colab_config\nHost a\n    User u\nInclude ~/.ssh/colab_config\n",
			"Include ~/.ssh/colab_config\nHost a\n    User u\nInclude ~/.ssh/colab_config\n",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := sshconfig.EnsureGlobalInclude(tc.content, line, "colab_config")
			if got != tc.want || changed != tc.changed {
				t.Fatalf("EnsureGlobalInclude = (%q, %v), want (%q, %v)", got, changed, tc.want, tc.changed)
			}
		})
	}
}

func TestHostNames(t *testing.T) {
	got := sshconfig.HostNames("Host free 8\n    HostName x\n# Host commented\nHost *\nHost neg-!x ok\nMatch all\n    HostName y\n")
	want := []string{"free", "8", "ok"}
	if len(got) != len(want) {
		t.Fatalf("HostNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HostNames = %v, want %v", got, want)
		}
	}
}

func TestLoadHostNames(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "config")
	extra := filepath.Join(dir, "extra.conf")
	other := filepath.Join(dir, "other.conf")
	if err := os.WriteFile(extra, []byte("Host from-extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("Host from-other\nInclude extra.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("Include extra.conf other.conf missing-*.conf\nHost main-host\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := sshconfig.LoadHostNames(main)
	if err != nil {
		t.Fatalf("LoadHostNames = %v", err)
	}
	want := map[string]bool{"main-host": true, "from-extra": true, "from-other": true}
	if len(got) != len(want) {
		t.Fatalf("LoadHostNames = %v, want %v", got, want)
	}
	for _, h := range got {
		if !want[h] {
			t.Fatalf("LoadHostNames = %v, want %v", got, want)
		}
	}
	// A missing file yields no names and no error; an include cycle ends.
	if got, err := sshconfig.LoadHostNames(filepath.Join(dir, "nope")); err != nil || len(got) != 0 {
		t.Fatalf("LoadHostNames(missing) = %v, %v; want empty, nil", got, err)
	}
	cycle := filepath.Join(dir, "cycle.conf")
	if err := os.WriteFile(cycle, []byte("Host cycled\nInclude cycle.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := sshconfig.LoadHostNames(cycle); err != nil || len(got) != 1 || got[0] != "cycled" {
		t.Fatalf("LoadHostNames(cycle) = %v, %v; want [cycled], nil", got, err)
	}
}

func TestHasInclude(t *testing.T) {
	if !sshconfig.HasInclude("Host a\n    User u\nInclude ~/.ssh/colab_config\n", "colab_config") {
		t.Fatal("HasInclude = false, want true")
	}
	if sshconfig.HasInclude("Host x\n", "colab_config") {
		t.Fatal("HasInclude without Include = true, want false")
	}
	if sshconfig.HasInclude("# Include ~/.ssh/colab_config\n", "colab_config") {
		t.Fatal("HasInclude with commented Include = true, want false")
	}
}
