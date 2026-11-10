package colab_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/carlesoctav/mycolab/pkg/colab"
	"github.com/carlesoctav/mycolab/pkg/profile"
)

func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// writeToken stores a token file for profile name and returns its path.
func writeToken(t *testing.T, name string, body map[string]any) string {
	t.Helper()
	dir, err := profile.MycolabDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A sessions file must exist for the profile to be well-formed.
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".token.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validTokenBody(tokenURI string, expiry time.Time) map[string]any {
	return map[string]any{
		"token":         "access-123",
		"refresh_token": "refresh-123",
		"token_uri":     tokenURI,
		"client_id":     "cid",
		"client_secret": "csecret",
		"scopes":        []string{"openid"},
		"expiry":        expiry.UTC().Format(time.RFC3339),
	}
}

func TestGetUsageNotLoggedIn(t *testing.T) {
	tempHome(t)
	if _, err := profile.Create("work"); err != nil {
		t.Fatal(err)
	}
	// Fresh profile: empty token file.
	if _, err := colab.GetUsage("work"); err != colab.ErrNotLoggedIn {
		t.Fatalf("GetUsage(fresh) = %v, want ErrNotLoggedIn", err)
	}
	// Missing profile entirely.
	if _, err := colab.GetUsage("nope"); err != colab.ErrNotLoggedIn {
		t.Fatalf("GetUsage(missing) = %v, want ErrNotLoggedIn", err)
	}
}

func TestGetUsageSuccess(t *testing.T) {
	tempHome(t)
	ccu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-123" {
			t.Errorf("ccu-info Authorization = %q", got)
		}
		if got := r.Header.Get("X-Colab-Client-Agent"); got != "colab-cli" {
			t.Errorf("ccu-info client agent = %q", got)
		}
		// Real backend prepends an XSSI prefix.
		_, _ = w.Write([]byte(")]}'\n" + `{"currentBalance":152.84,"consumptionRateHourly":11.5,` +
			`"assignmentsCount":1,"eligibleGpus":["T4"],"eligibleTpus":["V5E1"]}`))
	}))
	defer ccu.Close()
	userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"email":"work@example.com"}`))
	}))
	defer userinfo.Close()
	oldCCU, oldUserinfo := colab.CCUInfoURL, colab.UserinfoURL
	colab.CCUInfoURL, colab.UserinfoURL = ccu.URL, userinfo.URL
	defer func() { colab.CCUInfoURL, colab.UserinfoURL = oldCCU, oldUserinfo }()

	writeToken(t, "work", validTokenBody("https://unused.example/token", time.Now().Add(time.Hour)))

	u, err := colab.GetUsage("work")
	if err != nil {
		t.Fatalf("GetUsage = %v", err)
	}
	if u.Profile != "work" || u.Email != "work@example.com" {
		t.Fatalf("usage identity = %+v", u)
	}
	if u.Balance != 152.84 || u.RateHourly != 11.5 || u.Assignments != 1 {
		t.Fatalf("usage numbers = %+v", u)
	}
	if len(u.EligibleGPUs) != 1 || u.EligibleGPUs[0] != "T4" {
		t.Fatalf("eligible GPUs = %v", u.EligibleGPUs)
	}
}

func TestGetUsageRefreshesExpiredToken(t *testing.T) {
	tempHome(t)
	var sawRefresh bool
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRefresh = true
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse refresh form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q", got)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-123" {
			t.Errorf("refresh_token = %q", got)
		}
		_, _ = w.Write([]byte(`{"access_token":"access-new","expires_in":3600}`))
	}))
	defer oauth.Close()
	ccu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-new" {
			t.Errorf("ccu-info Authorization = %q, want refreshed token", got)
		}
		_, _ = w.Write([]byte(`{"currentBalance":7.5,"consumptionRateHourly":0,"assignmentsCount":0}`))
	}))
	defer ccu.Close()
	userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`)) // no email: must not fail the call
	}))
	defer userinfo.Close()
	oldCCU, oldUserinfo := colab.CCUInfoURL, colab.UserinfoURL
	colab.CCUInfoURL, colab.UserinfoURL = ccu.URL, userinfo.URL
	defer func() { colab.CCUInfoURL, colab.UserinfoURL = oldCCU, oldUserinfo }()

	path := writeToken(t, "work", validTokenBody(oauth.URL, time.Now().Add(-time.Hour)))

	u, err := colab.GetUsage("work")
	if err != nil {
		t.Fatalf("GetUsage = %v", err)
	}
	if !sawRefresh {
		t.Fatal("expected a refresh request, saw none")
	}
	if u.Balance != 7.5 || u.Email != "" {
		t.Fatalf("usage = %+v", u)
	}
	// Refreshed token is persisted for the next run.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["token"] != "access-new" {
		t.Fatalf("persisted token = %v, want access-new", saved["token"])
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, %v; want 0600", fi, err)
	}
}

func TestGetUsageRefreshFailure(t *testing.T) {
	tempHome(t)
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer oauth.Close()
	oldCCU := colab.CCUInfoURL
	colab.CCUInfoURL = "http://127.0.0.1:1/unused" // must never be reached
	defer func() { colab.CCUInfoURL = oldCCU }()

	writeToken(t, "work", validTokenBody(oauth.URL, time.Now().Add(-time.Hour)))

	if _, err := colab.GetUsage("work"); err == nil {
		t.Fatal("GetUsage with revoked refresh token = nil, want error")
	}
}

func TestGetUsageCcuError(t *testing.T) {
	tempHome(t)
	ccu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`unauthorized`))
	}))
	defer ccu.Close()
	oldCCU := colab.CCUInfoURL
	colab.CCUInfoURL = ccu.URL
	defer func() { colab.CCUInfoURL = oldCCU }()

	writeToken(t, "work", validTokenBody("https://unused.example/token", time.Now().Add(time.Hour)))

	if _, err := colab.GetUsage("work"); err == nil {
		t.Fatal("GetUsage with 401 ccu-info = nil, want error")
	}
}

func TestListAssignments(t *testing.T) {
	tempHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-123" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(")]}'\n" + `{"assignments":[{"endpoint":"ep-1","variant":1,"machineShape":1,"accelerator":"A100","runtimeProxyInfo":{"token":"jwt-123","tokenExpiresInSeconds":3600,"url":"https://proxy.example"}}]}`))
	}))
	defer server.Close()
	oldAssignments := colab.AssignmentsURL
	colab.AssignmentsURL = server.URL
	defer func() { colab.AssignmentsURL = oldAssignments }()

	writeToken(t, "work", validTokenBody("https://unused.example/token", time.Now().Add(time.Hour)))

	assignments, err := colab.ListAssignments("work")
	if err != nil {
		t.Fatalf("ListAssignments failed: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("got %d assignments, want 1", len(assignments))
	}
	a := assignments[0]
	if a.Endpoint != "ep-1" || a.Accelerator != "A100" || a.RuntimeProxyInfo.URL != "https://proxy.example" {
		t.Errorf("unexpected assignment: %+v", a)
	}
	if got := colab.VariantString(a.Variant); got != "GPU" {
		t.Errorf("VariantString = %q, want GPU", got)
	}
	if got := colab.ShapeString(a.MachineShape); got != "HIGH_RAM" {
		t.Errorf("ShapeString = %q, want HIGH_RAM", got)
	}
}

func TestExtractEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpu-a100-hm-ass1c0-5giphe4pf32o", "gpu-a100-hm-ass1c0-5giphe4pf32o"},
		{"https://colab.research.google.com/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o", "gpu-a100-hm-ass1c0-5giphe4pf32o"},
		{"/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o", "gpu-a100-hm-ass1c0-5giphe4pf32o"},
		{"https://colab.research.google.com/notebooks/empty.ipynb?dbu=/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o#datalabBackendUrl=https://colab.research.google.com/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o", "gpu-a100-hm-ass1c0-5giphe4pf32o"},
		{"https://colab.research.google.com/drive/123#datalabBackendUrl=/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o", "gpu-a100-hm-ass1c0-5giphe4pf32o"},
		{"https://8080-gpu-a100-hm-ass1c0-5giphe4pf32o-c.asia-southeast1.prod.r.colab.dev", "gpu-a100-hm-ass1c0-5giphe4pf32o"},
		{"", ""},
	}
	for _, tc := range tests {
		got := colab.ExtractEndpoint(tc.input)
		if got != tc.want {
			t.Errorf("ExtractEndpoint(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

