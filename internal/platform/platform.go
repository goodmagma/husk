// Package platform holds everything that depends on the operating system: folders to scan,
// sources of installed programs, the PATH variable, opening files and folders.
package platform

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
)

// Root is a folder whose first-level subfolders are scanned.
type Root struct {
	Path        string
	Area        string
	ProgramArea bool // program folder: subfolders containing executables are portable programs
	DotOnly     bool // only subfolders starting with "."
}

// Source is a source of installed programs.
type Source struct {
	Label string
	Read  func() []model.Evidence
}

// PathVar is an entry of the PATH variable.
type PathVar struct {
	Scope string
	Entry string
}

// notAppExe excludes installers, uninstallers and updaters from "portable" executables.
var notAppExe = regexp.MustCompile(`(?i)^(unins|uninstall|setup|install|update|vc_?redist|dotnet)`)

// ExistingRoots drops empty, missing and duplicate roots.
func ExistingRoots() []Root {
	var out []Root
	seen := map[string]bool{}
	for _, r := range Roots() {
		if r.Path == "" {
			continue
		}
		k := pathutil.Key(r.Path)
		if seen[k] {
			continue
		}
		if st, err := os.Stat(r.Path); err != nil || !st.IsDir() {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

// PathExecutables reads the executables in the process PATH.
// Folders without executables (entries left behind by an uninstaller) are skipped.
func PathExecutables() ([]model.Evidence, map[string]bool) {
	var out []model.Evidence
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		d = strings.Trim(strings.TrimSpace(d), `"`)
		if d == "" || isSystemPathDir(d) {
			continue
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		var stems []string
		for _, e := range entries {
			if stem, ok := pathProgram(e); ok {
				stems = append(stems, stem)
			}
		}
		if len(stems) == 0 {
			continue
		}
		out = append(out, model.Evidence{Name: filepath.Base(filepath.Clean(d)), Source: "PATH", Location: d})
		for _, s := range stems {
			ls := strings.ToLower(s)
			if !seen[ls] {
				seen[ls] = true
				out = append(out, model.Evidence{Name: s, Source: "PATH (" + d + ")", ExactOnly: true})
			}
		}
	}
	return out, seen
}

var exeCache sync.Map

// FindExecutables looks for programs in the first two levels of a folder:
// a hint of a portable program (installed from a zip file).
func FindExecutables(dir string) []string {
	if v, ok := exeCache.Load(dir); ok {
		return v.([]string)
	}
	const depth, limit = 2, 3
	var found []string
	level := []string{dir}
search:
	for i := 0; i < depth; i++ {
		var next []string
		for _, d := range level {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if isLink(e) {
					continue
				}
				if appProgram(e) {
					found = append(found, e.Name())
					if len(found) >= limit {
						break search
					}
					continue
				}
				if e.IsDir() {
					next = append(next, filepath.Join(d, e.Name()))
				}
			}
		}
		level = next
	}
	exeCache.Store(dir, found)
	return found
}

// PortablePrograms returns, for the dictionary only, the program-area folders that contain
// executables: the folder name and the executable names.
func PortablePrograms(roots []Root) []model.Evidence {
	var out []model.Evidence
	for _, r := range roots {
		if !r.ProgramArea {
			continue
		}
		entries, err := os.ReadDir(r.Path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || isLink(e) {
				continue
			}
			p := filepath.Join(r.Path, e.Name())
			exes := FindExecutables(p)
			if len(exes) == 0 {
				continue
			}
			src := "executables in " + p
			out = append(out, model.Evidence{Name: e.Name(), Source: src, Location: p, DictOnly: true})
			for _, x := range exes {
				out = append(out, model.Evidence{Name: strings.TrimSuffix(x, filepath.Ext(x)), Source: src, Location: p, DictOnly: true})
			}
		}
	}
	return out
}

func isLink(e fs.DirEntry) bool {
	if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return true
	}
	info, err := e.Info()
	if err != nil {
		return true
	}
	return IsReparse(info)
}

// IsLink reports whether a directory entry is a link that must not be followed.
func IsLink(e fs.DirEntry) bool { return isLink(e) }

// HasPrograms reports whether a PATH folder contains anything executable
// (libraries included: some entries exist only to load them).
func HasPrograms(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if _, ok := pathProgram(e); ok || isLibrary(e.Name()) {
			return true
		}
	}
	return false
}

// run executes a command and returns its output, or "" on error.
func run(name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}
