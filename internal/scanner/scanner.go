// Package scanner scans folders and classifies them.
package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goodmagma/husk/dictionary"
	"github.com/goodmagma/husk/internal/match"
	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
	"github.com/goodmagma/husk/internal/platform"
)

// Options are the parameters of a scan.
type Options struct {
	Days    int   // days without changes before a folder is an orphan
	MinSize int64 // minimum size in bytes for folders found by the heuristic
	All     bool  // include system folders
	Workers int
}

// DefaultOptions returns the default parameters.
func DefaultOptions() Options {
	return Options{Days: 90, Workers: 8}
}

// SourceCount is the amount of evidence collected from a source.
type SourceCount struct {
	Label string
	Count int
}

// Report is the outcome of a scan.
type Report struct {
	Options    Options
	Started    time.Time
	Duration   time.Duration
	Results    []model.Result
	PathIssues []model.PathIssue
	Evidence   []model.Evidence
	Sources    []SourceCount
	Dictionary *dictionary.Dictionary
	Installed  int // dictionary entries detected as installed
	Apps       int // [[app]] dictionary entries
	KnownPaths int // dictionary folders found on disk
}

// Progress receives the progress: the current phase and, while measuring, folders done and total.
type Progress func(phase string, done, total int)

// Run performs a full scan.
func Run(ctx context.Context, opt Options, progress Progress) (*Report, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	if opt.Workers < 1 {
		opt.Workers = 1
	}
	rep := &Report{Options: opt, Started: time.Now()}

	// 1) Installed programs
	progress("Collecting installed programs", 0, 0)
	var evidence []model.Evidence
	for _, src := range platform.Sources() {
		items := src.Read()
		rep.Sources = append(rep.Sources, SourceCount{src.Label, len(items)})
		evidence = append(evidence, items...)
	}
	pathEv, pathExes := platform.PathExecutables()
	rep.Sources = append(rep.Sources, SourceCount{"PATH", len(pathEv)})
	evidence = append(evidence, pathEv...)
	roots := platform.ExistingRoots()
	portable := platform.PortablePrograms(roots)
	rep.Sources = append(rep.Sources, SourceCount{"portable", len(portable)})
	evidence = append(evidence, portable...)
	rep.Evidence = evidence
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matcher := match.New()
	var installedNames []string
	for _, ev := range evidence {
		matcher.Add(ev)
		if !strings.HasPrefix(ev.Source, "PATH") {
			installedNames = append(installedNames, ev.Name)
		}
	}

	// 2) Dictionary
	progress("Loading the dictionary", 0, 0)
	dict := dictionary.LoadAll()
	dict.Detect(installedNames, pathExes)
	dictIndex := dict.ResolvePaths()
	rep.Dictionary = dict
	rep.KnownPaths = len(dictIndex)
	for _, e := range dict.List() {
		if !e.Shared {
			rep.Apps++
		}
		if e.Installed {
			rep.Installed++
		}
	}
	portableOwner := map[string][]string{}
	for _, e := range dict.List() {
		hit := strings.TrimPrefix(e.DetectedBy, "installed: ")
		if !e.Installed || hit == e.DetectedBy {
			continue
		}
		for _, ev := range portable {
			if ev.Name == hit {
				k := pathutil.Key(ev.Location)
				portableOwner[k] = appendUnique(portableOwner[k], e.Name)
			}
		}
	}

	isIgnored := func(p, area string) bool {
		k, name := pathutil.Key(p), filepath.Base(p)
		if dict.IgnorePaths[k] || dict.IgnoreNames[strings.ToLower(name)] {
			return true
		}
		if _, ok := dictIndex[k]; ok {
			return false // the dictionary takes precedence over the built-in exclusions
		}
		return platform.IsSystemFolder(name, area)
	}

	// 3) Folders to scan
	candidates := enumerate(roots)
	candKeys := map[string]bool{}
	for _, c := range candidates {
		candKeys[pathutil.Key(c.path)] = true
	}
	ignoredKeys := map[string]bool{}
	for i := range candidates {
		c := &candidates[i]
		c.ignored = isIgnored(c.path, c.area)
		if c.ignored {
			ignoredKeys[pathutil.Key(c.path)] = true
		}
	}
	contains := map[string][]string{}
	for _, k := range sortedKeys(dictIndex) {
		res := dictIndex[k]
		if candKeys[k] {
			continue
		}
		// deeper folder: if an ancestor is already scanned, note it there
		ancestor := ""
		for parent := pathutil.Parent(k); parent != ""; parent = pathutil.Parent(parent) {
			if candKeys[parent] {
				ancestor = parent
				break
			}
		}
		if ancestor != "" && !ignoredKeys[ancestor] {
			contains[ancestor] = appendUnique(contains[ancestor], res.Entry.Name)
		} else {
			candidates = append(candidates, candidate{path: res.Path, area: "Dictionary"})
			candKeys[k] = true
		}
	}

	var todo []candidate
	for _, c := range candidates {
		if opt.All || !c.ignored {
			todo = append(todo, c)
		}
	}

	// 4) Measurement and classification
	now := time.Now()
	results := make([]*model.Result, len(todo))
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	jobs := make(chan int)
	for w := 0; w < opt.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				results[i] = classify(todo[i], measure(ctx, todo[i].path), now, opt, dictIndex, contains, portableOwner, matcher)
				mu.Lock()
				done++
				progress("Scanning folders", done, len(todo))
				mu.Unlock()
			}
		}()
	}
	for i := range todo {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for _, r := range results {
		if r != nil {
			rep.Results = append(rep.Results, *r)
		}
	}
	order := map[model.Status]int{}
	for i, s := range model.StatusOrder {
		order[s] = i
	}
	sort.SliceStable(rep.Results, func(i, j int) bool {
		a, b := rep.Results[i], rep.Results[j]
		if order[a.Status] != order[b.Status] {
			return order[a.Status] < order[b.Status]
		}
		return a.Size > b.Size
	})

	// 5) PATH variable
	progress("Checking PATH", 0, 0)
	rep.PathIssues = CheckPath(platform.PathVariables(), rep.Results, func(app string) (bool, bool) {
		e, ok := dict.Entries[strings.ToLower(app)]
		if !ok {
			return false, false
		}
		return e.Installed, true
	})
	rep.Duration = time.Since(rep.Started)
	return rep, nil
}

type candidate struct {
	path    string
	area    string
	ignored bool
}

func enumerate(roots []platform.Root) []candidate {
	rootKeys := map[string]bool{}
	for _, r := range roots {
		rootKeys[pathutil.Key(r.Path)] = true
	}
	var out []candidate
	for _, r := range roots {
		entries, err := os.ReadDir(r.Path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || platform.IsLink(e) {
				continue
			}
			if r.DotOnly && !strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(r.Path, e.Name())
			if rootKeys[pathutil.Key(p)] {
				continue // scanned as a root of its own
			}
			out = append(out, candidate{path: p, area: r.Area})
		}
	}
	return out
}

type measurement struct {
	size, files, last int64
}

func measure(ctx context.Context, dir string) measurement {
	var m measurement
	if st, err := os.Stat(dir); err == nil {
		m.last = st.ModTime().Unix()
	}
	stack := []string{dir}
	for len(stack) > 0 && ctx.Err() == nil {
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if platform.IsLink(e) {
				continue
			}
			if e.IsDir() {
				stack = append(stack, filepath.Join(d, e.Name()))
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			m.size += info.Size()
			m.files++
			if t := info.ModTime().Unix(); t > m.last {
				m.last = t
			}
		}
	}
	return m
}

func classify(c candidate, m measurement, now time.Time, opt Options, dictIndex map[string]dictionary.Resolved,
	contains, portableOwner map[string][]string, matcher *match.Matcher) *model.Result {

	k := pathutil.Key(c.path)
	res, inDict := dictIndex[k]
	if m.size < opt.MinSize && !inDict {
		return nil // dictionary folders are always listed
	}
	r := &model.Result{Path: c.path, Area: c.area, Size: m.size, Files: m.files, LastWrite: m.last}
	old := now.Sub(time.Unix(m.last, 0)) >= time.Duration(opt.Days)*24*time.Hour
	var extra []string
	if names := contains[k]; len(names) > 0 {
		sort.Strings(names)
		extra = append(extra, "contains folders of: "+strings.Join(names, ", "))
	}

	switch {
	case c.ignored:
		r.Status = model.Ignored
	case inDict:
		e := res.Entry
		r.Source, r.Kind = model.SourceDictionary, res.Kind
		if e.Notes != "" {
			extra = append(extra, e.Notes)
		}
		if e.Clean != "" {
			extra = append(extra, "cleanup: "+e.Clean)
		}
		switch {
		case e.Shared:
			r.Status, r.Match = model.Shared, e.Name
		case e.Installed:
			r.Status, r.Match = model.Associated, e.Name+" ("+e.DetectedBy+")"
		default:
			r.Status, r.Match = model.Orphan, e.Name+": not detected as installed"
			if m.files == 0 {
				extra = append(extra, "empty folder")
			} else if !old {
				extra = append(extra, "recently modified: check that the program is not portable")
			}
		}
	default:
		r.Source = model.SourceHeuristic
		r.Match = matcher.Find(c.path)
		switch {
		case r.Match != "":
			r.Status = model.Associated
		case old || m.files == 0:
			// a folder without files and without a program is a leftover even if recently modified
			r.Status = model.Orphan
		default:
			r.Status = model.Suspect
		}
		if r.Match == "" && m.files == 0 {
			extra = append(extra, "empty folder")
		}
		if owners := portableOwner[k]; r.Match == "" && len(owners) > 0 {
			sort.Strings(owners)
			r.Status, r.Match = model.Associated, strings.Join(owners, ", ")+" (executables in the folder)"
		}
		if r.Match == "" && isProgramArea(c.area) {
			if exes := platform.FindExecutables(c.path); len(exes) > 0 {
				r.Status = model.Portable
				extra = append(extra, "contains executables ("+strings.Join(exes, ", ")+
					"): maybe installed from a zip file; if you use it, add it to apps.user.toml")
			}
		}
	}
	r.Details = strings.Join(extra, " · ")
	return r
}

func isProgramArea(area string) bool {
	for _, r := range platform.Roots() {
		if r.Area == area {
			return r.ProgramArea
		}
	}
	return false
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
