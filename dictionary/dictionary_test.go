package dictionary

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// Checks on the built-in dictionaries, run by the CI on every pull request.
// The rules are described in CONTRIBUTING.md.

var (
	entryKeys  = set("name", "category", "detect", "path", "paths", "kind", "notes", "used_by", "clean")
	detectKeys = set("names", "exe", "files")
	pathKeys   = set("path", "kind")
	kinds      = set("app", "config", "cache", "models", "logs", "dump", "data")
	categories = set("ai", "browser", "communication", "development", "driver", "games", "multimedia", "utilities")

	// personal paths: a user name written in the path instead of a variable or ~
	personal = regexp.MustCompile(`(?i)^([a-z]:\\users\\|/home/|/users/|/root/)`)
	winVar   = regexp.MustCompile(`%[A-Za-z0-9_()]+%`)
	unixVar  = regexp.MustCompile(`\$\{?[A-Za-z_]`)
)

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range items {
		m[s] = true
	}
	return m
}

func TestBuiltinDictionaries(t *testing.T) {
	for _, goos := range []string{"windows", "linux", "darwin"} {
		name, data := Builtin(goos)
		t.Run(name, func(t *testing.T) {
			d := New()
			d.Load(name, data, "builtin")
			for _, e := range d.Errors {
				t.Error(e)
			}

			var raw rawFile
			if _, err := toml.Decode(string(data), &raw); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, sec := range []struct {
				label string
				items []map[string]any
			}{{"[[app]]", raw.App}, {"[[shared]]", raw.Shared}} {
				for i, item := range sec.items {
					entryName, _ := item["name"].(string)
					where := sec.label + " " + entryName
					if entryName == "" {
						where = fmt.Sprintf("%s #%d", sec.label, i+1)
					}
					for _, msg := range checkEntry(item, sec.label == "[[shared]]", goos) {
						t.Errorf("%s: %s", where, msg)
					}
					key := strings.ToLower(entryName)
					if key != "" && seen[key] {
						t.Errorf("%s: duplicate name (merge the two entries)", where)
					}
					seen[key] = true
				}
			}
		})
	}
}

// checkEntry returns the problems of one dictionary entry.
func checkEntry(m map[string]any, shared bool, goos string) []string {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	for _, k := range sortedKeys(m) {
		if !entryKeys[k] {
			add("unknown key %q", k)
		}
	}
	if s, _ := m["name"].(string); strings.TrimSpace(s) == "" {
		add("missing name")
	}

	if shared {
		if _, ok := m["detect"]; ok {
			add("[[shared]] entries have no detect rules")
		}
		if s, _ := m["used_by"].(string); s == "" {
			add("missing used_by (which programs use this folder)")
		}
	} else {
		cat, _ := m["category"].(string)
		if !categories[cat] {
			add("category %q is not one of %s", cat, strings.Join(sortedKeys(categories), ", "))
		}
		det, _ := m["detect"].(map[string]any)
		if len(det) == 0 {
			add("missing detect rules (names, exe or files): without them the program is never detected as installed")
		}
		for _, k := range sortedKeys(det) {
			if !detectKeys[k] {
				add("unknown key detect.%s", k)
			}
			list, ok := det[k].([]any)
			if !ok || len(list) == 0 {
				add("detect.%s must be a non-empty list of strings", k)
				continue
			}
			for _, it := range list {
				s, ok := it.(string)
				if !ok || s == "" {
					add("detect.%s contains an empty or non-string item", k)
					continue
				}
				if _, err := path.Match(strings.ToLower(s), ""); err != nil {
					add("detect.%s: invalid pattern %q", k, s)
				}
				if k == "files" {
					errs = append(errs, checkPath(s, goos)...)
				}
			}
		}
	}

	if k, ok := m["kind"].(string); ok && !kinds[k] {
		add("kind %q is not one of %s", k, strings.Join(sortedKeys(kinds), ", "))
	}
	defKind, _ := m["kind"].(string)

	var paths []string
	if p, ok := m["path"].(string); ok {
		paths = append(paths, p)
		if defKind == "" {
			add("path %q has no kind", p)
		}
	}
	list, _ := m["paths"].([]any)
	if _, ok := m["paths"]; ok && list == nil {
		add("paths must be a list")
	}
	for _, it := range list {
		switch v := it.(type) {
		case string:
			paths = append(paths, v)
			if defKind == "" {
				add("path %q has no kind", v)
			}
		case map[string]any:
			for _, k := range sortedKeys(v) {
				if !pathKeys[k] {
					add("unknown key %q in paths", k)
				}
			}
			p, _ := v["path"].(string)
			if p == "" {
				add("paths item without path")
				continue
			}
			paths = append(paths, p)
			kind, _ := v["kind"].(string)
			if kind == "" {
				kind = defKind
			}
			if !kinds[kind] {
				add("path %q: kind %q is not one of %s", p, kind, strings.Join(sortedKeys(kinds), ", "))
			}
		default:
			add("paths item must be a string or { path = '...', kind = \"...\" }")
		}
	}
	if len(paths) == 0 {
		add("no path or paths")
	}
	for _, p := range paths {
		errs = append(errs, checkPath(p, goos)...)
	}
	return errs
}

// checkPath checks a path written in the dictionary of the given system.
func checkPath(p, goos string) []string {
	var errs []string
	if strings.TrimSpace(p) != p {
		errs = append(errs, fmt.Sprintf("path %q has leading or trailing spaces", p))
	}
	if personal.MatchString(p) {
		errs = append(errs, fmt.Sprintf("path %q contains a user name: use a variable (%%USERPROFILE%%, ~)", p))
	}
	if goos == "windows" {
		if unixVar.MatchString(p) || strings.HasPrefix(p, "~") {
			errs = append(errs, fmt.Sprintf("path %q: use %%VARIABLES%% on Windows, not $VAR or ~", p))
		}
		if strings.Contains(p, "/") {
			errs = append(errs, fmt.Sprintf("path %q: use \\ on Windows", p))
		}
		if !winVar.MatchString(p) {
			errs = append(errs, fmt.Sprintf("path %q must start from a variable (%%USERPROFILE%%, %%APPDATA%%, ...)", p))
		}
	} else {
		if winVar.MatchString(p) || strings.Contains(p, `\`) {
			errs = append(errs, fmt.Sprintf("path %q: use ~ or $VARIABLES and / on %s", p, goos))
		}
		if !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "$") {
			errs = append(errs, fmt.Sprintf("path %q must start with ~/, / or a $VARIABLE", p))
		}
	}
	// wildcards: path.Match reports a bad pattern (e.g. an unclosed [) whatever the separator
	if _, err := path.Match(strings.ReplaceAll(p, `\`, "/"), ""); err != nil {
		errs = append(errs, fmt.Sprintf("path %q: invalid wildcard pattern", p))
	}
	return errs
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestCheckEntry(t *testing.T) {
	bad := map[string]any{
		"name":     "Foo",
		"category": "tools",
		"detct":    map[string]any{},
		"paths": []any{
			map[string]any{"path": `C:\Users\mario\.foo`, "kind": "stuff"},
			map[string]any{"path": `~/.foo`, "kind": "cache"},
		},
	}
	errs := checkEntry(bad, false, "windows")
	for _, want := range []string{`unknown key "detct"`, `category "tools"`, "missing detect", "contains a user name", `kind "stuff"`, "not $VAR or ~"} {
		found := false
		for _, e := range errs {
			found = found || strings.Contains(e, want)
		}
		if !found {
			t.Errorf("expected an error containing %q, got %q", want, errs)
		}
	}

	good := map[string]any{
		"name":     "Foo",
		"category": "utilities",
		"detect":   map[string]any{"names": []any{"Foo*"}},
		"paths":    []any{map[string]any{"path": `%APPDATA%\Foo`, "kind": "config"}},
	}
	if errs := checkEntry(good, false, "windows"); len(errs) > 0 {
		t.Errorf("unexpected errors: %q", errs)
	}
}
