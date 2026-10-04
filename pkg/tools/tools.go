// Package tools describes the CLI tools mycolab can install on a Colab
// runtime: how to install each one and which local config/credential
// files to copy over when present. One tool per file (see muse.go).
package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Spec describes one installable tool.
type Spec struct {
	// Name is the CLI name, e.g. "muse".
	Name string
	// CheckBinary skips the (re)install when found on the remote PATH.
	CheckBinary string
	// InstallCmd is the remote shell command that installs it.
	InstallCmd string
	// PostInstall is remote shell run right after an install, e.g. to
	// link the binary into /usr/local/bin when the installer lands it
	// outside PATH. Empty means nothing extra.
	PostInstall string
	// ConfigBase is the home-relative parent of ConfigDir; empty means
	// ".config".
	ConfigBase string
	// ConfigDir mirrors local ~/<ConfigBase>/<dir> to /root/<ConfigBase>/<dir>.
	ConfigDir string
	// ConfigFiles are copied when present locally; missing ones are skipped.
	ConfigFiles []string
}

// registry lists every supported tool.
var registry = []Spec{Muse, Agy, Hf, HfMount}

// RunTools are installed on every new runtime so `run -v` (HF bucket
// mounts) works out of the box.
func RunTools() []Spec { return []Spec{Hf, HfMount} }

// Lookup returns the spec for name (case-insensitive), or an error
// listing the supported tool names.
func Lookup(name string) (Spec, error) {
	for _, s := range registry {
		if strings.EqualFold(s.Name, name) {
			return s, nil
		}
	}
	names := make([]string, len(registry))
	for i, s := range registry {
		names[i] = s.Name
	}
	sort.Strings(names)
	return Spec{}, fmt.Errorf("unknown tool %q (supported: %s)", name, strings.Join(names, ", "))
}

// ConfigRoot returns the home-relative config base (default ".config").
func (s Spec) ConfigRoot() string {
	if s.ConfigBase == "" {
		return ".config"
	}
	return s.ConfigBase
}

// InstallStdin builds the `colab exec` stdin that installs the tool: a
// no-op when CheckBinary is already on the remote PATH, otherwise
// InstallCmd plus PostInstall, with a final PATH check gating the marker.
func (s Spec) InstallStdin(marker string) string {
	var sb strings.Builder
	sb.WriteString("!")
	if s.ConfigDir != "" {
		sb.WriteString("mkdir -p /root/" + s.ConfigRoot() + "/" + s.ConfigDir + " && ")
	}
	sb.WriteString("(command -v " + s.CheckBinary + " || (" + s.InstallCmd)
	if s.PostInstall != "" {
		sb.WriteString(" && " + s.PostInstall)
	}
	sb.WriteString(")) && command -v " + s.CheckBinary + " >/dev/null && echo " + marker + "\n")
	return sb.String()
}
