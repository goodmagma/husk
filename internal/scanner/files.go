package scanner

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/goodmagma/husk/dictionary"
	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
	"github.com/goodmagma/husk/internal/platform"
)

// Loose files in the profile folder: junk extensions are always listed,
// other files only when they are large.
var (
	junkKind = map[string]string{
		".log":   "logs",
		".dmp":   "dump",
		".mdmp":  "dump",
		".hprof": "dump",
		".tmp":   "cache",
	}
	largeFile int64 = 100 << 20
	digitRun        = regexp.MustCompile(`\d+`)
)

// fileGroup is one file, or files sharing a pattern, reported as a single row.
type fileGroup struct {
	pattern string
	files   []string
	entry   *dictionary.Entry // nil when found by the heuristic
	kind    string
}

// scanFiles lists the files known to the dictionary and the loose files in the profile folder.
func scanFiles(dict *dictionary.Dictionary, roots []platform.Root, opt Options, now time.Time,
	ignored func(path string) bool) []model.Result {

	var groups []fileGroup
	known := map[string]bool{}
	for _, g := range dict.ResolveFiles() {
		var files []string
		for _, f := range g.Files {
			known[pathutil.Key(f)] = true
			if !ignored(f) {
				files = append(files, f)
			}
		}
		if len(files) > 0 {
			groups = append(groups, fileGroup{pattern: displayPattern(g.Pattern, files), files: files, entry: g.Entry, kind: g.Kind})
		}
	}
	for _, r := range roots {
		if r.Area == "Profile" {
			groups = append(groups, looseFiles(r.Path, known, ignored)...)
		}
	}

	var out []model.Result
	for _, g := range groups {
		if r := classifyFiles(g, measureFiles(g.files), now, opt, areaOf(g.files[0], roots)); r != nil {
			out = append(out, *r)
		}
	}
	return out
}

// looseFiles groups the junk and large files directly inside dir; names that differ
// only in their digits (jcef_1234.log, jcef_5678.log) form one group.
func looseFiles(dir string, known map[string]bool, ignored func(string) bool) []fileGroup {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byKey := map[string]*fileGroup{}
	var keys []string
	for _, e := range entries {
		if !e.Type().IsRegular() || platform.IsSystemFile(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if known[pathutil.Key(p)] || ignored(p) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		kind, junk := junkKind[strings.ToLower(filepath.Ext(e.Name()))]
		if !junk && info.Size() < largeFile {
			continue
		}
		key := digitRun.ReplaceAllString(e.Name(), "*")
		g, ok := byKey[key]
		if !ok {
			g = &fileGroup{pattern: filepath.Join(dir, key), kind: kind}
			byKey[key] = g
			keys = append(keys, key)
		}
		g.files = append(g.files, p)
	}
	sort.Strings(keys)
	out := make([]fileGroup, 0, len(keys))
	for _, k := range keys {
		g := byKey[k]
		g.pattern = displayPattern(g.pattern, g.files)
		out = append(out, *g)
	}
	return out
}

// displayPattern shows a single file by its own name and a group by its pattern.
func displayPattern(pattern string, files []string) string {
	if len(files) == 1 {
		return files[0]
	}
	return pattern
}

func measureFiles(files []string) measurement {
	var m measurement
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		m.size += st.Size()
		m.files++
		if t := st.ModTime().Unix(); t > m.last {
			m.last = t
		}
	}
	return m
}

func classifyFiles(g fileGroup, m measurement, now time.Time, opt Options, area string) *model.Result {
	if g.entry == nil && m.size < opt.MinSize {
		return nil
	}
	r := &model.Result{Type: model.TypeFiles, Path: g.pattern, Area: area, Size: m.size, Files: m.files,
		LastWrite: m.last, Kind: g.kind}
	old := now.Sub(time.Unix(m.last, 0)) >= time.Duration(opt.Days)*24*time.Hour
	var extra []string

	if e := g.entry; e != nil {
		r.Source = model.SourceDictionary
		// the notes of an entry describe its folders and settings, not its logs and dumps
		if e.Notes != "" && !model.DisposableKinds[g.kind] {
			extra = append(extra, e.Notes)
		}
		switch {
		case model.DisposableKinds[g.kind]:
			r.Status, r.Match = model.Disposable, e.Name
			if !e.Shared {
				if e.Installed {
					r.Match += " (installed; the files can be deleted anyway)"
				} else {
					r.Match += ": not detected as installed"
				}
			}
		case e.Shared:
			r.Status, r.Match = model.Shared, e.Name
		case e.Installed:
			r.Status, r.Match = model.Associated, e.Name+" ("+e.DetectedBy+")"
		default:
			r.Status, r.Match = model.Orphan, e.Name+": not detected as installed"
		}
	} else {
		r.Source = model.SourceHeuristic
		switch {
		case g.kind != "":
			r.Status = model.Disposable
			extra = append(extra, "log, dump or temporary file in the profile folder")
		case old:
			r.Status = model.Orphan
			extra = append(extra, "large file in the profile folder")
		default:
			r.Status = model.Suspect
			extra = append(extra, "large file in the profile folder")
		}
	}
	if m.size == 0 {
		extra = append(extra, "empty files")
	}
	r.Details = strings.Join(extra, " · ")
	return r
}

// areaOf returns the area of the root that directly contains path.
func areaOf(path string, roots []platform.Root) string {
	parent := pathutil.Key(filepath.Dir(path))
	for _, r := range roots {
		if pathutil.Key(r.Path) == parent {
			return r.Area
		}
	}
	return "Dictionary"
}
