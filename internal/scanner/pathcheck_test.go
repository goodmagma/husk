package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/goodmagma/husk/dictionary"
	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/platform"
)

func TestCheckPath(t *testing.T) {
	tmp := t.TempDir()
	mkdir := func(parts ...string) string {
		p := filepath.Join(append([]string{tmp}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exe := "tool"
	if runtime.GOOS == "windows" {
		exe = "tool.exe"
	}
	addTool := func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, exe), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	withTool := mkdir("tools")
	addTool(withTool)
	empty := mkdir("empty")
	orphan := mkdir("Leftover")
	orphanBin := mkdir("Leftover", "bin")
	addTool(orphanBin)
	emptyGoBin := mkdir("go", "pkg")

	// dictionary with an installed program, a missing one and a shared folder
	d := dictionary.New()
	d.Load("test.toml", []byte(`
[[app]]
name = "Go"
detect.exe = ["go"]
paths = [ '`+filepath.Join(tmp, "go")+`' ]

[[app]]
name = ".NET SDK"
detect.exe = ["dotnet"]
paths = [ '`+filepath.Join(tmp, ".dot*")+`' ]

[[shared]]
name = "User executables"
path = '`+filepath.Join(tmp, ".local", "bin")+`'
`), "test")
	if len(d.Errors) > 0 {
		t.Fatal(d.Errors)
	}
	d.Detect(nil, map[string]bool{"go": true})
	owner := func(p string) (Owner, bool) {
		e, ok := d.Owner(p)
		if !ok {
			return Owner{}, false
		}
		return Owner{Name: e.Name, Shared: e.Shared, Installed: e.Installed}, true
	}

	vars := []platform.PathVar{
		{Scope: "user", Entry: withTool},
		{Scope: "system", Entry: withTool},                             // duplicate
		{Scope: "user", Entry: empty},                                  // no executables, no owner
		{Scope: "user", Entry: filepath.Join(tmp, "missing")},          // missing, no owner
		{Scope: "user", Entry: filepath.Join(tmp, "go", "bin")},        // missing, Go installed
		{Scope: "user", Entry: emptyGoBin},                             // empty, Go installed
		{Scope: "user", Entry: filepath.Join(tmp, ".dotnet", "tools")}, // missing, .NET not installed
		{Scope: "user", Entry: filepath.Join(tmp, ".local", "bin")},    // missing, shared folder
		{Scope: "user", Entry: orphanBin},                              // inside an orphan folder
		{Scope: "user", Entry: "  "},                                   // blank
	}
	results := []model.Result{{Path: orphan, Status: model.Orphan, Match: "Leftover: not detected as installed"}}

	got := map[string]model.PathIssue{}
	for _, i := range CheckPath(vars, results, owner) {
		got[i.Scope+" "+filepath.Base(filepath.Dir(i.Expanded))+"/"+filepath.Base(i.Expanded)] = i
	}
	base := filepath.Base(tmp)
	want := map[string]string{
		"system " + base + "/tools": "duplicate",
		"user " + base + "/empty":   "no executables",
		"user " + base + "/missing": "missing folder",
		"user .dotnet/tools":        "missing folder",
		"user Leftover/bin":         "inside an orphan folder",
	}
	for k, problem := range want {
		if got[k].Problem != problem {
			t.Errorf("%s: problem %q, want %q", k, got[k].Problem, problem)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d issues, want %d: %v", len(got), len(want), got)
	}
	if d := got["user .dotnet/tools"].Details; d != "belongs to .NET SDK, which is not installed" {
		t.Errorf(".dotnet details: %q", d)
	}
}

func TestDictionaryOwner(t *testing.T) {
	d := dictionary.New()
	root := filepath.Join(t.TempDir(), "Home")
	d.Load("test.toml", []byte(`
[[app]]
name = "Parent"
paths = [ '`+root+`' ]

[[app]]
name = "Child"
paths = [ '`+filepath.Join(root, "Tool*")+`' ]
`), "test")
	cases := map[string]string{
		filepath.Join(root, "ToolBox", "bin"):  "Child",  // most specific pattern wins
		filepath.Join(root, "other"):           "Parent", // ancestor
		root:                                   "Parent", // the folder itself
		filepath.Join(filepath.Dir(root), "x"): "",       // outside
	}
	for p, want := range cases {
		e, ok := d.Owner(p)
		got := ""
		if ok {
			got = e.Name
		}
		if got != want {
			t.Errorf("Owner(%s) = %q, want %q", p, got, want)
		}
	}
}
