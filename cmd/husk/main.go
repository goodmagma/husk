// Command husk finds the leftover folders of uninstalled programs (read-only).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/goodmagma/husk/internal/model"
	"github.com/goodmagma/husk/internal/platform"
	"github.com/goodmagma/husk/internal/report"
	"github.com/goodmagma/husk/internal/scanner"
)

// version is set at build time: -ldflags "-X main.version=1.0.0".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	opt := scanner.DefaultOptions()
	fs := flag.NewFlagSet("husk", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Husk %s - report of folders left behind by uninstalled programs (read-only).\n\n", version)
		fmt.Fprintf(fs.Output(), "Usage: husk [options]\n\nOptions:\n")
		fs.PrintDefaults()
	}
	fs.IntVar(&opt.Days, "days", opt.Days, "days without changes before a folder is an orphan")
	minMB := fs.Int64("min-mb", 0, "skip folders found by the heuristic below N MB (0: all)")
	out := fs.String("out", ".", "output folder")
	fs.BoolVar(&opt.All, "all", false, "include system folders")
	noOpen := fs.Bool("no-open", false, "do not open the report when done")
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
	opt.MinSize = *minMB * 1024 * 1024

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	interactive := isTerminal()
	lastPhase := ""
	rep, err := scanner.Run(ctx, opt, func(phase string, done, total int) {
		if phase != lastPhase {
			if lastPhase != "" && interactive {
				fmt.Println()
			}
			lastPhase = phase
			if total == 0 || !interactive {
				fmt.Println(phase + "...")
			}
		}
		if total > 0 && interactive {
			fmt.Printf("\r%s: %d/%d", phase, done, total)
		}
	})
	if interactive {
		fmt.Println()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Scan interrupted:", err)
		return 1
	}

	for _, s := range rep.Sources {
		fmt.Printf("  %-12s %5d items\n", s.Label, s.Count)
	}
	fmt.Printf("Dictionary: %d entries; %d/%d programs detected, %d known folders found\n",
		len(rep.Dictionary.Entries), rep.Installed, rep.Apps, rep.KnownPaths)
	for _, l := range rep.Dictionary.Loaded {
		fmt.Println("  " + l)
	}
	for _, e := range rep.Dictionary.Errors {
		fmt.Fprintln(os.Stderr, "  ! "+e)
	}

	printSummary(rep)

	dir, _ := filepath.Abs(*out)
	files, err := report.Write(rep, dir, version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write the report:", err)
		return 1
	}
	fmt.Printf("\nReport:      %s\n", files.HTML)
	fmt.Printf("Programs:    %s\n", files.Programs)
	fmt.Printf("PATH:        %s\n", files.Path)
	if files.SuggestN > 0 {
		fmt.Printf("Suggestions: %s (%d entries)\n", files.Suggestions, files.SuggestN)
	}
	fmt.Printf("Time:        %.1fs\n", rep.Duration.Seconds())

	if !*noOpen {
		if err := platform.OpenURL(files.HTML); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot open the report:", err)
		}
	}
	return 0
}

func printSummary(rep *scanner.Report) {
	var orphans []model.Result
	var orphanSize, sharedSize int64
	shared := 0
	for _, r := range rep.Results {
		switch r.Status {
		case model.Orphan:
			orphans = append(orphans, r)
			orphanSize += r.Size
		case model.Shared:
			shared++
			sharedSize += r.Size
		}
	}
	fmt.Printf("\nLikely orphans: %d (%s)\n", len(orphans), report.FmtSize(orphanSize))
	for i, r := range orphans {
		if i == 15 {
			fmt.Printf("  ... and %d more in the report\n", len(orphans)-15)
			break
		}
		fmt.Printf("  %10s  %s  [%.4s]  %s\n", report.FmtSize(r.Size), report.FmtDate(r.LastWrite), r.Source, r.Path)
	}
	if shared > 0 {
		fmt.Printf("Shared caches: %d (%s)\n", shared, report.FmtSize(sharedSize))
	}
	if len(rep.PathIssues) > 0 {
		fmt.Printf("PATH entries to review: %d\n", len(rep.PathIssues))
		for _, i := range rep.PathIssues {
			fmt.Printf("  [%s] %s: %s\n", i.Scope, i.Problem, i.Entry)
		}
	}
}

func isTerminal() bool {
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
