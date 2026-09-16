// Package sshconfig reads and edits the user's ssh client config for mycolab:
// keeping the managed file's Include line in global scope.
package sshconfig

import (
	"fmt"
	"os"
	"strings"
)

// directive splits a config line into its lowercased keyword and arguments,
// stripping comments. It returns "" for blank/comment-only lines.
func directive(line string) (string, []string) {
	if i := strings.Index(line, "#"); i != -1 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", nil
	}
	return strings.ToLower(fields[0]), fields[1:]
}

// HasInclude reports whether content has an Include directive covering needle.
func HasInclude(content, needle string) bool {
	for _, line := range strings.Split(content, "\n") {
		if includeCovers(line, needle) {
			return true
		}
	}
	return false
}

func includeCovers(line, needle string) bool {
	d, args := directive(line)
	if d != "include" {
		return false
	}
	for _, pat := range args {
		if strings.Contains(pat, needle) {
			return true
		}
	}
	return false
}

// globalEnd returns the line index of the first Host/Match directive, i.e.
// where global scope ends, or the line count when the file has no blocks.
func globalEnd(lines []string) int {
	for i := range lines {
		if d, _ := directive(lines[i]); d == "host" || d == "match" {
			return i
		}
	}
	return len(lines)
}

// EnsureGlobalInclude returns content with includeLine present in global
// scope (inserted before the first Host/Match block). An Include inside a
// Host block is conditional on that block matching, so a scoped occurrence
// alone (e.g. appended at end of file) never applies to other hosts — any
// existing occurrences are removed in favor of the global one. changed is
// false when a global occurrence already exists.
func EnsureGlobalInclude(content, includeLine, needle string) (string, bool) {
	lines := strings.Split(content, "\n")
	end := globalEnd(lines)
	for _, line := range lines[:end] {
		if includeCovers(line, needle) {
			return content, false
		}
	}
	kept := lines[:0:0]
	for _, line := range lines {
		if !includeCovers(line, needle) {
			kept = append(kept, line)
		}
	}
	at := globalEnd(kept)
	if at == len(kept) && at > 0 && kept[at-1] == "" {
		// Content ends with a newline: insert before its empty tail
		// element to preserve the trailing newline.
		at--
	}
	kept = append(kept, "")
	copy(kept[at+1:], kept[at:])
	kept[at] = includeLine
	return strings.Join(kept, "\n"), true
}

// BackupPath returns path+".mycolab.bak", or a numbered variant when taken,
// so repeated backups never clobber each other.
func BackupPath(path string) string {
	candidate := path + ".mycolab.bak"
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 1; ; i++ {
		candidate = fmt.Sprintf("%s.mycolab.bak.%d", path, i)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
