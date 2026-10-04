// Package profile manages mycolab profiles: per-account colab-cli session
// state and token files stored under ~/.config/mycolab, with the active
// profile linked into colab-cli's own config dir (~/.config/colab-cli).
//
// Layout:
//
//	~/.config/mycolab/<name>.json        session state (colab sessions.json format)
//	~/.config/mycolab/<name>.token.json  oauth2 token (empty until first login)
//	~/.config/colab-cli/sessions.json -> ~/.config/mycolab/<current>.json
//	~/.config/colab-cli/token.json    -> ~/.config/mycolab/<current>.token.json
package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Session is one colab session entry stored in a profile's sessions file.
type Session struct {
	Name        string
	Accelerator string
	Variant     string
	Endpoint    string
}

func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("unable to determine user home directory: %w", err)
	}
	return home, nil
}

// MycolabDir returns ~/.config/mycolab.
func MycolabDir() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mycolab"), nil
}

// ColabCLIDir returns ~/.config/colab-cli (colab-cli hardcodes this path).
func ColabCLIDir() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "colab-cli"), nil
}

// ColabSSHConfig returns the mycolab-managed ssh config file (~/.ssh/colab_config).
func ColabSSHConfig() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "colab_config"), nil
}

// MainSSHConfig returns the user's main ssh config file (~/.ssh/config).
func MainSSHConfig() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// ProfilePath returns the sessions file for a profile.
func ProfilePath(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := MycolabDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

// TokenPath returns the token file for a profile.
func TokenPath(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := MycolabDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".token.json"), nil
}

// ActiveSessionsPath returns colab-cli's sessions.json (a symlink when managed).
func ActiveSessionsPath() (string, error) {
	dir, err := ColabCLIDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.json"), nil
}

// ActiveTokenPath returns colab-cli's token.json (a symlink when managed).
func ActiveTokenPath() (string, error) {
	dir, err := ColabCLIDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

// ServerPath returns the file holding the selected always-on server
// (~/.config/mycolab/server): one ssh host name, written by
// `mycolab server connect`.
func ServerPath() (string, error) {
	dir, err := MycolabDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server"), nil
}

// GetServer returns the selected server host, or "" when none is selected.
func GetServer() (string, error) {
	path, err := ServerPath()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

// SetServer selects the always-on server host.
func SetServer(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("server host must not be empty")
	}
	dir, err := MycolabDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path, err := ServerPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o644)
}

// ValidateName rejects empty names and anything outside [a-zA-Z0-9_-].
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: use only letters, digits, '-' and '_'", name)
	}
	return nil
}

// List returns all profile names, sorted. A missing mycolab dir means no profiles.
func List() ([]string, error) {
	dir, err := MycolabDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		if !strings.HasSuffix(base, ".json") || strings.HasSuffix(base, ".token.json") {
			continue
		}
		name := strings.TrimSuffix(base, ".json")
		if ValidateName(name) != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Exists reports whether a profile's sessions file exists.
func Exists(name string) (bool, error) {
	path, err := ProfilePath(name)
	if err != nil {
		return false, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !fi.IsDir(), nil
}

// Create makes a new empty profile: `<name>.json` holding `{}` plus an empty
// `<name>.token.json` so the token symlink never dangles. colab-cli treats an
// empty token file as "not logged in" and overwrites it on first login.
func Create(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	exists, err := Exists(name)
	if err != nil {
		return "", err
	}
	if exists {
		dir, _ := MycolabDir()
		return "", fmt.Errorf("profile %q already exists in %s", name, dir)
	}
	dir, err := MycolabDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sessionsPath := filepath.Join(dir, name+".json")
	if err := os.WriteFile(sessionsPath, []byte("{}\n"), 0o644); err != nil {
		return "", err
	}
	tokenPath := filepath.Join(dir, name+".token.json")
	f, err := os.OpenFile(tokenPath, os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	return sessionsPath, nil
}

// GetCurrent returns the active profile name, or "" when colab-cli's
// sessions.json is not a mycolab-managed symlink.
func GetCurrent() (string, error) {
	link, err := ActiveSessionsPath()
	if err != nil {
		return "", err
	}
	fi, err := os.Lstat(link)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return "", nil
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	dir, err := MycolabDir()
	if err != nil {
		return "", err
	}
	if filepath.Dir(target) != dir || !strings.HasSuffix(target, ".json") {
		return "", nil
	}
	name := strings.TrimSuffix(filepath.Base(target), ".json")
	if ValidateName(name) != nil {
		return "", nil
	}
	exists, err := Exists(name)
	if err != nil || !exists {
		return "", nil
	}
	return name, nil
}

// backupPath returns link+".bak", or link+".bak.N" (first free N) to never clobber.
func backupPath(link string) string {
	candidate := link + ".bak"
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 1; ; i++ {
		candidate = fmt.Sprintf("%s.bak.%d", link, i)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// linkProfileFile points link at target. An existing symlink is repointed; an
// existing real file is moved aside to a .bak path first. It returns the
// backup path, or "" when nothing was backed up.
func linkProfileFile(link, target string) (string, error) {
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(link); err != nil {
				return "", err
			}
		} else {
			backup := backupPath(link)
			if err := os.Rename(link, backup); err != nil {
				return "", err
			}
			if err := os.Symlink(target, link); err != nil {
				return "", err
			}
			return backup, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(target, link); err != nil {
		return "", err
	}
	return "", nil
}

// SetCurrent activates a profile by symlinking colab-cli's sessions.json and
// token.json at the profile's files. Pre-existing real files are moved to
// .bak paths; the returned slice lists any backups made.
func SetCurrent(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	exists, err := Exists(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("profile %q does not exist (use `mycolab add %s` to create it)", name, name)
	}
	colabDir, err := ColabCLIDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(colabDir, 0o755); err != nil {
		return nil, err
	}
	sessionsTarget, err := ProfilePath(name)
	if err != nil {
		return nil, err
	}
	tokenTarget, err := TokenPath(name)
	if err != nil {
		return nil, err
	}
	// Profiles created by hand may lack a token file; touch one so the
	// token symlink never dangles.
	if f, err := os.OpenFile(tokenTarget, os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		_ = f.Close()
	} else if !os.IsExist(err) {
		return nil, err
	}

	var backups []string
	link, err := ActiveSessionsPath()
	if err != nil {
		return nil, err
	}
	if backup, err := linkProfileFile(link, sessionsTarget); err != nil {
		return backups, err
	} else if backup != "" {
		backups = append(backups, backup)
	}
	tokenLink, err := ActiveTokenPath()
	if err != nil {
		return backups, err
	}
	if backup, err := linkProfileFile(tokenLink, tokenTarget); err != nil {
		return backups, err
	} else if backup != "" {
		backups = append(backups, backup)
	}
	return backups, nil
}

// rawSession mirrors the fields of colab-cli's SessionState we display.
type rawSession struct {
	Accelerator string `json:"accelerator"`
	Variant     string `json:"variant"`
	Endpoint    string `json:"endpoint"`
}

// Sessions reads the profile's sessions, sorted by name.
func Sessions(name string) ([]Session, error) {
	path, err := ProfilePath(name)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil, nil
	}
	var raw map[string]rawSession
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("profile %q sessions file is not valid JSON: %w", name, err)
	}
	sessions := make([]Session, 0, len(raw))
	for sname, r := range raw {
		sessions = append(sessions, Session{
			Name:        sname,
			Accelerator: r.Accelerator,
			Variant:     r.Variant,
			Endpoint:    r.Endpoint,
		})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	return sessions, nil
}

// SessionCount returns the number of sessions stored in a profile.
func SessionCount(name string) (int, error) {
	sessions, err := Sessions(name)
	if err != nil {
		return 0, err
	}
	return len(sessions), nil
}

// HasToken reports whether the profile has a non-empty token file (i.e. it
// has logged in at least once).
func HasToken(name string) (bool, error) {
	path, err := TokenPath(name)
	if err != nil {
		return false, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return fi.Size() > 0, nil
}
