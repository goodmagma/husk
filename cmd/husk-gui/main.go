// Command husk-gui is the graphical interface of Husk. It runs the scan and opens the HTML report
// in the default browser.
package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/platform"
	"github.com/goodmagma/husk/internal/report"
	"github.com/goodmagma/husk/internal/scanner"
)

// version is set at build time: -ldflags "-X main.version=1.0.0".
var version = "dev"

const appID = "io.github.goodmagma.husk"

var minSizes = []struct {
	label string
	bytes int64
}{
	{"all", 0},
	{"1 MB", 1 << 20},
	{"10 MB", 10 << 20},
	{"100 MB", 100 << 20},
}

type ui struct {
	app fyne.App
	win fyne.Window

	days    *widget.Entry
	minSize *widget.Select
	outDir  *widget.Entry
	all     *widget.Check
	autoOpn *widget.Check

	scanBtn   *widget.Button
	cancelBtn *widget.Button
	phase     *widget.Label
	progress  *widget.ProgressBar

	summary *fyne.Container
	log     *widget.Label
	actions *fyne.Container

	cancel context.CancelFunc
	files  report.Files
}

func main() {
	a := app.NewWithID(appID)
	w := a.NewWindow("Husk – Leftover folders")
	u := &ui{app: a, win: w}
	w.SetContent(u.build())
	w.Resize(fyne.NewSize(860, 640))
	w.ShowAndRun()
}

func (u *ui) build() fyne.CanvasObject {
	prefs := u.app.Preferences()

	title := widget.NewLabelWithStyle("Husk", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	intro := widget.NewLabel("Finds the folders left behind by uninstalled programs. " +
		"Nothing is deleted: you get a report to review.")
	intro.Wrapping = fyne.TextWrapWord

	u.days = widget.NewEntry()
	u.days.SetText(strconv.Itoa(prefs.IntWithFallback("days", 90)))
	u.days.Validator = func(s string) error {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 {
			return errors.New("enter a number of days greater than zero")
		}
		return nil
	}

	var labels []string
	for _, m := range minSizes {
		labels = append(labels, m.label)
	}
	u.minSize = widget.NewSelect(labels, nil)
	u.minSize.SetSelected(prefs.StringWithFallback("minSize", "all"))

	u.outDir = widget.NewEntry()
	u.outDir.SetText(prefs.StringWithFallback("outDir", defaultOutDir()))
	browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), u.chooseOutDir)

	u.all = widget.NewCheck("Include system folders", nil)
	u.all.SetChecked(prefs.BoolWithFallback("all", false))
	u.autoOpn = widget.NewCheck("Open the report when done", nil)
	u.autoOpn.SetChecked(prefs.BoolWithFallback("autoOpen", true))

	form := widget.NewForm(
		widget.NewFormItem("Orphan after (days without changes)", u.days),
		widget.NewFormItem("Minimum size (folders found by heuristic)", u.minSize),
		widget.NewFormItem("Report folder", container.NewBorder(nil, nil, nil, browse, u.outDir)),
		widget.NewFormItem("", container.NewHBox(u.all, u.autoOpn)),
	)

	u.scanBtn = widget.NewButtonWithIcon("Scan", theme.SearchIcon(), u.startScan)
	u.scanBtn.Importance = widget.HighImportance
	u.cancelBtn = widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		if u.cancel != nil {
			u.cancel()
		}
	})
	u.cancelBtn.Hide()

	u.phase = widget.NewLabel("Ready.")
	u.progress = widget.NewProgressBar()
	u.progress.Hide()

	u.summary = container.NewGridWithColumns(3)
	u.actions = container.NewHBox()
	u.log = widget.NewLabel("")
	u.log.Wrapping = fyne.TextWrapWord
	u.log.TextStyle = fyne.TextStyle{Monospace: true}

	top := container.NewVBox(
		title, intro,
		widget.NewCard("", "Options", form),
		container.NewHBox(u.scanBtn, u.cancelBtn, layout.NewSpacer(), widget.NewLabel("Husk "+version)),
		u.phase, u.progress,
		u.summary, u.actions,
	)
	return container.NewBorder(top, nil, nil, nil, container.NewVScroll(u.log))
}

func defaultOutDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if st, err := os.Stat(filepath.Join(home, "Documents")); err == nil && st.IsDir() {
		return filepath.Join(home, "Documents", "Husk")
	}
	return filepath.Join(home, "Husk")
}

func (u *ui) chooseOutDir() {
	d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}
		u.outDir.SetText(uri.Path())
	}, u.win)
	d.Show()
}

func (u *ui) options() (scanner.Options, error) {
	opt := scanner.DefaultOptions()
	if err := u.days.Validate(); err != nil {
		return opt, err
	}
	opt.Days, _ = strconv.Atoi(strings.TrimSpace(u.days.Text))
	for _, m := range minSizes {
		if m.label == u.minSize.Selected {
			opt.MinSize = m.bytes
		}
	}
	opt.All = u.all.Checked
	if strings.TrimSpace(u.outDir.Text) == "" {
		return opt, errors.New("choose a report folder")
	}

	prefs := u.app.Preferences()
	prefs.SetInt("days", opt.Days)
	prefs.SetString("minSize", u.minSize.Selected)
	prefs.SetString("outDir", u.outDir.Text)
	prefs.SetBool("all", opt.All)
	prefs.SetBool("autoOpen", u.autoOpn.Checked)
	return opt, nil
}

func (u *ui) startScan() {
	opt, err := u.options()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	outDir := strings.TrimSpace(u.outDir.Text)
	autoOpen := u.autoOpn.Checked

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.scanBtn.Disable()
	u.cancelBtn.Show()
	u.progress.SetValue(0)
	u.progress.Show()
	u.summary.RemoveAll()
	u.actions.RemoveAll()
	u.log.SetText("")

	go func() {
		defer cancel()
		rep, err := scanner.Run(ctx, opt, func(phase string, done, total int) {
			fyne.Do(func() {
				if total > 0 {
					u.phase.SetText(fmt.Sprintf("%s: %d of %d", phase, done, total))
					u.progress.SetValue(float64(done) / float64(total))
				} else {
					u.phase.SetText(phase + "...")
				}
			})
		})
		var files report.Files
		if err == nil {
			files, err = report.Write(rep, outDir, version)
		}
		fyne.Do(func() {
			u.cancel = nil
			u.scanBtn.Enable()
			u.cancelBtn.Hide()
			u.progress.Hide()
			switch {
			case errors.Is(err, context.Canceled):
				u.phase.SetText("Scan cancelled.")
				return
			case err != nil:
				u.phase.SetText("Error.")
				dialog.ShowError(err, u.win)
				return
			}
			u.files = files
			u.showResults(rep)
			if autoOpen {
				u.open(files.HTML)
			}
		})
	}()
}

func (u *ui) showResults(rep *scanner.Report) {
	u.phase.SetText(fmt.Sprintf("Scan completed in %.1f s.", rep.Duration.Seconds()))

	for _, s := range []model.Status{model.Orphan, model.Suspect, model.Portable, model.Shared, model.Associated} {
		var size int64
		n := 0
		for _, r := range rep.Results {
			if r.Status == s {
				n++
				size += r.Size
			}
		}
		u.summary.Add(statusCard(s, n, size))
	}
	u.summary.Add(pathCard(len(rep.PathIssues)))

	openReport := widget.NewButtonWithIcon("Open report", theme.DocumentIcon(), func() { u.open(u.files.HTML) })
	openReport.Importance = widget.HighImportance
	u.actions.Add(openReport)
	u.actions.Add(widget.NewButtonWithIcon("Open report folder", theme.FolderOpenIcon(), func() {
		u.open(filepath.Dir(u.files.HTML))
	}))
	if u.files.SuggestN > 0 {
		u.actions.Add(widget.NewButtonWithIcon(fmt.Sprintf("Suggestions (%d)", u.files.SuggestN), theme.FileIcon(), func() {
			u.open(u.files.Suggestions)
		}))
	}

	var b strings.Builder
	b.WriteString("Installed programs:\n")
	for _, s := range rep.Sources {
		fmt.Fprintf(&b, "  %-12s %5d items\n", s.Label, s.Count)
	}
	fmt.Fprintf(&b, "Dictionary: %d entries; %d/%d programs detected, %d known folders found\n",
		len(rep.Dictionary.Entries), rep.Installed, rep.Apps, rep.KnownPaths)
	for _, l := range rep.Dictionary.Loaded {
		b.WriteString("  " + l + "\n")
	}
	for _, e := range rep.Dictionary.Errors {
		b.WriteString("  ! " + e + "\n")
	}
	if len(rep.PathIssues) > 0 {
		b.WriteString("\nPATH entries to review:\n")
		for _, i := range rep.PathIssues {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", i.Scope, i.Problem, i.Entry)
		}
	}
	b.WriteString("\nFiles:\n  " + u.files.HTML + "\n  " + u.files.CSV + "\n  " + u.files.Programs + "\n  " + u.files.Path + "\n")
	if u.files.Suggestions != "" {
		b.WriteString("  " + u.files.Suggestions + "\n")
	}
	u.log.SetText(b.String())
}

func (u *ui) open(target string) {
	if err := platform.OpenURL(target); err != nil {
		dialog.ShowError(err, u.win)
	}
}

func statusColor(s model.Status) color.Color {
	switch s {
	case model.Orphan:
		return theme.Color(theme.ColorNameError)
	case model.Suspect, model.Portable:
		return theme.Color(theme.ColorNameWarning)
	case model.Shared:
		return theme.Color(theme.ColorNamePrimary)
	case model.Associated:
		return theme.Color(theme.ColorNameSuccess)
	}
	return theme.Color(theme.ColorNameDisabled)
}

func card(c color.Color, value, label string) fyne.CanvasObject {
	bar := canvas.NewRectangle(c)
	bar.SetMinSize(fyne.NewSize(4, 0))
	v := widget.NewLabelWithStyle(value, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l := widget.NewLabel(label)
	l.Wrapping = fyne.TextWrapWord
	return container.NewBorder(nil, nil, bar, nil, container.NewVBox(v, l))
}

func statusCard(s model.Status, n int, size int64) fyne.CanvasObject {
	return card(statusColor(s), report.FmtSize(size), fmt.Sprintf("%s: %d", model.StatusLabel[s], n))
}

func pathCard(n int) fyne.CanvasObject {
	c := theme.Color(theme.ColorNameSuccess)
	if n > 0 {
		c = theme.Color(theme.ColorNameError)
	}
	return card(c, strconv.Itoa(n), "PATH entries to review")
}
