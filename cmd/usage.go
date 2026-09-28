package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/carlesoctav/mycolab/pkg/colab"
	"github.com/carlesoctav/mycolab/pkg/profile"
	"github.com/spf13/cobra"
)

var usageJSON bool

var usageCmd = &cobra.Command{
	Use:   "usage [profile_name]",
	Short: "Show remaining Colab compute-unit credits for each profile",
	Long: `Show remaining Colab compute-unit (CU) credits per profile.

Without arguments, every profile is queried with its own login token and
the remaining balance, hourly burn rate and active assignment count are
printed as a table, plus the account email when it can be resolved.
Pass a profile name to show just that account.

Expired access tokens are refreshed automatically (the refreshed token is
written back to the profile's token file). Profiles that never logged in
are listed as such instead of failing the whole command.`,
	ValidArgsFunction: completeProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var names []string
		if len(args) == 1 {
			if err := profile.ValidateName(args[0]); err != nil {
				return err
			}
			ok, err := profile.Exists(args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("profile %q does not exist (use `mycolab add %s` to create it)", args[0], args[0])
			}
			names = []string{args[0]}
		} else {
			var err error
			names, err = profile.List()
			if err != nil {
				return err
			}
			if len(names) == 0 {
				dir, _ := profile.MycolabDir()
				fmt.Printf("No profiles found in %s\nUse `mycolab add <profile_name>` to create one.\n", dir)
				return nil
			}
		}

		rows := make([]usageRow, 0, len(names))
		for _, n := range names {
			u, err := colab.GetUsage(n)
			rows = append(rows, usageRow{name: n, usage: u, err: err})
		}

		if usageJSON {
			return printUsageJSON(rows)
		}
		return printUsageTable(rows)
	},
}

// usageRow is one profile's fetched (or failed) usage result.
type usageRow struct {
	name  string
	usage *colab.Usage
	err   error
}

// usageJSONRow is the per-profile element of --json output.
type usageJSONRow struct {
	Profile     string   `json:"profile"`
	Email       string   `json:"email,omitempty"`
	Balance     *float64 `json:"balance,omitempty"`
	RateHourly  *float64 `json:"rateHourly,omitempty"`
	Assignments *int     `json:"assignments,omitempty"`
	Active      bool     `json:"active"`
	LoggedIn    bool     `json:"loggedIn"`
	Error       string   `json:"error,omitempty"`
}

func printUsageJSON(rows []usageRow) error {
	current, _ := profile.GetCurrent()
	out := make([]usageJSONRow, 0, len(rows))
	var failed []string
	for _, r := range rows {
		jr := usageJSONRow{Profile: r.name, Active: r.name == current, LoggedIn: true}
		switch {
		case r.err == nil:
			b, rt, a := r.usage.Balance, r.usage.RateHourly, r.usage.Assignments
			jr.Email, jr.Balance, jr.RateHourly, jr.Assignments = r.usage.Email, &b, &rt, &a
		case errors.Is(r.err, colab.ErrNotLoggedIn):
			jr.LoggedIn = false
		default:
			jr.Error = r.err.Error()
			failed = append(failed, r.name)
		}
		out = append(out, jr)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to fetch usage for: %s", joinNames(failed))
	}
	return nil
}

func printUsageTable(rows []usageRow) error {
	current, _ := profile.GetCurrent()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, " \tPROFILE\tEMAIL\tBALANCE (CU)\tRATE/HR\tASSIGNMENTS")
	var total float64
	var counted int
	var failed []string
	for _, r := range rows {
		marker := " "
		if r.name == current {
			marker = "*"
		}
		switch {
		case r.err == nil:
			email := r.usage.Email
			if email == "" {
				email = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%.2f\t%.2f\t%d\n",
				marker, r.name, email, r.usage.Balance, r.usage.RateHourly, r.usage.Assignments)
			total += r.usage.Balance
			counted++
		case errors.Is(r.err, colab.ErrNotLoggedIn):
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", marker, r.name, "-", "not logged in", "-", "-")
		default:
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", marker, r.name, "-", "error", "-", "-")
			fmt.Fprintf(os.Stderr, "profile %q: %v\n", r.name, r.err)
			failed = append(failed, r.name)
		}
	}
	tw.Flush()
	if counted > 0 {
		fmt.Printf("Total remaining: %.2f compute units across %d profile(s).\n", total, counted)
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to fetch usage for: %s", joinNames(failed))
	}
	return nil
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

func init() {
	usageCmd.Flags().BoolVar(&usageJSON, "json", false, "output per-profile usage as JSON")
	rootCmd.AddCommand(usageCmd)
}
