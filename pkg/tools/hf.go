package tools

// Hf installs the Hugging Face CLI (`hf`) with uv. UV_TOOL_BIN_DIR puts
// the shim in /usr/local/bin, which is on PATH for exec and ssh alike.
// uv itself is bootstrapped with pip when the runtime lacks it.
var Hf = Spec{
	Name:        "hf",
	CheckBinary: "hf",
	InstallCmd: "(command -v uv >/dev/null 2>&1 || pip install -q uv) && " +
		"UV_TOOL_BIN_DIR=/usr/local/bin uv tool install --force 'huggingface_hub[cli]'",
}

// HfMount installs hf-mount (mount HF buckets/repos as a filesystem) from
// the upstream release binaries: the daemon plus its NFS and FUSE backends.
var HfMount = Spec{
	Name:        "hf-mount",
	CheckBinary: "hf-mount",
	InstallCmd: "(command -v mount.nfs >/dev/null 2>&1 || (rm -f /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*ppa* 2>/dev/null || true; apt-get update -qq -o Acquire::http::Timeout=10 -o Acquire::https::Timeout=10 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nfs-common)) && " +
		"for b in hf-mount hf-mount-nfs hf-mount-fuse; do " +
		"curl -fsSL -o /usr/local/bin/$b https://github.com/huggingface/hf-mount/releases/latest/download/$b-x86_64-linux " +
		"&& chmod +x /usr/local/bin/$b || exit 1; done",
}
