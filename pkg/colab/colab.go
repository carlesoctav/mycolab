// Package colab queries the Colab backend for account-level compute-unit
// (CU) balance and burn rate, using each mycolab profile's own OAuth token.
//
// Data source: GET https://colab.research.google.com/tun/m/ccu-info
// (same bearer-token auth and session backend as `colab new`). The response
// carries an XSSI prefix followed by JSON:
//
//	{"currentBalance": 152.84, "consumptionRateHourly": 0,
//	 "assignmentsCount": 0, "eligibleGpus": [...], ...}
package colab

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/carlesoctav/mycolab/pkg/profile"
)

// ErrNotLoggedIn is returned when a profile has no usable token (it has
// never logged in, or its token file is empty).
var ErrNotLoggedIn = errors.New("not logged in")

// Endpoints. Variables (not constants) so tests can point them at local servers.
var (
	// CCUInfoURL reports CU balance, hourly burn rate and assignment count.
	// authuser=0 is required: without it the backend answers HTTP 400.
	CCUInfoURL = "https://colab.research.google.com/tun/m/ccu-info?authuser=0"
	// UserinfoURL resolves the account email for a token.
	UserinfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
	// HTTPClient performs backend requests.
	HTTPClient = &http.Client{Timeout: 20 * time.Second}
)

// refreshMargin triggers a proactive token refresh when the access token
// expires within this duration.
const refreshMargin = 60 * time.Second

const xssiPrefix = ")]}'\n"

// oauthToken mirrors google-auth's authorized-user file format
// (Credentials.to_json), as stored in <name>.token.json.
type oauthToken struct {
	Token          string    `json:"token"`
	RefreshToken   string    `json:"refresh_token"`
	TokenURI       string    `json:"token_uri"`
	ClientID       string    `json:"client_id"`
	ClientSecret   string    `json:"client_secret"`
	Scopes         []string  `json:"scopes"`
	UniverseDomain string    `json:"universe_domain"`
	Account        string    `json:"account"`
	Expiry         time.Time `json:"expiry"`
}

// CcuInfo is the parsed /tun/m/ccu-info response.
type CcuInfo struct {
	CurrentBalance        float64  `json:"currentBalance"`
	ConsumptionRateHourly float64  `json:"consumptionRateHourly"`
	AssignmentsCount      int      `json:"assignmentsCount"`
	EligibleGPUs          []string `json:"eligibleGpus"`
	IneligibleGPUs        []string `json:"ineligibleGpus"`
	EligibleTPUs          []string `json:"eligibleTpus"`
	IneligibleTPUs        []string `json:"ineligibleTpus"`
}

// Usage is the per-profile result shown by `mycolab usage`.
type Usage struct {
	Profile      string   `json:"profile"`
	Email        string   `json:"email"`
	Balance      float64  `json:"balance"`
	RateHourly   float64  `json:"rateHourly"`
	Assignments  int      `json:"assignments"`
	EligibleGPUs []string `json:"eligibleGpus,omitempty"`
	EligibleTPUs []string `json:"eligibleTpus,omitempty"`
}

// loadToken reads and parses a profile's token file.
func loadToken(name string) (*oauthToken, string, error) {
	path, err := profile.TokenPath(name)
	if err != nil {
		return nil, "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNotLoggedIn
		}
		return nil, "", err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil, "", ErrNotLoggedIn
	}
	var tok oauthToken
	if err := json.Unmarshal(content, &tok); err != nil {
		return nil, "", fmt.Errorf("profile %q token file is not valid JSON: %w", name, err)
	}
	if tok.Token == "" && tok.RefreshToken == "" {
		return nil, "", ErrNotLoggedIn
	}
	return &tok, path, nil
}

// expired reports whether the access token needs a refresh.
func (t *oauthToken) expired() bool {
	if t.Expiry.IsZero() {
		return false
	}
	return time.Until(t.Expiry) < refreshMargin
}

// refreshResponse is the subset of the OAuth2 refresh response we consume.
type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// refresh exchanges the refresh token for a new access token and persists
// the updated token file (mode 0600, like colab-cli).
func (t *oauthToken) refresh(path string) error {
	if t.RefreshToken == "" {
		return fmt.Errorf("%w: access token expired and no refresh token stored", ErrNotLoggedIn)
	}
	if t.TokenURI == "" {
		return errors.New("token file has no token_uri; cannot refresh")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.RefreshToken},
		"client_id":     {t.ClientID},
		"client_secret": {t.ClientSecret},
	}
	resp, err := HTTPClient.PostForm(t.TokenURI, form)
	if err != nil {
		return fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("token refresh read failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rr refreshResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return fmt.Errorf("token refresh returned invalid JSON: %w", err)
	}
	if rr.AccessToken == "" {
		return errors.New("token refresh returned no access token")
	}
	t.Token = rr.AccessToken
	if rr.RefreshToken != "" {
		t.RefreshToken = rr.RefreshToken
	}
	if rr.ExpiresIn > 0 {
		t.Expiry = time.Now().UTC().Add(time.Duration(rr.ExpiresIn) * time.Second)
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("failed to save refreshed token: %w", err)
	}
	return nil
}

// accessToken returns a valid access token for the profile, refreshing and
// persisting it first when expired.
func accessToken(name string) (string, error) {
	tok, path, err := loadToken(name)
	if err != nil {
		return "", err
	}
	if tok.expired() {
		if err := tok.refresh(path); err != nil {
			return "", fmt.Errorf("profile %q: %w", name, err)
		}
	}
	if tok.Token == "" {
		return "", fmt.Errorf("profile %q: %w", name, ErrNotLoggedIn)
	}
	return tok.Token, nil
}

// getJSON performs an authenticated GET and decodes the JSON body,
// stripping the XSSI prefix Colab prepends when present.
func getJSON(url, accessToken string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Colab-Client-Agent", "colab-cli")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	text := strings.TrimPrefix(string(body), xssiPrefix)
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("GET %s: invalid JSON: %w", url, err)
	}
	return nil
}

// fetchCcuInfo queries the CU balance endpoint.
func fetchCcuInfo(accessToken string) (*CcuInfo, error) {
	var info CcuInfo
	if err := getJSON(CCUInfoURL, accessToken, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// fetchEmail resolves the account email for an access token.
func fetchEmail(accessToken string) (string, error) {
	var v struct {
		Email string `json:"email"`
	}
	if err := getJSON(UserinfoURL, accessToken, &v); err != nil {
		return "", err
	}
	return v.Email, nil
}

// GetUsage returns the CU balance, burn rate and account email for one profile.
// A missing email never fails the call: the balance is the primary result.
func GetUsage(name string) (*Usage, error) {
	token, err := accessToken(name)
	if err != nil {
		return nil, err
	}
	info, err := fetchCcuInfo(token)
	if err != nil {
		return nil, fmt.Errorf("profile %q: %w", name, err)
	}
	u := &Usage{
		Profile:      name,
		Balance:      info.CurrentBalance,
		RateHourly:   info.ConsumptionRateHourly,
		Assignments:  info.AssignmentsCount,
		EligibleGPUs: info.EligibleGPUs,
		EligibleTPUs: info.EligibleTPUs,
	}
	if email, err := fetchEmail(token); err == nil {
		u.Email = email
	}
	return u, nil
}
