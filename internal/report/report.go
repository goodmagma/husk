// Package report writes the results of a scan: HTML, CSV and dictionary suggestions.
package report

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/pathutil"
	"github.com/goodmagma/husk/internal/scanner"
)

//go:embed template.html
var htmlTemplate string

// Files lists the files written by Write.
type Files struct {
	Dir         string // folder of this scan
	HTML        string
	CSV         string
	Programs    string
	Path        string
	Suggestions string // empty when there are no suggestions
	SuggestN    int
}

// Write saves all report files in a new folder husk_<date>_<time> inside parent.
func Write(rep *scanner.Report, parent, version string) (Files, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Files{}, err
	}
	dir, err := newRunDir(parent, "husk_"+rep.Started.Format("20060102_150405"))
	if err != nil {
		return Files{}, err
	}
	f := Files{
		Dir:         dir,
		HTML:        filepath.Join(dir, "husk_report.html"),
		CSV:         filepath.Join(dir, "husk_report.csv"),
		Programs:    filepath.Join(dir, "husk_programs.csv"),
		Path:        filepath.Join(dir, "husk_path.csv"),
		Suggestions: filepath.Join(dir, "husk_suggestions.toml"),
	}
	if err := os.WriteFile(f.HTML, []byte(HTML(rep, version)), 0o644); err != nil {
		return f, err
	}
	if err := writeCSV(f.CSV, resultRows(rep.Results)); err != nil {
		return f, err
	}
	if err := writeCSV(f.Programs, programRows(rep)); err != nil {
		return f, err
	}
	if err := writeCSV(f.Path, pathRows(rep.PathIssues)); err != nil {
		return f, err
	}
	text, n := Suggestions(rep.Results)
	f.SuggestN = n
	if n == 0 {
		f.Suggestions = ""
	} else if err := os.WriteFile(f.Suggestions, []byte(text), 0o644); err != nil {
		return f, err
	}
	return f, nil
}

// newRunDir creates parent/name, or parent/name_2, name_3, ... if it already exists.
func newRunDir(parent, name string) (string, error) {
	for i := 1; ; i++ {
		dir := filepath.Join(parent, name)
		if i > 1 {
			dir = fmt.Sprintf("%s_%d", dir, i)
		}
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) || i >= 100 {
			return "", err
		}
	}
}

// FmtSize formats a size in bytes (1.5 MB).
func FmtSize(b int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(b)
	for i, u := range units {
		if v < 1024 || i == len(units)-1 {
			if i == 0 {
				return fmt.Sprintf("%d B", b)
			}
			return fmt.Sprintf("%.1f %s", v, u)
		}
		v /= 1024
	}
	return ""
}

// FmtDate formats a Unix time as yyyy-mm-dd.
func FmtDate(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("2006-01-02")
}

// FmtInt formats an integer with a comma as thousands separator.
func FmtInt(n int64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// Subtitle describes the scan in one line.
func Subtitle(rep *scanner.Report) string {
	s := fmt.Sprintf("Generated %s · %d pieces of evidence of installed programs · dictionary: %d entries, %d programs detected · "+
		"orphan (heuristic) = no match and no changes for %d days",
		rep.Started.Format("2006-01-02 15:04"), len(rep.Evidence), len(rep.Dictionary.Entries),
		rep.Installed, rep.Options.Days)
	if rep.Options.MinSize > 0 {
		s += " · heuristic above " + FmtSize(rep.Options.MinSize)
	}
	return s
}

// HTML returns the report as a self-contained page.
func HTML(rep *scanner.Report, version string) string {
	e := html.EscapeString
	var cards strings.Builder
	for _, s := range model.StatusOrder {
		if s == model.Ignored && !rep.Options.All {
			continue
		}
		var size int64
		n := 0
		for _, r := range rep.Results {
			if r.Status == s {
				size += r.Size
				n++
			}
		}
		fmt.Fprintf(&cards, `<div class="card %s"><div class="n">%s</div><div>%s: %d</div></div>`,
			s, FmtSize(size), e(model.StatusLabel[s]), n)
	}

	var rows strings.Builder
	for _, r := range rep.Results {
		kind := model.KindLabel[r.Kind]
		if kind == "" {
			kind = r.Kind
		}
		kindHTML := e(kind)
		if r.Kind == "data" {
			kindHTML = `<span class="warn">` + kindHTML + `</span>`
		}
		details := e(r.Match)
		if r.Details != "" {
			details += `<div class="small">` + e(r.Details) + `</div>`
		}
		fmt.Fprintf(&rows, `<tr data-s="%s" data-b="%d"><td><span class="tag %s">%s</span></td>`+
			`<td class="src-%s">%s</td><td>%s</td>`+
			`<td class="path"><a href="%s">%s</a><div class="small">%s</div></td>`+
			`<td class="num" data-v="%d">%s</td><td class="num" data-v="%d">%s</td>`+
			`<td data-v="%d">%s</td><td>%s</td></tr>`+"\n",
			r.Status, r.Size, r.Status, e(model.StatusLabel[r.Status]),
			r.Source, e(r.Source), kindHTML,
			e(FileURL(r.Path)), e(r.Path), e(r.Area),
			r.Size, FmtSize(r.Size), r.Files, FmtInt(r.Files),
			r.LastWrite, FmtDate(r.LastWrite), details)
	}

	return strings.NewReplacer(
		"__VERSION__", e(version),
		"__SUBTITLE__", e(Subtitle(rep)),
		"__CARDS__", cards.String(),
		"__ROWS__", rows.String(),
		"__PATHS__", pathSection(rep.PathIssues),
	).Replace(htmlTemplate)
}

func pathSection(issues []model.PathIssue) string {
	e := html.EscapeString
	head := `<h2>PATH entries to review</h2><div class="sub">Entries of the PATH variable that point to missing folders, ` +
		`folders without executables or orphan folders, and duplicated entries.</div>`
	if len(issues) == 0 {
		return head + `<p>No problems found.</p>`
	}
	var rows strings.Builder
	for _, i := range issues {
		expanded := ""
		if i.Expanded != i.Entry {
			expanded = `<div class="small">` + e(i.Expanded) + `</div>`
		}
		fmt.Fprintf(&rows, `<tr><td>PATH %s</td><td><span class="tag orphan">%s</span></td>`+
			`<td class="path">%s%s</td><td class="small">%s</td></tr>`+"\n",
			e(i.Scope), e(i.Problem), e(i.Entry), expanded, e(i.Details))
	}
	return head + `<div class="wrap"><table id="paths"><thead><tr><th>Variable</th><th>Problem</th>` +
		`<th>Entry</th><th>Details</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div>`
}

// FileURL converts a local path into a file:// URL.
func FileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func writeCSV(file string, rows [][]string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil { // BOM: lets Excel detect UTF-8
		return err
	}
	w := csv.NewWriter(f)
	w.Comma = ','
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	return f.Close()
}

func resultRows(results []model.Result) [][]string {
	rows := [][]string{{"Status", "Source", "Content", "Area", "Path", "Bytes", "Size", "Files",
		"LastModified", "Match", "Details"}}
	for _, r := range results {
		kind := model.KindLabel[r.Kind]
		if kind == "" {
			kind = r.Kind
		}
		last := ""
		if r.LastWrite != 0 {
			last = time.Unix(r.LastWrite, 0).Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{model.StatusLabel[r.Status], r.Source, kind, r.Area, r.Path,
			fmt.Sprint(r.Size), FmtSize(r.Size), fmt.Sprint(r.Files), last, r.Match, r.Details})
	}
	return rows
}

func programRows(rep *scanner.Report) [][]string {
	rows := [][]string{{"Name", "Source", "Publisher", "Location"}}
	ev := append([]model.Evidence(nil), rep.Evidence...)
	sort.SliceStable(ev, func(i, j int) bool {
		if ev[i].Source != ev[j].Source {
			return ev[i].Source < ev[j].Source
		}
		return strings.ToLower(ev[i].Name) < strings.ToLower(ev[j].Name)
	})
	for _, e := range ev {
		rows = append(rows, []string{e.Name, e.Source, e.Publisher, e.Location})
	}
	rows = append(rows, []string{}, []string{"Dictionary entry", "Origin", "Installed", "Detected by"})
	entries := rep.Dictionary.List()
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	for _, e := range entries {
		if e.Shared {
			continue
		}
		inst := "no"
		if e.Installed {
			inst = "yes"
		}
		rows = append(rows, []string{e.Name, e.Origin, inst, e.DetectedBy})
	}
	return rows
}

func pathRows(issues []model.PathIssue) [][]string {
	rows := [][]string{{"Variable", "Entry", "Path", "Problem", "Details"}}
	for _, i := range issues {
		rows = append(rows, []string{"PATH " + i.Scope, i.Entry, i.Expanded, i.Problem, i.Details})
	}
	return rows
}

// Suggestions returns entries proposed for apps.user.toml (folders found only by the heuristic).
func Suggestions(results []model.Result) (string, int) {
	var b strings.Builder
	b.WriteString("# Folders found only by the heuristic: candidates for apps.user.toml.\n")
	b.WriteString("# Check which program they belong to, fill in the fields and uncomment.\n\n")
	n := 0
	for _, r := range results {
		if r.Source != model.SourceHeuristic ||
			(r.Status != model.Orphan && r.Status != model.Suspect && r.Status != model.Portable) {
			continue
		}
		n++
		name := strings.TrimLeft(filepath.Base(r.Path), ".")
		fmt.Fprintf(&b, "# %s - last modified %s - %s\n", FmtSize(r.Size), FmtDate(r.LastWrite), model.StatusLabel[r.Status])
		b.WriteString("# [[app]]\n")
		fmt.Fprintf(&b, "# name = %q\n", name)
		b.WriteString("# category = \"\"\n")
		fmt.Fprintf(&b, "# detect.names = [%q]\n", name+"*")
		fmt.Fprintf(&b, "# detect.exe = [%q]\n", strings.ToLower(name))
		fmt.Fprintf(&b, "# paths = [ { path = '%s', kind = \"cache\" } ]\n\n", pathutil.Unexpand(r.Path))
	}
	return b.String(), n
}
