package tools

// Agy installs the Antigravity CLI (agy). The installer lands the binary
// in ~/.local/bin (off-PATH for non-interactive shells), so PostInstall
// links it into /usr/local/bin. The login lives in
// ~/.gemini/antigravity-cli/antigravity-oauth-token.
var Agy = Spec{
	Name:        "agy",
	CheckBinary: "agy",
	InstallCmd:  "curl -fsSL --compressed https://antigravity.google/cli/install.sh | bash",
	PostInstall: "ln -sf /root/.local/bin/agy /usr/local/bin/agy",
	ConfigBase:  ".gemini",
	ConfigDir:   "antigravity-cli",
	ConfigFiles: []string{"antigravity-oauth-token"},
}
