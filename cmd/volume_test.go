package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMountDetailed(t *testing.T) {
	cases := []struct {
		in          string
		wantBucket  string
		wantRemote  string
		wantRO      bool
		expectError bool
	}{
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
		},
		{
			in:         "carlesoctav/multi-serverasync-grpo:/content/lora",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
		},
		{
			in:         "carlesoctav/multi-serverasync-grpo:/content/lora:ro",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     true,
		},
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora:ro",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     true,
		},
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora:rw",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
		},
		{
			in:         "hf://carlesoctav/multi-serverasync-grpo:/content/lora",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
		},
		{
			in:         "me/data/sub:/content/data/",
			wantBucket: "me/data/sub",
			wantRemote: "/content/data",
			wantRO:     false,
		},
		{
			in:         "me/data/sub:/content/data/:ro",
			wantBucket: "me/data/sub",
			wantRemote: "/content/data",
			wantRO:     true,
		},
		{in: "nocolon", expectError: true},
		{in: "data:/x", expectError: true},
		{in: "me/d;rm:/x", expectError: true},
		{in: "me/d:rel", expectError: true},
		{in: "me/d:/x:foo", expectError: true},
		{in: ":/content/data", expectError: true},
		{in: "me/data:", expectError: true},
	}

	for _, c := range cases {
		m, err := parseMount(c.in)
		if c.expectError {
			if err == nil {
				t.Errorf("parseMount(%q) expected error, got %+v", c.in, m)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMount(%q) unexpected error: %v", c.in, err)
			continue
		}
		if m.Bucket != c.wantBucket || m.Remote != c.wantRemote || m.ReadOnly != c.wantRO {
			t.Errorf("parseMount(%q) = %+v, want Bucket=%q Remote=%q ReadOnly=%v",
				c.in, m, c.wantBucket, c.wantRemote, c.wantRO)
		}
	}
}

func TestMountScriptReadOnly(t *testing.T) {
	sRW := mountSpec{Bucket: "me/data", Remote: "/content/data", ReadOnly: false}.mountScript()
	if !strings.Contains(sRW, "hf-mount start --fuse") || strings.Contains(sRW, "--read-only") {
		t.Errorf("script (rw) unexpected: %q", sRW)
	}

	sRO := mountSpec{Bucket: "me/data", Remote: "/content/data", ReadOnly: true}.mountScript()
	if !strings.Contains(sRO, "hf-mount start --fuse --read-only") {
		t.Errorf("script (ro) missing --fuse --read-only: %q", sRO)
	}
	if !strings.Contains(sRO, "mkdir -p '/content/data'") {
		t.Errorf("script (ro) missing mkdir: %q", sRO)
	}
}

func TestMountVolumes(t *testing.T) {
	origRun := runSessionCommand
	defer func() { runSessionCommand = origRun }()

	var executed []string
	runSessionCommand = func(session, script string) ([]byte, error) {
		executed = append(executed, session+":"+script)
		return []byte("mounted ok"), nil
	}

	mounts := []mountSpec{
		{Bucket: "user/b1", Remote: "/content/b1", ReadOnly: false},
		{Bucket: "user/b2", Remote: "/content/b2", ReadOnly: true},
	}

	if err := mountVolumes("test-session", mounts); err != nil {
		t.Fatalf("mountVolumes failed: %v", err)
	}

	if len(executed) != 2 {
		t.Fatalf("executed %d commands, want 2", len(executed))
	}
	if !strings.HasPrefix(executed[0], "test-session:mkdir -p '/content/b1'") || strings.Contains(executed[0], "--read-only") {
		t.Errorf("command 0 unexpected: %s", executed[0])
	}
	if !strings.HasPrefix(executed[1], "test-session:mkdir -p '/content/b2'") || !strings.Contains(executed[1], "--read-only") {
		t.Errorf("command 1 unexpected: %s", executed[1])
	}

	// Test error handling
	runSessionCommand = func(session, script string) ([]byte, error) {
		return []byte("failed to start daemon"), errors.New("exit status 1")
	}

	err := mountVolumes("test-session", mounts[:1])
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to start daemon") {
		t.Errorf("error %q does not contain output", err.Error())
	}
}
