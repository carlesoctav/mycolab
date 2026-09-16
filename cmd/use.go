package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var useCmd = &cobra.Command{
	Use:   "use [profile_name]",
	Short: "Switch to a profile (interactive picker when omitted)",
	Long: `Switch the active mycolab profile.

With a name, that profile is activated directly. Without one, an interactive
picker is shown (j/k or arrow keys to move, Enter to select). When stdin is
not a terminal, the current profile is printed instead.

Switching repoints colab-cli's sessions.json and token.json symlinks at the
profile's files, so plain 'colab ...' commands operate on that workspace.`,
	ValidArgsFunction: completeProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return switchTo(args[0])
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current == "" {
				fmt.Println("No active profile. Use `mycolab use <profile_name>` to set one.")
			} else {
				fmt.Printf("Active profile: %s\n", current)
			}
			return nil
		}
		profiles, err := profile.List()
		if err != nil {
			return err
		}
		if len(profiles) == 0 {
			fmt.Println("No profiles yet. Create one with `mycolab add <profile_name>`.")
			return nil
		}
		current, _ := profile.GetCurrent()
		lines := make([]string, len(profiles))
		start := 0
		for i, p := range profiles {
			lines[i] = p
			if p == current {
				lines[i] += "  (active)"
				start = i
			}
		}
		idx, err := SelectWithDefault("Select profile:", lines, start)
		if err != nil {
			if errors.Is(err, ErrCancelled) {
				return nil
			}
			return err
		}
		return switchTo(profiles[idx])
	},
}

func switchTo(name string) error {
	prev, err := profile.GetCurrent()
	if err != nil {
		return err
	}
	if prev == name {
		fmt.Printf("Already on profile %q.\n", name)
		return nil
	}
	prevCount := 0
	if prev != "" {
		prevCount, _ = profile.SessionCount(prev)
	}
	backups, err := profile.SetCurrent(name)
	if err != nil {
		return err
	}
	for _, b := range backups {
		fmt.Printf("Backed up existing colab-cli state to %s\n", b)
	}
	fmt.Printf("Switched to profile %q.\n", name)
	if prev != "" && prevCount > 0 {
		fmt.Printf("Note: profile %q still holds %d session(s); their keep-alive daemons "+
			"may exit while it is inactive, letting those VMs idle out. Switch back with "+
			"`mycolab use %s` to resume them.\n", prev, prevCount, prev)
	}
	if ok, _ := profile.HasToken(name); !ok {
		fmt.Printf("Profile %q is not logged in yet; the next `colab ...` command will start the login flow.\n", name)
	}
	return nil
}

func completeProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	profiles, err := profile.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	current, _ := profile.GetCurrent()
	var results []string
	for _, p := range profiles {
		desc := "mycolab profile"
		if p == current {
			desc = "active profile"
		}
		results = append(results, p+"\t"+desc)
	}
	return results, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	rootCmd.AddCommand(useCmd)
}
