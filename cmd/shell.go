package cmd

import "strings"

// shellQuote wraps s in single quotes for a remote shell, escaping
// embedded quotes so arguments cannot inject extra commands.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
