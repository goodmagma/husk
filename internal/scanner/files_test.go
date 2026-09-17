package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goodmagma/husk/dictionary"
	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/platform"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanFiles(t *testing.T) {
	defer func(v int64) { largeFile = v }(largeFile)
	largeFile = 1000

	home := t.TempDir()
	writeFile(t, filepath.Join(home, "jcef_12.log"), 0)
	writeFile(t, filepath.Join(home, "jcef_345.log"), 0)
	writeFile(t, filepath.Join(home, "java_error_in_ide.hprof"), 50)
	writeFile(t, filepath.Join(home, "crash.dmp"), 10)
	writeFile(t, filepath.Join(home, "big.iso"), 2000)
	writeFile(t, filepath.Join(home, "small.txt"), 10)
	writeFile(t, filepath.Join(home, "notes.log"), 5)
	writeFile(t, filepath.Join(home, ".tool.conf.yml"), 5)
	if platform.IsSystemFile("desktop.ini") { // large system file: never listed
		writeFile(t, filepath.Join(home, "desktop.ini"), 5000)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(home, "big.iso"), old, old); err != nil {
		t.Fatal(err)
	}

	d := dictionary.New()
	d.Load("test.toml", []byte(`
[[app]]
name = "IDE"
detect.exe = ["ide"]
paths = [
  { path = '`+filepath.Join(home, "java_error_in_*.hprof")+`', kind = "dump" },
  { path = '`+filepath.Join(home, "jcef_*.log")+`', kind = "logs" },
]

[[app]]
name = "Tool"
detect.exe = ["tool"]
paths = [ { path = '`+filepath.Join(home, ".tool.conf.yml")+`', kind = "config" } ]
`), "test")
	if len(d.Errors) > 0 {
		t.Fatal(d.Errors)
	}
	d.Detect(nil, map[string]bool{"ide": true})

	roots := []platform.Root{{Path: home, Area: "Profile"}}
	ignored := func(p string) bool { return filepath.Base(p) == "notes.log" }
	results := scanFiles(d, roots, DefaultOptions(), time.Now(), ignored)

	got := map[string]model.Result{}
	for _, r := range results {
		if r.Type != model.TypeFiles {
			t.Errorf("%s: type %q", r.Path, r.Type)
		}
		got[filepath.Base(r.Path)] = r
	}
	want := map[string]model.Status{
		"jcef_*.log":              model.Disposable, // dictionary group, program installed
		"java_error_in_ide.hprof": model.Disposable, // single file shown by name
		".tool.conf.yml":          model.Orphan,     // config of a program that is not installed
		"crash.dmp":               model.Disposable, // heuristic: junk extension
		"big.iso":                 model.Orphan,     // heuristic: large and old
	}
	for name, status := range want {
		r, ok := got[name]
		if !ok {
			t.Errorf("%s: not reported", name)
			continue
		}
		if r.Status != status {
			t.Errorf("%s: status %s, want %s", name, r.Status, status)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d results, want %d: %v", len(got), len(want), got)
	}
	if r := got["jcef_*.log"]; r.Files != 2 || r.Source != model.SourceDictionary ||
		!strings.Contains(r.Match, "installed; the files can be deleted anyway") {
		t.Errorf("jcef group: %+v", r)
	}
	if r := got["crash.dmp"]; r.Kind != "dump" || r.Source != model.SourceHeuristic {
		t.Errorf("crash.dmp: %+v", r)
	}
}

func TestLooseFilesGrouping(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a_1.log", "a_22.log", "b.log", "c_3.tmp"} {
		writeFile(t, filepath.Join(dir, n), 1)
	}
	groups := looseFiles(dir, map[string]bool{}, func(string) bool { return false })
	names := map[string]int{}
	for _, g := range groups {
		names[filepath.Base(g.pattern)] = len(g.files)
	}
	want := map[string]int{"a_*.log": 2, "b.log": 1, "c_3.tmp": 1}
	for n, c := range want {
		if names[n] != c {
			t.Errorf("%s: %d files, want %d (got %v)", n, names[n], c, names)
		}
	}
}
