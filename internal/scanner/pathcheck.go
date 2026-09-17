package scanner

import (
	"os"
	"strings"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
	"github.com/goodmagma/husk/internal/platform"
)

// Owner describes the dictionary entry whose folders contain a path.
type Owner struct {
	Name      string
	Shared    bool
	Installed bool
}

// CheckPath reports PATH entries that are duplicated, missing, without executables
// or inside an orphan folder. owner finds the dictionary entry a path belongs to:
// a missing or empty entry inside the folders of an installed program or of a shared
// cache (e.g. %USERPROFILE%\go\bin with Go installed) is expected and not reported.
func CheckPath(vars []platform.PathVar, results []model.Result,
	owner func(path string) (Owner, bool)) []model.PathIssue {
	orphans := map[string]model.Result{}
	for _, r := range results {
		if r.Status == model.Orphan && r.Type != model.TypeFiles {
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
			explainOwner(&issue, full, owner)
		case !platform.HasPrograms(full):
			issue.Problem = "no executables"
			if entries, _ := os.ReadDir(full); len(entries) == 0 {
				issue.Details = "empty folder"
			}
			explainOwner(&issue, full, owner)
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

// explainOwner clears the issue when the folder belongs to an installed program or a shared
// cache (tools folders are created later, e.g. by "go install"), and names the program otherwise.
func explainOwner(issue *model.PathIssue, path string, owner func(string) (Owner, bool)) {
	o, ok := owner(path)
	switch {
	case !ok:
	case o.Shared || o.Installed:
		issue.Problem = ""
	default:
		issue.Details = joinDetails(issue.Details, "belongs to "+o.Name+", which is not installed")
	}
}

func joinDetails(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
