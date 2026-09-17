// Package match compares folder names with installed programs.
package match

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
)

var (
	nonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	versionTail = regexp.MustCompile(`\s+v?\d+(\.\d+)+.*$`)
	parens      = regexp.MustCompile(`\(.*?\)|\[.*?\]`)
	company     = regexp.MustCompile(`(?i)\b(inc|corp|corporation|ltd|llc|gmbh|s\.?r\.?l|s\.?p\.?a|co|company|limited|technologies|b\.?v)\b\.?`)
	updaterTail = regexp.MustCompile(`(?i)[-_. ]?updater$`)
)

// GenericKeys are words too common for a similarity match.
var GenericKeys = map[string]bool{
	"update": true, "updater": true, "setup": true, "install": true, "installer": true,
	"uninstall": true, "helper": true, "service": true, "launcher": true, "desktop": true,
	"tools": true, "common": true, "shared": true, "runtime": true, "client": true,
	"server": true, "cache": true, "config": true, "local": true, "data": true,
	"files": true, "program": true, "programs": true, "python": true, "scripts": true,
	"bin": true, "lib": true, "share": true, "app": true, "apps": true,
}

// Aliases maps a normalized folder name to an alternative key to look up.
var Aliases = map[string]string{
	"vscode":       "visualstudiocode",
	"code":         "visualstudiocode",
	"nuget":        "visualstudio",
	"vs":           "visualstudio",
	"msplaywright": "playwright",
	"ipython":      "python",
	"jupyter":      "python",
	"pip":          "python",
	"npm":          "nodejs",
	"npmcache":     "nodejs",
	"gnupg":        "gpg",
}

// Norm reduces a name to lowercase letters and digits.
func Norm(s string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(s), "")
}

// Tokens splits a name into words: "MongoDBCompass" -> [mongo dbcompass], "aider-desk" -> [aider desk].
func Tokens(s string) []string {
	var b strings.Builder
	prevLower := false
	for _, r := range s {
		if unicode.IsUpper(r) && prevLower {
			b.WriteRune(' ')
		}
		prevLower = unicode.IsLower(r)
		b.WriteRune(r)
	}
	var out []string
	for _, t := range nonAlnum.Split(strings.ToLower(b.String()), -1) {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Aligned reports whether needle equals consecutive words of toks
// (or, when at least 8 characters long, the start of a word).
func Aligned(needle string, toks []string) bool {
	for i := range toks {
		if len(needle) >= 8 && strings.HasPrefix(toks[i], needle) {
			return true
		}
		acc := ""
		for _, t := range toks[i:] {
			acc += t
			if acc == needle {
				return true
			}
			if len(acc) >= len(needle) {
				break
			}
		}
	}
	return false
}

type fuzzyKey struct {
	key   string
	toks  []string
	label string
}

type location struct {
	key   string
	label string
}

// Matcher links a folder to evidence of an installed program.
type Matcher struct {
	exact     map[string]string
	keys      []fuzzyKey
	locations []location
}

// New returns an empty matcher.
func New() *Matcher {
	return &Matcher{exact: map[string]string{}}
}

func (m *Matcher) addKey(raw, label string, fuzzy bool) {
	k := Norm(raw)
	if len(k) < 3 {
		return
	}
	if _, ok := m.exact[k]; !ok {
		m.exact[k] = label
	}
	if fuzzy && !GenericKeys[k] {
		m.keys = append(m.keys, fuzzyKey{k, Tokens(raw), label})
	}
}

// Add records a piece of evidence.
func (m *Matcher) Add(ev model.Evidence) {
	if ev.DictOnly {
		return
	}
	label := fmt.Sprintf("%s [%s]", ev.Name, ev.Source)
	fuzzy := !ev.ExactOnly
	base := versionTail.ReplaceAllString(ev.Name, "")
	seen := map[string]bool{}
	for _, raw := range []string{ev.Name, base, parens.ReplaceAllString(base, "")} {
		if !seen[raw] {
			seen[raw] = true
			m.addKey(raw, label, fuzzy)
		}
	}
	if ev.Publisher != "" {
		m.addKey(company.ReplaceAllString(ev.Publisher, ""),
			fmt.Sprintf("%s [%s, publisher: %s]", ev.Name, ev.Source, ev.Publisher), true)
	}
	if loc := strings.Trim(strings.TrimSpace(ev.Location), `"`); len(loc) > 3 && filepath.IsAbs(loc) {
		m.locations = append(m.locations, location{pathutil.Key(loc), fmt.Sprintf("%s [%s, path]", ev.Name, ev.Source)})
		m.addKey(filepath.Base(filepath.Clean(loc)), label, true)
	}
}

// Find returns a description of the program the folder belongs to, or "".
func (m *Matcher) Find(folder string) string {
	f := pathutil.Key(folder)
	for _, l := range m.locations {
		if pathutil.IsUnder(l.key, f) {
			return l.label
		}
	}

	name := updaterTail.ReplaceAllString(strings.TrimLeft(filepath.Base(folder), "."), "")
	name = strings.TrimSuffix(name, ".app")
	n := Norm(name)
	if len(n) < 3 {
		return ""
	}
	targets := []string{n}
	if a, ok := Aliases[n]; ok {
		targets = append(targets, a)
	}
	for _, t := range targets {
		if label, ok := m.exact[t]; ok {
			return label
		}
	}
	folderToks := Tokens(name)
	for _, t := range targets {
		tToks := []string{t}
		if t == n {
			tToks = folderToks
		}
		for _, k := range m.keys {
			if (len(k.key) >= 5 && Aligned(k.key, tToks)) || (len(t) >= 5 && Aligned(t, k.toks)) {
				return fmt.Sprintf("%s (similar to '%s')", k.label, k.key)
			}
		}
	}
	return ""
}
