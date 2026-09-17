package scanner

import (
	"os"
	"strings"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
	"github.com/goodmagma/husk/internal/platform"
)

// CheckPath reports PATH entries that are duplicated, missing, without executables
// or inside an orphan folder. installed reports whether a dictionary entry is installed
// (known is false if the dictionary has no such entry): missing default folders of
// installed programs, such as %USERPROFILE%\go\bin, are not reported.
func CheckPath(vars []platform.PathVar, results []model.Result,
	installed func(app string) (ok, known bool)) []model.PathIssue {
	orphans := map[string]model.Result{}
	for _, r := range results {
		if r.Status == model.Orphan {
			orphans[pathutil.Key(r.Path)] = r
		}
	}
	seen := map[string]string{}
	var issues []model.PathIssue
	for _, v := range vars {
		entry := strings.Trim(strings.TrimSpace(v.Entry), `"`)
		if entry == "" {
			continue
		}
		full := pathutil.Expand(entry)
		k := pathutil.Key(full)
		issue := model.PathIssue{Scope: v.Scope, Entry: entry, Expanded: full}
		st, err := os.Stat(full)
		switch {
		case seen[k] != "":
			issue.Problem, issue.Details = "duplicate", "already in the "+seen[k]+" PATH"
		case err != nil || !st.IsDir():
			issue.Problem = "missing folder"
			if def, ok := platform.MatchDefaultPathEntry(full); ok {
				inst, known := false, false
				if def.App != "" {
					inst, known = installed(def.App)
				}
				switch {
				case def.App == "" || inst:
					issue.Problem = "" // expected: created by the first `install`
				case known:
					issue.Details = "added by " + def.App + ", which is not installed"
				default:
					issue.Details = "default folder of " + def.App + " (created by " + def.Install + ")"
				}
			}
		case !platform.HasPrograms(full):
			issue.Problem = "no executables"
			if entries, _ := os.ReadDir(full); len(entries) == 0 {
				issue.Details = "empty folder"
			}
		default:
			for parent := k; parent != ""; parent = pathutil.Parent(parent) {
				if r, ok := orphans[parent]; ok {
					issue.Problem = "inside an orphan folder"
					why := r.Match
					if why == "" {
						why = "likely orphan"
					}
					issue.Details = r.Path + ": " + why
					break
				}
			}
		}
		if seen[k] == "" {
			seen[k] = v.Scope
		}
		if issue.Problem != "" {
			issues = append(issues, issue)
		}
	}
	return issues
}
