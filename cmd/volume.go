package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// mountSpec mounts an HF bucket at a runtime dir (via hf-mount).
type mountSpec struct {
	Bucket   string
	Remote   string
	ReadOnly bool
}

// bindVolumeFlags registers the -v/--volume flag on c.
func bindVolumeFlags(c *cobra.Command) {
	c.Flags().StringArrayP("volume", "v", nil, "hf_bucket:colab_dir[:ro] to mount on the runtime with hf-mount (repeatable)")
}

// parseMount parses "[hf://buckets/]<bucket_id>:<mount_path>[:ro]".
// Bucket is user/name[/subpath].
func parseMount(s string) (mountSpec, error) {
	orig := s
	readOnly := false
	if strings.HasSuffix(s, ":ro") {
		readOnly = true
		s = strings.TrimSuffix(s, ":ro")
	} else if strings.HasSuffix(s, ":rw") {
		s = strings.TrimSuffix(s, ":rw")
	}
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return mountSpec{}, fmt.Errorf("invalid -v %q (want [hf://buckets/]bucket:colab_dir[:ro])", orig)
	}
	bucket, remote := s[:i], s[i+1:]
	bucket = strings.TrimPrefix(bucket, "hf://buckets/")
	bucket = strings.TrimPrefix(bucket, "hf://")
	if !strings.Contains(bucket, "/") || strings.ContainsAny(bucket, " \t\r\n'\"\\$`;&|<>()") {
		return mountSpec{}, fmt.Errorf("invalid -v %q: bucket must look like user/name", orig)
	}
	if !strings.HasPrefix(remote, "/") {
		return mountSpec{}, fmt.Errorf("invalid -v %q: colab dir %q must be an absolute path", orig, remote)
	}
	cleanRemote := strings.TrimRight(remote, "/")
	if cleanRemote == "" {
		cleanRemote = "/"
	}
	return mountSpec{
		Bucket:   bucket,
		Remote:   cleanRemote,
		ReadOnly: readOnly,
	}, nil
}

// mountScript mounts a bucket on the runtime. HF_TOKEN reaches ssh sessions
// via `mycolab env HF_TOKEN <token>`.
func (m mountSpec) mountScript() string {
	ro := ""
	if m.ReadOnly {
		ro = " --read-only"
	}
	return fmt.Sprintf(`mkdir -p %s && hf-mount start --fuse%s ${HF_TOKEN:+--hf-token "$HF_TOKEN"} bucket %s %s`,
		shellQuote(m.Remote), ro, m.Bucket, shellQuote(m.Remote))
}

// runSessionCommand executes script on session over SSH.
// A variable so tests can stub it.
var runSessionCommand = func(session, script string) ([]byte, error) {
	sshBin, err := findSSH()
	if err != nil {
		return nil, fmt.Errorf("ssh binary not found in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var args []string
	if alt := os.Getenv("MYCOLAB_SSH_CONFIG"); alt != "" {
		args = append(args, "-F", alt)
	}
	args = append(args, session, script)
	c := exec.CommandContext(ctx, sshBin, args...)
	return c.CombinedOutput()
}

// mountVolumes mounts the requested volumes on session over SSH and validates success.
func mountVolumes(session string, mounts []mountSpec) error {
	for _, m := range mounts {
		fmt.Printf("Mounting HF bucket %s at %s...\n", m.Bucket, m.Remote)
		out, err := runSessionCommand(session, m.mountScript())
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg != "" {
				return fmt.Errorf("mount %s at %s failed: %w (output: %s)", m.Bucket, m.Remote, err, msg)
			}
			return fmt.Errorf("mount %s at %s failed: %w", m.Bucket, m.Remote, err)
		}
		fmt.Printf("Mounted %s at %s.\n", m.Bucket, m.Remote)
	}
	return nil
}
