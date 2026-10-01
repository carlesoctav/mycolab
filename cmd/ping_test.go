package cmd

import (
	"testing"

	"github.com/carlesoctav/mycolab/pkg/profile"
)

func TestPingEndpoint(t *testing.T) {
	sessions := []profile.Session{
		{Name: "a", Endpoint: "ep-a"},
		{Name: "b", Endpoint: ""},
	}
	if ep, err := pingEndpoint(sessions, "a"); err != nil || ep != "ep-a" {
		t.Errorf("pingEndpoint(a) = %q, %v; want ep-a, nil", ep, err)
	}
	if _, err := pingEndpoint(sessions, "b"); err == nil {
		t.Error("pingEndpoint(empty endpoint): nil error, want error")
	}
	if _, err := pingEndpoint(sessions, "nope"); err == nil {
		t.Error("pingEndpoint(missing session): nil error, want error")
	}
	if _, err := pingEndpoint(nil, "a"); err == nil {
		t.Error("pingEndpoint(nil sessions): nil error, want error")
	}
}
