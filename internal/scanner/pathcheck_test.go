package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	withTool := mkdir("tools")
	if err := os.WriteFile(filepath.Join(withTool, exe), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := mkdir("empty")
	orphan := mkdir("Leftover")
	orphanBin := mkdir("Leftover", "bin")
	if err := os.WriteFile(filepath.Join(orphanBin, exe), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	installed := map[string]bool{"Go": true, ".NET SDK": false}
	lookup := func(app string) (bool, bool) {
		ok, known := installed[app]
		return ok, known
	}
	vars := []platform.PathVar{
		{Scope: "user", Entry: withTool},
		{Scope: "system", Entry: withTool},                             // duplicate
		{Scope: "user", Entry: empty},                                  // no executables
		{Scope: "user", Entry: filepath.Join(tmp, "missing")},          // missing
		{Scope: "user", Entry: filepath.Join(tmp, "go", "bin")},        // default, Go installed
		{Scope: "user", Entry: filepath.Join(tmp, ".dotnet", "tools")}, // default, .NET not installed
		{Scope: "user", Entry: filepath.Join(tmp, ".cargo", "bin")},    // default, Rust unknown
		{Scope: "user", Entry: orphanBin},                              // inside an orphan folder
		{Scope: "user", Entry: "  "},                                   // blank
	}
	results := []model.Result{{Path: orphan, Status: model.Orphan, Match: "Leftover: not detected as installed"}}

	got := map[string]model.PathIssue{}
	for _, i := range CheckPath(vars, results, lookup) {
		got[i.Scope+" "+filepath.Base(filepath.Dir(i.Expanded))+"/"+filepath.Base(i.Expanded)] = i
	}
	want := map[string]string{
		"system " + filepath.Base(tmp) + "/tools": "duplicate",
		"user " + filepath.Base(tmp) + "/empty":   "no executables",
		"user " + filepath.Base(tmp) + "/missing": "missing folder",
		"user .dotnet/tools":                      "missing folder",
		"user .cargo/bin":                         "missing folder",
		"user Leftover/bin":                       "inside an orphan folder",
	}
	for k, problem := range want {
		if got[k].Problem != problem {
			t.Errorf("%s: problem %q, want %q", k, got[k].Problem, problem)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d issues, want %d: %v", len(got), len(want), got)
	}
	if d := got["user .dotnet/tools"].Details; d != "added by .NET SDK, which is not installed" {
		t.Errorf(".dotnet details: %q", d)
	}
	if d := got["user .cargo/bin"].Details; d != "default folder of Rust (created by cargo install)" {
		t.Errorf(".cargo details: %q", d)
	}
}
