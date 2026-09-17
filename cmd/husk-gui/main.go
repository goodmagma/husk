// Command husk-gui is the graphical interface of Husk. It runs the scan and opens the HTML report
// in the default browser.
package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
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
	hversion "github.com/goodmagma/husk/internal/version"
)

// version is the program version (see internal/version).
var version = hversion.Version

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
	u.minSize.SetSelected(labels[0])
	if saved := prefs.String("minSize"); slices.Contains(labels, saved) {
		u.minSize.SetSelected(saved)
	}

	u.outDir = widget.NewEntry()
	u.outDir.SetText(prefs.StringWithFallback("reportDir", report.DefaultDir()))
	browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), u.chooseOutDir)

	u.all = widget.NewCheck("Include system folders", nil)
	u.all.SetChecked(prefs.BoolWithFallback("all", false))
	u.autoOpn = widget.NewCheck("Open the report when done", nil)
	u.autoOpn.SetChecked(prefs.BoolWithFallback("autoOpen", true))

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
	u.log.TextStyle = fyne.TextStyle{Monospace: true}
	u.log.Selectable = true // select with the mouse, copy with Ctrl+C or the context menu

	// options in three compact rows
	daysBox := container.NewGridWrap(fyne.NewSize(90, u.days.MinSize().Height), u.days)
	limits := container.NewHBox(
		widget.NewLabel("Orphan after"), daysBox, widget.NewLabel("days without changes"),
		layout.NewSpacer(),
		widget.NewLabel("Minimum size (heuristic)"), u.minSize,
	)
	folder := container.NewBorder(nil, nil, widget.NewLabel("Report folder"), browse, u.outDir)
	run := container.NewHBox(u.all, u.autoOpn, layout.NewSpacer(), u.cancelBtn, u.scanBtn)
	status := container.NewBorder(nil, nil, nil, widget.NewLabel("Husk "+version), u.phase)

	top := container.NewVBox(
		limits, folder, run,
		widget.NewSeparator(),
		status, u.progress,
		u.summary, u.actions,
	)
	return container.NewBorder(top, nil, nil, nil, container.NewScroll(u.log))
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
	prefs.SetString("reportDir", u.outDir.Text)
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
	u.actions.Add(widget.NewButtonWithIcon("Copy log", theme.ContentCopyIcon(), func() {
		u.app.Clipboard().SetContent(u.log.Text)
		u.phase.SetText("Log copied to the clipboard.")
	}))
	if u.files.SuggestN > 0 {
		u.actions.Add(widget.NewButtonWithIcon(fmt.Sprintf("Suggestions (%d)", u.files.SuggestN), theme.FileIcon(), func() {
			u.open(u.files.Suggestions)
		}))
	}

	var b strings.Builder
	report.WriteText(&b, rep, report.TextOptions{Statuses: report.DefaultStatuses})
	report.WriteFileList(&b, u.files)
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
