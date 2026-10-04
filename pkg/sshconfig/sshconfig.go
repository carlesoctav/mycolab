// Package sshconfig reads and edits the user's ssh client config for mycolab:
// keeping the managed file's Include line in global scope.
package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
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

// HostNames returns the literal host patterns declared by Host directives
// in content, in order of appearance. Wildcard/negated patterns (those
// containing *?! ) are skipped: they cannot be selected by exact name.
func HostNames(content string) []string {
	var names []string
	for _, line := range strings.Split(content, "\n") {
		d, args := directive(line)
		if d != "host" {
			continue
		}
		for _, pat := range args {
			if strings.ContainsAny(pat, "*?!") {
				continue
			}
			names = append(names, pat)
		}
	}
	return names
}

// LoadHostNames returns every literal Host pattern declared by path,
// following Include directives recursively (cycle-safe). Relative include
// patterns resolve against the including file's directory. A missing file
// yields no names and no error.
func LoadHostNames(path string) ([]string, error) {
	return loadHostNames(path, map[string]bool{})
}

func loadHostNames(path string, visited map[string]bool) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if visited[abs] {
		return nil, nil
	}
	visited[abs] = true
	content, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := HostNames(string(content))
	for _, line := range strings.Split(string(content), "\n") {
		d, args := directive(line)
		if d != "include" {
			continue
		}
		for _, pat := range args {
			expanded := expandIncludePattern(pat, filepath.Dir(abs))
			if expanded == "" {
				continue
			}
			matches, err := filepath.Glob(expanded)
			if err != nil {
				continue
			}
			for _, m := range matches {
				sub, err := loadHostNames(m, visited)
				if err != nil {
					return nil, err
				}
				names = append(names, sub...)
			}
		}
	}
	return names, nil
}

// expandIncludePattern resolves one Include pattern: a leading ~/ expands
// against the user's home, and relative patterns resolve against dir (the
// including file's directory). It returns "" when home cannot be determined.
func expandIncludePattern(pat, dir string) string {
	if pat == "~" || strings.HasPrefix(pat, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		if pat == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(pat, "~/"))
	}
	if !filepath.IsAbs(pat) {
		return filepath.Join(dir, pat)
	}
	return pat
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
