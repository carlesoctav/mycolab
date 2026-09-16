package sshconfig_test

import (
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
