package tools

// Muse installs the Muse coding agent the way colab/mv.sh does:
// the upstream installer plus the local auth/settings/trust files.
// The installer lands the binary in ~/.local/bin (off-PATH for
// non-interactive shells), so PostInstall links it into /usr/local/bin,
// which is on PATH for both exec and ssh.
var Muse = Spec{
	Name:        "muse",
	CheckBinary: "muse",
	InstallCmd:  "curl -fsSL https://dev.meta.ai/install.sh | bash",
	PostInstall: "ln -sf /root/.local/bin/muse /usr/local/bin/muse",
	ConfigDir:   "muse",
	ConfigFiles: []string{"auth.json", "settings.json", "trust.json"},
}
