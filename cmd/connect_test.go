package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlesoctav/mycolab/pkg/colab"
	"github.com/carlesoctav/mycolab/pkg/profile"
)

func TestFindMatchingAssignment(t *testing.T) {
	assignments := []colab.Assignment{
		{
			Endpoint:    "gpu-a100-hm-ass1c0-5giphe4pf32o",
			Accelerator: "A100",
		},
		{
			Endpoint:    "cpu-vm-test123",
			Accelerator: "NONE",
		},
	}

	// 1. Exact endpoint match
	a, err := findMatchingAssignment(assignments, "gpu-a100-hm-ass1c0-5giphe4pf32o")
	if err != nil || a.Endpoint != "gpu-a100-hm-ass1c0-5giphe4pf32o" {
		t.Fatalf("findMatchingAssignment(exact) = %v, %v", a, err)
	}

	// 2. Colab tunnel URL match
	a, err = findMatchingAssignment(assignments, "https://colab.research.google.com/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o")
	if err != nil || a.Endpoint != "gpu-a100-hm-ass1c0-5giphe4pf32o" {
		t.Fatalf("findMatchingAssignment(tunnel url) = %v, %v", a, err)
	}

	// 3. Colab proxy URL match
	a, err = findMatchingAssignment(assignments, "https://8080-gpu-a100-hm-ass1c0-5giphe4pf32o-c.asia-southeast1.prod.r.colab.dev")
	if err != nil || a.Endpoint != "gpu-a100-hm-ass1c0-5giphe4pf32o" {
		t.Fatalf("findMatchingAssignment(proxy url) = %v, %v", a, err)
	}

	// 4. Missing endpoint
	_, err = findMatchingAssignment(assignments, "non-existent-endpoint")
	if err == nil {
		t.Fatal("findMatchingAssignment(non-existent) = nil, want error")
	}
}

func TestSelectAssignmentSingleOrphan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := profile.Create("work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profile.SetCurrent("work"); err != nil {
		t.Fatal(err)
	}

	assignments := []colab.Assignment{
		{
			Endpoint:    "ep-orphan",
			Accelerator: "A100",
		},
	}

	a, err := selectAssignment("work", assignments)
	if err != nil {
		t.Fatalf("selectAssignment(single orphan) failed: %v", err)
	}
	if a.Endpoint != "ep-orphan" {
		t.Fatalf("got %s, want ep-orphan", a.Endpoint)
	}
}

func TestSaveConnectedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := profile.Create("work")
	if err != nil {
		t.Fatal(err)
	}

	target := &colab.Assignment{
		Endpoint:     "gpu-ep1",
		Variant:      1,
		MachineShape: 1,
		Accelerator:  "A100",
		RuntimeProxyInfo: colab.RuntimeProxyInfo{
			Token:                 "tok123",
			TokenExpiresInSeconds: 3600,
			URL:                   "https://proxy.example",
		},
	}

	if err := saveConnectedSession("work", "trainer", target); err != nil {
		t.Fatalf("saveConnectedSession failed: %v", err)
	}

	sessions, err := profile.Sessions("work")
	if err != nil {
		t.Fatalf("profile.Sessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.Name != "trainer" || s.Endpoint != "gpu-ep1" || s.Accelerator != "A100" || s.Variant != "GPU" {
		t.Fatalf("unexpected session: %+v", s)
	}

	// Verify json file content on disk
	path, _ := profile.ProfilePath("work")
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Fatal("sessions file is empty")
	}

	// Update with another session
	target2 := &colab.Assignment{
		Endpoint:     "tpu-ep2",
		Variant:      2,
		MachineShape: 0,
		Accelerator:  "V5E1",
		RuntimeProxyInfo: colab.RuntimeProxyInfo{
			Token: "tok456",
			URL:   "https://proxy2.example",
		},
	}
	if err := saveConnectedSession("work", "eval", target2); err != nil {
		t.Fatalf("saveConnectedSession(second) failed: %v", err)
	}

	sessions, err = profile.Sessions("work")
	if err != nil {
		t.Fatalf("profile.Sessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].Name != "eval" || sessions[1].Name != "trainer" {
		t.Fatalf("expected sorted [eval, trainer], got %+v", sessions)
	}
	_ = filepath.Base(path)
}
