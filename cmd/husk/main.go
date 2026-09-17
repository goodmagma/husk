// Command husk finds the leftover folders of uninstalled programs (read-only).
//
// By default it prints the report to standard output; progress goes to standard error.
// With --report it also writes the HTML/CSV files and opens the HTML page.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/goodmagma/husk/internal/platform"
	"github.com/goodmagma/husk/internal/report"
	"github.com/goodmagma/husk/internal/scanner"
	hversion "github.com/goodmagma/husk/internal/version"
)

// version is the program version (see internal/version).
var version = hversion.Version

func main() {
	os.Exit(run())
}

func run() int {
	opt := scanner.DefaultOptions()
	fs := flag.NewFlagSet("husk", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Husk %s - lists the folders left behind by uninstalled programs (read-only).\n\n", version)
		fmt.Fprintf(fs.Output(), "Usage: husk [options]\n\nOptions:\n")
		fs.PrintDefaults()
	}
	fs.IntVar(&opt.Days, "days", opt.Days, "days without changes before a folder is an orphan")
	minMB := fs.Int64("min-mb", 0, "skip folders found by the heuristic below N MB (0: all)")
	fs.BoolVar(&opt.All, "all", false, "include system folders")
	show := fs.String("show", "orphan,suspect,portable,shared",
		"statuses to list folder by folder: orphan, suspect, portable, shared, associated, ignored, or all")
	verbose := fs.Bool("v", false, "print the match and the notes of each folder")
	writeReport := fs.Bool("report", false, "write the HTML/CSV report files and open the HTML page")
	out := fs.String("out", report.DefaultDir(), "parent folder of the husk_<date>_<time> report folder (with --report)")
	noOpen := fs.Bool("no-open", false, "with --report, do not open the HTML page")
	fs.IntVar(&opt.Workers, "workers", opt.Workers, "folders scanned in parallel")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Printf("husk %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	}
	statuses, err := report.ParseStatuses(*show)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	opt.MinSize = *minMB * 1024 * 1024

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	progress, clearProgress := progressPrinter()
	rep, err := scanner.Run(ctx, opt, progress)
	clearProgress()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Scan interrupted:", err)
		return 1
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	report.WriteText(w, rep, report.TextOptions{Statuses: statuses, Verbose: *verbose})
	if !*writeReport {
		return 0
	}

	dir, _ := filepath.Abs(*out)
	files, err := report.Write(rep, dir, version)
	if err != nil {
		w.Flush()
		fmt.Fprintln(os.Stderr, "Cannot write the report:", err)
		return 1
	}
	report.WriteFileList(w, files)
	if !*noOpen {
		if err := platform.OpenURL(files.HTML); err != nil {
			w.Flush()
			fmt.Fprintln(os.Stderr, "Cannot open the report:", err)
		}
	}
	return 0
}

// progressPrinter shows the scan phases on standard error: a single live line on a terminal,
// one line per phase otherwise. The returned function clears the live line.
func progressPrinter() (scanner.Progress, func()) {
	interactive := isTerminal(os.Stderr)
	lastPhase := ""
	clear := func() {
		if interactive {
			fmt.Fprintf(os.Stderr, "\r%*s\r", 60, "")
		}
	}
	progress := func(phase string, done, total int) {
		if !interactive {
			if phase != lastPhase {
				fmt.Fprintln(os.Stderr, phase+"...")
			}
			lastPhase = phase
			return
		}
		if phase != lastPhase {
			clear()
			lastPhase = phase
		}
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r%s: %d/%d", phase, done, total)
		} else {
			fmt.Fprintf(os.Stderr, "\r%s...", phase)
		}
	}
	return progress, clear
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
