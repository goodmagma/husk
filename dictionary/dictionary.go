// Package dictionary loads the dictionary of known programs (one TOML file per operating system)
// and the user's personal entries.
package dictionary

import (
	_ "embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/goodmagma/husk/internal/pathutil"
)

//go:embed windows.toml
var windowsTOML []byte

//go:embed linux.toml
var linuxTOML []byte

//go:embed darwin.toml
var darwinTOML []byte

// Builtin returns the built-in dictionary for the given operating system.
func Builtin(goos string) (name string, data []byte) {
	switch goos {
	case "windows":
		return "windows.toml", windowsTOML
	case "darwin":
		return "darwin.toml", darwinTOML
	default:
		return "linux.toml", linuxTOML
	}
}

// PathSpec is a program folder; it may contain wildcards and variables.
type PathSpec struct {
	Pattern string
	Kind    string
}

// Entry is a dictionary entry: a program ([[app]]) or a folder shared by several programs ([[shared]]).
type Entry struct {
	Name     string
	Origin   string
	Category string
	Shared   bool
	Names    []string // patterns matched against installed program names
	Exe      []string // executables looked up in PATH
	Files    []string // files whose presence means the program is installed
	Paths    []PathSpec
	Notes    string
	Clean    string

	Installed  bool
	DetectedBy string
}

// Dictionary holds the entries and the exclusions.
type Dictionary struct {
	Entries     map[string]*Entry
	order       []string
	IgnoreNames map[string]bool
	IgnorePaths map[string]bool
	Loaded      []string
	Errors      []string
}

// New returns an empty dictionary.
func New() *Dictionary {
	return &Dictionary{
		Entries:     map[string]*Entry{},
		IgnoreNames: map[string]bool{},
		IgnorePaths: map[string]bool{},
	}
}

// List returns the entries in load order.
func (d *Dictionary) List() []*Entry {
	out := make([]*Entry, 0, len(d.order))
	for _, k := range d.order {
		out = append(out, d.Entries[k])
	}
	return out
}

type rawFile struct {
	App    []map[string]any `toml:"app"`
	Shared []map[string]any `toml:"shared"`
	Ignore struct {
		Names []string `toml:"names"`
		Paths []string `toml:"paths"`
	} `toml:"ignore"`
}

// LoadFile loads a file if it exists.
func (d *Dictionary) LoadFile(file, origin string) {
	data, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", file, err))
		}
		return
	}
	d.Load(filepath.Base(file), data, origin)
}

// Load adds the entries of a TOML file. An entry with the same name as one already loaded
// extends it: detection rules and paths are merged; category, notes and clean
// (when set) replace the previous values.
func (d *Dictionary) Load(name string, data []byte, origin string) {
	var raw rawFile
	if _, err := toml.Decode(string(data), &raw); err != nil {
		d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", name, err))
		return
	}
	for _, sec := range []struct {
		items  []map[string]any
		shared bool
	}{{raw.App, false}, {raw.Shared, true}} {
		for _, item := range sec.items {
			e, err := parseEntry(item, origin, sec.shared)
			if err != nil {
				d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			d.add(e)
		}
	}
	for _, n := range raw.Ignore.Names {
		d.IgnoreNames[strings.ToLower(n)] = true
	}
	for _, p := range raw.Ignore.Paths {
		d.IgnorePaths[pathutil.Key(pathutil.Expand(p))] = true
	}
	d.Loaded = append(d.Loaded, fmt.Sprintf("%s (%s)", name, origin))
}

func (d *Dictionary) add(e *Entry) {
	key := strings.ToLower(e.Name)
	base, ok := d.Entries[key]
	if !ok {
		d.Entries[key] = e
		d.order = append(d.order, key)
		return
	}
	base.Origin += " + " + e.Origin
	if e.Category != "" {
		base.Category = e.Category
	}
	base.Names = append(base.Names, e.Names...)
	base.Exe = append(base.Exe, e.Exe...)
	base.Files = append(base.Files, e.Files...)
	base.Paths = append(base.Paths, e.Paths...)
	if e.Notes != "" {
		base.Notes = e.Notes
	}
	if e.Clean != "" {
		base.Clean = e.Clean
	}
}

func parseEntry(m map[string]any, origin string, shared bool) (*Entry, error) {
	name, _ := m["name"].(string)
	if name == "" {
		sec := "app"
		if shared {
			sec = "shared"
		}
		return nil, fmt.Errorf("[[%s]] entry without 'name'", sec)
	}
	e := &Entry{Name: name, Origin: origin, Shared: shared}
	e.Category, _ = m["category"].(string)
	e.Clean, _ = m["clean"].(string)
	var notes []string
	for _, k := range []string{"used_by", "notes"} {
		if s, _ := m[k].(string); s != "" {
			notes = append(notes, strings.TrimSuffix(s, "."))
		}
	}
	if len(notes) > 0 {
		e.Notes = strings.Join(notes, ". ") + "."
	}

	if det, ok := m["detect"].(map[string]any); ok {
		e.Names = strList(det["names"])
		e.Exe = strList(det["exe"])
		e.Files = strList(det["files"])
	}

	defKind, _ := m["kind"].(string)
	if p, ok := m["path"].(string); ok {
		e.Paths = append(e.Paths, PathSpec{p, defKind})
	}
	if list, ok := m["paths"].([]any); ok {
		for _, it := range list {
			switch v := it.(type) {
			case string:
				e.Paths = append(e.Paths, PathSpec{v, defKind})
			case map[string]any:
				p, _ := v["path"].(string)
				if p == "" {
					return nil, fmt.Errorf("%s: path entry without 'path'", name)
				}
				kind, _ := v["kind"].(string)
				if kind == "" {
					kind = defKind
				}
				e.Paths = append(e.Paths, PathSpec{p, kind})
			}
		}
	}
	return e, nil
}

func strList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, it := range list {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Detect marks the entries whose program is installed.
func (d *Dictionary) Detect(installedNames []string, pathExes map[string]bool) {
	lowered := make([]string, len(installedNames))
	for i, n := range installedNames {
		lowered[i] = strings.ToLower(n)
	}
	for _, e := range d.List() {
		e.Installed, e.DetectedBy = false, ""
		if e.Shared {
			continue
		}
		how := ""
	names:
		for _, pat := range e.Names {
			pl := strings.ToLower(pat)
			for i, low := range lowered {
				if ok, _ := path.Match(pl, low); ok {
					how = "installed: " + installedNames[i]
					break names
				}
			}
		}
		if how == "" {
			for _, x := range e.Exe {
				if pathExes[strings.ToLower(x)] {
					how = "executable in PATH: " + x
					break
				}
			}
		}
		if how == "" {
			for _, f := range e.Files {
				if m, _ := filepath.Glob(pathutil.Expand(f)); len(m) > 0 {
					how = "file found: " + f
					break
				}
			}
		}
		e.Installed = how != ""
		e.DetectedBy = how
	}
}

// Resolved is a dictionary folder that exists on disk.
type Resolved struct {
	Entry *Entry
	Kind  string
	Path  string
}

// FileGroup is a set of existing files matching one path pattern of an entry.
type FileGroup struct {
	Entry   *Entry
	Kind    string
	Pattern string // expanded pattern, e.g. C:\Users\me\jcef_*.log
	Files   []string
}

// ResolveFiles returns the files (not folders) matched by the dictionary paths,
// one group per pattern. A file matched by several patterns belongs to the first one.
func (d *Dictionary) ResolveFiles() []FileGroup {
	var out []FileGroup
	seen := map[string]bool{}
	for _, e := range d.List() {
		for _, ps := range e.Paths {
			pattern := pathutil.Expand(ps.Pattern)
			matches, _ := filepath.Glob(pattern)
			var files []string
			for _, p := range matches {
				st, err := os.Lstat(p)
				k := pathutil.Key(p)
				if err != nil || !st.Mode().IsRegular() || seen[k] {
					continue
				}
				seen[k] = true
				files = append(files, p)
			}
			if len(files) > 0 {
				out = append(out, FileGroup{e, ps.Kind, pattern, files})
			}
		}
	}
	return out
}

// ResolvePaths returns the existing dictionary folders, keyed by path.
func (d *Dictionary) ResolvePaths() map[string]Resolved {
	out := map[string]Resolved{}
	for _, e := range d.List() {
		for _, ps := range e.Paths {
			matches, _ := filepath.Glob(pathutil.Expand(ps.Pattern))
			for _, p := range matches {
				if st, err := os.Stat(p); err != nil || !st.IsDir() {
					continue
				}
				k := pathutil.Key(p)
				if _, dup := out[k]; !dup {
					out[k] = Resolved{e, ps.Kind, p}
				}
			}
		}
	}
	return out
}

// Owner returns the entry whose folders contain p (p itself or one of its ancestors),
// whether or not the folder exists. When several entries match, the most specific wins.
func (d *Dictionary) Owner(p string) (*Entry, bool) {
	sep := string(filepath.Separator)
	parts := strings.Split(pathutil.Key(p), sep)
	var best *Entry
	bestLen := 0
	for _, e := range d.List() {
		for _, ps := range e.Paths {
			pattern := strings.Split(pathutil.Key(pathutil.Expand(ps.Pattern)), sep)
			if len(pattern) <= bestLen || len(pattern) > len(parts) {
				continue
			}
			if matchParts(pattern, parts) {
				best, bestLen = e, len(pattern)
			}
		}
	}
	return best, best != nil
}

func matchParts(pattern, parts []string) bool {
	for i, pat := range pattern {
		if ok, err := filepath.Match(pat, parts[i]); err != nil || !ok {
			return false
		}
	}
	return true
}

// UserDirs returns the folders searched for apps.user.toml and ignore.txt:
// the executable folder and the user configuration folder (e.g. %APPDATA%\husk).
func UserDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(cfg, "husk"))
	}
	return dirs
}

// LoadIgnoreTxt reads a simple exclusion file: one folder name or path per line.
func (d *Dictionary) LoadIgnoreTxt(file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.ContainsAny(t, `\/%$~`) {
			d.IgnorePaths[pathutil.Key(pathutil.Expand(t))] = true
		} else {
			d.IgnoreNames[strings.ToLower(t)] = true
		}
	}
}

// LoadAll loads the built-in dictionary and the user's files.
func LoadAll() *Dictionary {
	d := New()
	name, data := Builtin(runtime.GOOS)
	d.Load(name, data, "built-in")
	for _, dir := range UserDirs() {
		d.LoadFile(filepath.Join(dir, "apps.user.toml"), "user")
		d.LoadIgnoreTxt(filepath.Join(dir, "ignore.txt"))
	}
	return d
}
