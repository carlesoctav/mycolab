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
		wantAW      bool
		expectError bool
	}{
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     false,
		},
		{
			in:         "carlesoctav/multi-serverasync-grpo:/content/lora",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     false,
		},
		{
			in:         "carlesoctav/multi-serverasync-grpo:/content/lora:ro",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     true,
			wantAW:     false,
		},
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora:ro",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     true,
			wantAW:     false,
		},
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora:rw",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     false,
		},
		{
			in:         "carlesoctav/multi-serverasync-grpo:/content/lora:aw",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     true,
		},
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora:aw",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     true,
		},
		{
			in:         "hf://buckets/carlesoctav/multi-serverasync-grpo:/content/lora:rw:aw",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     true,
		},
		{
			in:         "hf://carlesoctav/multi-serverasync-grpo:/content/lora",
			wantBucket: "carlesoctav/multi-serverasync-grpo",
			wantRemote: "/content/lora",
			wantRO:     false,
			wantAW:     false,
		},
		{
			in:         "me/data/sub:/content/data/",
			wantBucket: "me/data/sub",
			wantRemote: "/content/data",
			wantRO:     false,
			wantAW:     false,
		},
		{
			in:         "me/data/sub:/content/data/:ro",
			wantBucket: "me/data/sub",
			wantRemote: "/content/data",
			wantRO:     true,
			wantAW:     false,
		},
		{
			in:         "me/data/sub:/content/data/:aw",
			wantBucket: "me/data/sub",
			wantRemote: "/content/data",
			wantRO:     false,
			wantAW:     true,
		},
		{in: "carlesoctav/multi-serverasync-grpo:/content/lora:ro:aw", expectError: true},
		{in: "carlesoctav/multi-serverasync-grpo:/content/lora:aw:ro", expectError: true},
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
		if m.Bucket != c.wantBucket || m.Remote != c.wantRemote || m.ReadOnly != c.wantRO || m.AdvancedWrites != c.wantAW {
			t.Errorf("parseMount(%q) = %+v, want Bucket=%q Remote=%q ReadOnly=%v AdvancedWrites=%v",
				c.in, m, c.wantBucket, c.wantRemote, c.wantRO, c.wantAW)
		}
	}
}

func TestMountScriptReadOnly(t *testing.T) {
	sRW := mountSpec{Bucket: "me/data", Remote: "/content/data", ReadOnly: false}.mountScript()
	if !strings.Contains(sRW, "hf-mount start --fuse") || strings.Contains(sRW, "--read-only") || strings.Contains(sRW, "--advanced-writes") {
		t.Errorf("script (rw) unexpected: %q", sRW)
	}

	sRO := mountSpec{Bucket: "me/data", Remote: "/content/data", ReadOnly: true}.mountScript()
	if !strings.Contains(sRO, "hf-mount start --fuse --read-only") || strings.Contains(sRO, "--advanced-writes") {
		t.Errorf("script (ro) missing --fuse --read-only: %q", sRO)
	}
	if !strings.Contains(sRO, "mkdir -p '/content/data'") {
		t.Errorf("script (ro) missing mkdir: %q", sRO)
	}

	sAW := mountSpec{Bucket: "me/data", Remote: "/content/data", AdvancedWrites: true}.mountScript()
	if !strings.Contains(sAW, "hf-mount start --fuse --advanced-writes") || strings.Contains(sAW, "--read-only") {
		t.Errorf("script (aw) missing --fuse --advanced-writes: %q", sAW)
	}
	if !strings.Contains(sAW, "mkdir -p '/content/data'") {
		t.Errorf("script (aw) missing mkdir: %q", sAW)
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

	if err := mountVolumes("test-session", nil); err != nil {
		t.Fatalf("mountVolumes(empty) failed: %v", err)
	}
	if len(executed) != 0 {
		t.Fatalf("executed %d commands for empty mounts, want 0", len(executed))
	}

	mounts := []mountSpec{
		{Bucket: "user/b1", Remote: "/content/b1", ReadOnly: false},
		{Bucket: "user/b2", Remote: "/content/b2", ReadOnly: true},
		{Bucket: "user/b3", Remote: "/content/b3", AdvancedWrites: true},
	}

	if err := mountVolumes("test-session", mounts); err != nil {
		t.Fatalf("mountVolumes failed: %v", err)
	}

	if len(executed) != 4 {
		t.Fatalf("executed %d commands, want 4", len(executed))
	}
	if !strings.Contains(executed[0], "hf-mount") {
		t.Errorf("command 0 unexpected: %s", executed[0])
	}
	if !strings.HasPrefix(executed[1], "test-session:mkdir -p '/content/b1'") || strings.Contains(executed[1], "--read-only") || strings.Contains(executed[1], "--advanced-writes") {
		t.Errorf("command 1 unexpected: %s", executed[1])
	}
	if !strings.HasPrefix(executed[2], "test-session:mkdir -p '/content/b2'") || !strings.Contains(executed[2], "--read-only") {
		t.Errorf("command 2 unexpected: %s", executed[2])
	}
	if !strings.HasPrefix(executed[3], "test-session:mkdir -p '/content/b3'") || !strings.Contains(executed[3], "--advanced-writes") || strings.Contains(executed[3], "--read-only") {
		t.Errorf("command 3 unexpected: %s", executed[3])
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
