package cmd

import (
	"strings"
	"testing"
)

func TestUpsertSessionHostEmpty(t *testing.T) {
	updated, prevProfile, removedLegacy := upsertSessionHost("", "work", "trainer")
	if prevProfile != "" {
		t.Errorf("prevProfile = %q, want empty", prevProfile)
	}
	if removedLegacy {
		t.Error("removedLegacy = true, want false")
	}
	for _, want := range []string{
		"Managed by mycolab",
		"Host trainer\n",
		"ProxyCommand mycolab ssh -s trainer\n",
		"# Profile: work | Session: trainer",
	} {
		if !strings.Contains(updated, want) {
			t.Errorf("upsert missing %q:\n%s", want, updated)
		}
	}
	if strings.Contains(updated, "Host colab\n") {
		t.Errorf("upsert must not create legacy Host colab:\n%s", updated)
	}
}

func TestUpsertSessionHostPreservesOthers(t *testing.T) {
	first, _, _ := upsertSessionHost("", "work", "trainer")
	second, prevProfile, removedLegacy := upsertSessionHost(first, "work", "eval")
	if prevProfile != "" {
		t.Errorf("prevProfile = %q, want empty for new host", prevProfile)
	}
	if removedLegacy {
		t.Error("removedLegacy = true, want false")
	}
	for _, want := range []string{
		"Host trainer\n",
		"ProxyCommand mycolab ssh -s trainer\n",
		"Host eval\n",
		"ProxyCommand mycolab ssh -s eval\n",
	} {
		if !strings.Contains(second, want) {
			t.Errorf("upsert missing %q:\n%s", want, second)
		}
	}
}

func TestUpsertSessionHostSingleHeader(t *testing.T) {
	first, _, _ := upsertSessionHost("", "work", "trainer")
	second, _, _ := upsertSessionHost(first, "work", "eval")
	third, _, _ := upsertSessionHost(second, "work", "trainer")
	for name, content := range map[string]string{"first": first, "second": second, "third": third} {
		if got := strings.Count(content, "Managed by mycolab"); got != 1 {
			t.Errorf("%s upsert: header appears %d times, want 1:\n%s", name, got, content)
		}
	}
}

func TestUpsertSessionHostIdempotent(t *testing.T) {
	first, _, _ := upsertSessionHost("", "work", "trainer")
	second, prevProfile, _ := upsertSessionHost(first, "work", "trainer")
	if prevProfile != "work" {
		t.Errorf("prevProfile = %q, want work", prevProfile)
	}
	if got := strings.Count(second, "Host trainer\n"); got != 1 {
		t.Errorf("Host trainer appears %d times, want 1:\n%s", got, second)
	}
}

func TestUpsertSessionHostProfileChange(t *testing.T) {
	first, _, _ := upsertSessionHost("", "work", "trainer")
	second, prevProfile, _ := upsertSessionHost(first, "personal", "trainer")
	if prevProfile != "work" {
		t.Errorf("prevProfile = %q, want work", prevProfile)
	}
	if !strings.Contains(second, "# Profile: personal | Session: trainer") {
		t.Errorf("profile comment not updated:\n%s", second)
	}
	if strings.Contains(second, "# Profile: work | Session: trainer") {
		t.Errorf("stale profile comment kept:\n%s", second)
	}
}

func TestUpsertSessionHostRemovesLegacyColab(t *testing.T) {
	legacy := "# Managed by mycolab — regenerated on every 'mycolab ssh' run, manual edits will be lost.\n" +
		"# Profile: work | Session: trainer\n" +
		"Host colab\n" +
		"    HostName colab-runtime\n" +
		"    User root\n" +
		"    ProxyCommand colab ssh --proxy-mode -s trainer\n"
	updated, _, removedLegacy := upsertSessionHost(legacy, "work", "trainer")
	if !removedLegacy {
		t.Error("removedLegacy = false, want true")
	}
	if strings.Contains(updated, "Host colab\n") {
		t.Errorf("legacy Host colab not removed:\n%s", updated)
	}
	if !strings.Contains(updated, "Host trainer\n") {
		t.Errorf("new Host trainer missing:\n%s", updated)
	}
}

func TestPruneStaleHostsRemovesDead(t *testing.T) {
	first, _, _ := upsertSessionHost("", "work", "trainer")
	content, _, _ := upsertSessionHost(first, "work", "eval")
	updated, removed := pruneStaleHosts(content, map[string]bool{"trainer": true})
	if len(removed) != 1 || removed[0] != "eval" {
		t.Errorf("removed = %q, want [eval]", removed)
	}
	if strings.Contains(updated, "Host eval\n") {
		t.Errorf("stale Host eval not pruned:\n%s", updated)
	}
	if !strings.Contains(updated, "Host trainer\n") {
		t.Errorf("live Host trainer missing after prune:\n%s", updated)
	}
}

func TestPruneStaleHostsKeepsAllWhenAlive(t *testing.T) {
	first, _, _ := upsertSessionHost("", "work", "trainer")
	content, _, _ := upsertSessionHost(first, "work", "eval")
	updated, removed := pruneStaleHosts(content, map[string]bool{"trainer": true, "eval": true})
	if len(removed) != 0 {
		t.Errorf("removed = %q, want none", removed)
	}
	if updated != content {
		t.Errorf("content changed when nothing to prune:\n%s", updated)
	}
}

func TestPruneStaleHostsPreservesForeign(t *testing.T) {
	content, _, _ := upsertSessionHost("", "work", "trainer")
	content += "Host myserver\n    HostName example.com\n"
	updated, removed := pruneStaleHosts(content, map[string]bool{"trainer": true})
	if len(removed) != 0 {
		t.Errorf("removed = %q, want none", removed)
	}
	if !strings.Contains(updated, "Host myserver\n") {
		t.Errorf("foreign Host myserver not preserved:\n%s", updated)
	}
}

func TestPruneStaleHostsEmpty(t *testing.T) {
	updated, removed := pruneStaleHosts("", map[string]bool{"trainer": true})
	if len(removed) != 0 {
		t.Errorf("removed = %q, want none", removed)
	}
	if updated != "" {
		t.Errorf("updated = %q, want empty", updated)
	}
}

func TestValidateSessionHost(t *testing.T) {
	for _, ok := range []string{"trainer", "train-1", "eval_x", "a.b", "colab"} {
		if err := validateSessionHost(ok); err != nil {
			t.Errorf("validateSessionHost(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "has space", "tab\there", "hash#tag", "star*", "q?", "bang!", "quo'te", `dq"uote`, `back\slash`, "-leading-dash"} {
		if err := validateSessionHost(bad); err == nil {
			t.Errorf("validateSessionHost(%q) = nil, want error", bad)
		}
	}
}

func TestIsManagedBlock(t *testing.T) {
	if !isManagedBlock(colabHostBlock("work", "trainer")) {
		t.Error("current block not managed")
	}
	legacy := "Host old\n    ProxyCommand colab ssh --proxy-mode -s old\n"
	if !isManagedBlock(legacy) {
		t.Error("old --proxy-mode block not managed")
	}
	foreign := "Host myserver\n    HostName example.com\n"
	if isManagedBlock(foreign) {
		t.Error("foreign block reported managed")
	}
}

func TestUpsertReplacesOldProxyForm(t *testing.T) {
	old := "# Profile: work | Session: trainer\n" +
		"Host trainer\n" +
		"    HostName colab-runtime\n" +
		"    ProxyCommand colab ssh --proxy-mode -s trainer\n"
	updated, _, _ := upsertSessionHost(old, "work", "trainer")
	if !strings.Contains(updated, "ProxyCommand mycolab ssh -s trainer\n") {
		t.Errorf("old proxy form not replaced:\n%s", updated)
	}
	if strings.Contains(updated, "colab ssh --proxy-mode") {
		t.Errorf("old proxy form kept:\n%s", updated)
	}
}

func TestPruneStaleHostsRemovesOldForm(t *testing.T) {
	content := "# Managed by mycolab\n" +
		"Host ghost\n" +
		"    ProxyCommand colab ssh --proxy-mode -s ghost\n"
	updated, removed := pruneStaleHosts(content, map[string]bool{"trainer": true})
	if len(removed) != 1 || removed[0] != "ghost" {
		t.Errorf("removed = %q, want [ghost]", removed)
	}
	if strings.Contains(updated, "Host ghost\n") {
		t.Errorf("old-form stale host not pruned:\n%s", updated)
	}
}
