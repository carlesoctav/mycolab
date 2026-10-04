package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidEnvName reports whether name is a valid environment variable name.
func ValidEnvName(name string) bool { return envNameRe.MatchString(name) }

// CustomEnvPath returns ~/.config/mycolab/env.json, the predefined custom
// env vars synced to every new runtime.
func CustomEnvPath() (string, error) {
	dir, err := MycolabDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "env.json"), nil
}

// CustomEnv loads the custom env vars (empty when none are defined).
func CustomEnv() (map[string]string, error) {
	path, err := CustomEnvPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	vars := map[string]string{}
	if err := json.Unmarshal(data, &vars); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return vars, nil
}

func saveCustomEnv(vars map[string]string) error {
	path, err := CustomEnvPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// SetCustomEnv adds or updates a custom env var.
func SetCustomEnv(key, value string) error {
	if !ValidEnvName(key) {
		return fmt.Errorf("invalid env name %q", key)
	}
	vars, err := CustomEnv()
	if err != nil {
		return err
	}
	vars[key] = value
	return saveCustomEnv(vars)
}

// DeleteCustomEnv removes a custom env var; it errors when absent.
func DeleteCustomEnv(key string) error {
	vars, err := CustomEnv()
	if err != nil {
		return err
	}
	if _, ok := vars[key]; !ok {
		return fmt.Errorf("custom env %q is not set", key)
	}
	delete(vars, key)
	return saveCustomEnv(vars)
}

// CustomEnvNames returns the sorted names of the custom env vars.
func CustomEnvNames(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
