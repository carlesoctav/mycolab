package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func sessionTestCmd(value string) *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().StringP("session", "s", "", "")
	if value != "" {
		_ = c.Flags().Set("session", value)
	}
	return c
}

func TestRequireSession(t *testing.T) {
	got, err := requireSession(sessionTestCmd("trainer"))
	if err != nil || got != "trainer" {
		t.Errorf("requireSession(trainer) = %q, %v; want trainer, nil", got, err)
	}
	if _, err := requireSession(sessionTestCmd("")); err == nil {
		t.Error("requireSession(empty) = nil error, want missing-session error")
	}
	if _, err := requireSession(sessionTestCmd("has space")); err == nil {
		t.Error("requireSession(\"has space\") = nil error, want validation error")
	}
}

func TestResolveSyncHost(t *testing.T) {
	got, err := resolveSyncHost("", "trainer")
	if err != nil || got != "trainer" {
		t.Errorf("resolveSyncHost(\"\", trainer) = %q, %v; want trainer, nil", got, err)
	}
	got, err = resolveSyncHost("custom", "trainer")
	if err != nil || got != "custom" {
		t.Errorf("resolveSyncHost(custom, trainer) = %q, %v; want custom, nil", got, err)
	}
	if _, err := resolveSyncHost("", ""); err == nil {
		t.Error("resolveSyncHost(\"\", \"\") = nil error, want error")
	}
}
