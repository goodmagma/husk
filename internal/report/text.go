package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/scanner"
)

// DefaultStatuses are the statuses listed by default in the text report.
var DefaultStatuses = []model.Status{model.Orphan, model.Disposable, model.Suspect, model.Portable, model.Shared}

// DefaultDir is the default parent folder for report files: the system temporary folder.
// Each scan writes its files in a husk_<date>_<time> subfolder.
func DefaultDir() string {
	return os.TempDir()
}

// OldDefaultDir is the default folder used before version 0.1.0 was released;
// the GUI replaces a saved value equal to it with DefaultDir.
func OldDefaultDir() string {
	return filepath.Join(os.TempDir(), "husk")
}

// ParseStatuses reads a comma-separated list of statuses; "all" selects every status.
func ParseStatuses(list string) ([]model.Status, error) {
	if strings.TrimSpace(list) == "all" {
		return model.StatusOrder, nil
	}
	valid := map[model.Status]bool{}
	for _, s := range model.StatusOrder {
		valid[s] = true
	}
	var out []model.Status
	for _, item := range strings.Split(list, ",") {
		s := model.Status(strings.TrimSpace(strings.ToLower(item)))
		if s == "" {
			continue
		}
		if !valid[s] {
			return nil, fmt.Errorf("unknown status %q (valid: %s, all)", s, joinStatuses(model.StatusOrder))
		}
		out = append(out, s)
	}
	return out, nil
}

func joinStatuses(list []model.Status) string {
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// TextOptions select what WriteText prints.
type TextOptions struct {
	Statuses []model.Status // statuses to list folder by folder
	Verbose  bool           // print match and notes under each folder
}

// WriteText prints the scan as plain text: sources, dictionary, folders by status, PATH issues.
// The command line and the GUI log use the same output.
func WriteText(w io.Writer, rep *scanner.Report, opt TextOptions) {
	fmt.Fprintln(w, "Installed programs:")
	for _, s := range rep.Sources {
		fmt.Fprintf(w, "  %-12s %5d items\n", s.Label, s.Count)
	}
	fmt.Fprintf(w, "Dictionary: %d entries; %d/%d programs detected, %d known folders found\n",
		len(rep.Dictionary.Entries), rep.Installed, rep.Apps, rep.KnownPaths)
	for _, l := range rep.Dictionary.Loaded {
		fmt.Fprintln(w, "  "+l)
	}
	for _, e := range rep.Dictionary.Errors {
		fmt.Fprintln(w, "  ! "+e)
	}

	listed := map[model.Status]bool{}
	for _, s := range opt.Statuses {
		listed[s] = true
	}
	for _, s := range model.StatusOrder {
		var items []model.Result
		var size int64
		for _, r := range rep.Results {
			if r.Status == s {
				items = append(items, r)
				size += r.Size
			}
		}
		if len(items) == 0 || (!listed[s] && s == model.Ignored) {
			continue
		}
		fmt.Fprintf(w, "\n%s: %s, %s\n", model.StatusTitle[s], countItems(items), FmtSize(size))
		if !listed[s] {
			continue
		}
		const row = "  %10s  %-10s  %-10s  %s\n"
		fmt.Fprintf(w, row, "SIZE", "MODIFIED", "SOURCE", "PATH")
		for _, r := range items {
			p := r.Path
			if r.Type == model.TypeFiles && r.Files > 1 {
				p += " (" + fileCount(r.Files) + ")"
			}
			fmt.Fprintf(w, row, FmtSize(r.Size), FmtDate(r.LastWrite), r.Source, p)
			if opt.Verbose {
				for _, line := range []string{r.Match, r.Details} {
					if line != "" {
						fmt.Fprintf(w, "%38s%s\n", "", line)
					}
				}
			}
		}
	}

	fmt.Fprintf(w, "\nPATH entries to review: %d\n", len(rep.PathIssues))
	for _, i := range rep.PathIssues {
		fmt.Fprintf(w, "  [%s] %s: %s\n", i.Scope, i.Problem, i.Entry)
		if opt.Verbose && i.Details != "" {
			fmt.Fprintf(w, "      %s\n", i.Details)
		}
	}

	fmt.Fprintf(w, "\nScanned %s in %.1fs\n", countItems(rep.Results), rep.Duration.Seconds())
}

// countItems describes a list of results, e.g. "12 folders", "3 file groups", "5 folders, 1 file group".
func countItems(items []model.Result) string {
	folders, groups := 0, 0
	for _, r := range items {
		if r.Type == model.TypeFiles {
			groups++
		} else {
			folders++
		}
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	var parts []string
	if folders > 0 || groups == 0 {
		parts = append(parts, plural(folders, "folder", "folders"))
	}
	if groups > 0 {
		parts = append(parts, plural(groups, "file group", "file groups"))
	}
	return strings.Join(parts, ", ")
}

// WriteFileList prints the paths of the report files.
func WriteFileList(w io.Writer, f Files) {
	fmt.Fprintf(w, "\nFolder:      %s\n", f.Dir)
	fmt.Fprintf(w, "Report:      %s\n", f.HTML)
	fmt.Fprintf(w, "CSV:         %s\n", f.CSV)
	fmt.Fprintf(w, "Programs:    %s\n", f.Programs)
	fmt.Fprintf(w, "PATH:        %s\n", f.Path)
	if f.SuggestN > 0 {
		fmt.Fprintf(w, "Suggestions: %s (%d entries)\n", f.Suggestions, f.SuggestN)
	}
}
