// Package pathutil expands and compares paths consistently on every operating system.
package pathutil

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var winVar = regexp.MustCompile(`%([^%]+)%`)

// Expand replaces %VAR% (all systems), $VAR, ${VAR} and ~ (not Windows)
// and cleans the path. Undefined variables are left as they are.
func Expand(p string) string {
	s := winVar.ReplaceAllStringFunc(p, func(m string) string {
		if v, ok := os.LookupEnv(m[1 : len(m)-1]); ok {
			return v
		}
		return m
	})
	if runtime.GOOS != "windows" {
		if s == "~" || strings.HasPrefix(s, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				s = home + s[1:]
			}
		}
		s = os.Expand(s, func(name string) string {
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
			return "${" + name + "}"
		})
	} else {
		s = strings.ReplaceAll(s, "/", `\`)
	}
	return filepath.Clean(s)
}

// CaseInsensitive reports whether the system file system ignores letter case.
func CaseInsensitive() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

// Key returns the comparison key of a path.
func Key(p string) string {
	k := filepath.Clean(p)
	if CaseInsensitive() {
		k = strings.ToLower(k)
	}
	return k
}

// Parent returns the parent of a key, or "" for a root.
func Parent(key string) string {
	parent := filepath.Dir(key)
	if parent == key {
		return ""
	}
	return parent
}

// IsUnder reports whether key equals dir or is below it (both are keys).
func IsUnder(key, dir string) bool {
	if key == dir {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(key, strings.TrimSuffix(dir, sep)+sep)
}

// Unexpand replaces well-known prefixes with the variables used in dictionaries.
func Unexpand(p string) string {
	type pair struct{ prefix, repl string }
	var pairs []pair
	if runtime.GOOS == "windows" {
		for _, v := range []string{"LOCALAPPDATA", "APPDATA", "ProgramData", "ProgramFiles(x86)", "ProgramFiles", "USERPROFILE"} {
			if val := os.Getenv(v); val != "" {
				pairs = append(pairs, pair{Key(val), "%" + v + "%"})
			}
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		pairs = append(pairs, pair{Key(home), "~"})
	}
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].prefix) > len(pairs[j].prefix) })
	k := Key(p)
	clean := filepath.Clean(p)
	for _, pr := range pairs {
		if IsUnder(k, pr.prefix) {
			return pr.repl + clean[len(pr.prefix):]
		}
	}
	return clean
}
