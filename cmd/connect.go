package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/carlesoctav/mycolab/pkg/colab"
	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect -s <name> [url]",
	Short: "Connect an existing Colab session (from web UI or URL) and wire for SSH",
	Long: `Connect an existing Colab session (e.g. created via web UI, or from a URL/endpoint)
into the active profile's local sessions, then wire it for SSH.

The session name is given by -s/--session. If [url] is provided, it can be a Colab
notebook URL, a backend tunnel URL, a proxy URL, or a raw endpoint ID. If [url] is
omitted, connect will find the active orphaned session on your Colab account.

Examples:
    # Auto-detect and connect the running orphaned session
    mycolab connect -s trainer

    # Connect by Colab notebook or tunnel URL
    mycolab connect -s trainer "https://colab.research.google.com/tun/m/gpu-a100-hm-ass1c0-5giphe4pf32o"

    # Connect by runtime endpoint directly
    mycolab connect -s trainer gpu-a100-hm-ass1c0-5giphe4pf32o

    # Connect and mount HF Storage Buckets
    mycolab connect -s trainer -v myuser/data:/content/data`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionName, err := requireSession(cmd)
		if err != nil {
			return err
		}
		current, err := profile.GetCurrent()
		if err != nil {
			return err
		}
		if current == "" {
			return fmt.Errorf("no active profile (use `mycolab use <profile_name>` first)")
		}

		var urlArg string
		if len(args) > 0 {
			urlArg = args[0]
		}

		assignments, err := colab.ListAssignments(current)
		if err != nil {
			return fmt.Errorf("failed to list assignments from Colab backend: %w", err)
		}
		if len(assignments) == 0 {
			return fmt.Errorf("no active assignments found on Colab account for profile %q", current)
		}

		var target *colab.Assignment
		if urlArg != "" {
			target, err = findMatchingAssignment(assignments, urlArg)
			if err != nil {
				return err
			}
		} else {
			target, err = selectAssignment(current, assignments)
			if err != nil {
				return err
			}
		}

		if err := saveConnectedSession(current, sessionName, target); err != nil {
			return err
		}

		fmt.Printf("Connected assignment %s (%s, %s) as session %q in profile %q.\n",
			target.Endpoint, target.Accelerator, colab.ShapeString(target.MachineShape), sessionName, current)

		dropMaster(sessionName)
		if err := runSSHSetup(cmd, sessionName); err != nil {
			return err
		}

		volumes, _ := cmd.Flags().GetStringArray("volume")
		var mounts []mountSpec
		for _, v := range volumes {
			m, err := parseMount(v)
			if err != nil {
				return err
			}
			mounts = append(mounts, m)
		}
		if err := mountVolumes(sessionName, mounts); err != nil {
			return err
		}

		forwards, err := forwardArgsFromFlags(cmd)
		if err != nil {
			return err
		}
		if len(forwards) > 0 {
			logf := func(format string, a ...any) { fmt.Printf("[mycolab connect] "+format+"\n", a...) }
			if err := ensureMaster(context.Background(), sessionName, logf, setupMasterBudget, forwards...); err != nil {
				return err
			}
		}

		return nil
	},
}

func findMatchingAssignment(assignments []colab.Assignment, urlArg string) (*colab.Assignment, error) {
	extracted := colab.ExtractEndpoint(urlArg)
	for i := range assignments {
		a := &assignments[i]
		if a.Endpoint == urlArg || a.Endpoint == extracted || strings.Contains(urlArg, a.Endpoint) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no active assignment found matching %q", urlArg)
}

func selectAssignment(current string, assignments []colab.Assignment) (*colab.Assignment, error) {
	localSessions, _ := profile.Sessions(current)
	knownEndpoints := make(map[string]bool)
	for _, s := range localSessions {
		knownEndpoints[s.Endpoint] = true
	}

	var orphans []colab.Assignment
	for _, a := range assignments {
		if !knownEndpoints[a.Endpoint] {
			orphans = append(orphans, a)
		}
	}

	if len(orphans) == 1 {
		return &orphans[0], nil
	}
	if len(orphans) == 0 {
		if len(assignments) == 1 {
			return &assignments[0], nil
		}
		return nil, fmt.Errorf("no unattached Colab assignments found; specify the URL or endpoint explicitly")
	}

	if stdinIsTerminal() {
		lines := make([]string, len(orphans))
		for i, a := range orphans {
			lines[i] = fmt.Sprintf("%s (Hardware: %s, Shape: %s, Variant: %s)",
				a.Endpoint, a.Accelerator, colab.ShapeString(a.MachineShape), colab.VariantString(a.Variant))
		}
		idx, err := Select("Select an assignment to connect:", lines)
		if err != nil {
			return nil, err
		}
		return &orphans[idx], nil
	}

	var endpoints []string
	for _, a := range orphans {
		endpoints = append(endpoints, a.Endpoint)
	}
	return nil, fmt.Errorf("multiple unattached assignments found (%s); pass the URL or endpoint as an argument", strings.Join(endpoints, ", "))
}

type storedSession struct {
	Name           string  `json:"name"`
	Token          string  `json:"token"`
	URL            string  `json:"url"`
	Endpoint       string  `json:"endpoint"`
	TokenExpiresAt string  `json:"token_expires_at,omitempty"`
	Variant        string  `json:"variant"`
	Accelerator    string  `json:"accelerator"`
	MachineShape   string  `json:"machine_shape"`
	KernelID       *string `json:"kernel_id"`
	SessionID      *string `json:"session_id"`
	LastExecution  any     `json:"last_execution"`
	Running        *string `json:"running"`
}

func saveConnectedSession(currentProfile, sessionName string, target *colab.Assignment) error {
	path, err := profile.ProfilePath(currentProfile)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	sessionsMap, err := decodeSessions(content)
	if err != nil {
		return err
	}

	var tokenExpires string
	if target.RuntimeProxyInfo.TokenExpiresInSeconds > 0 {
		tokenExpires = time.Now().UTC().Add(time.Duration(target.RuntimeProxyInfo.TokenExpiresInSeconds) * time.Second).Format("2006-01-02T15:04:05.999999Z")
	}

	entry := storedSession{
		Name:           sessionName,
		Token:          target.RuntimeProxyInfo.Token,
		URL:            target.RuntimeProxyInfo.URL,
		Endpoint:       target.Endpoint,
		TokenExpiresAt: tokenExpires,
		Variant:        colab.VariantString(target.Variant),
		Accelerator:    target.Accelerator,
		MachineShape:   colab.ShapeString(target.MachineShape),
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	sessionsMap[sessionName] = raw
	encoded, err := encodeSessions(sessionsMap)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

func init() {
	bindSSHSetupFlags(connectCmd)
	bindVolumeFlags(connectCmd)
	bindForwardFlags(connectCmd)
	rootCmd.AddCommand(connectCmd)
}
