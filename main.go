// Command inventur finds large files, large folders and known clutter
// (node_modules, venvs, ML checkpoints, caches, logs, …) below a directory.
// It is strictly read-only: it reports and reveals in Finder, it never
// deletes or modifies anything.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"inventur/internal/format"
	"inventur/internal/report"
	"inventur/internal/scan"
	"inventur/internal/tui"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var (
		minFile     = flag.String("min-file", "50MB", "files at least this big are listed as large files")
		listMin     = flag.String("list-min", "1MB", "files at least this big are listed individually in folders")
		minDir      = flag.String("min-dir", "1GB", "smallest folder shown in the report's directory tree")
		stale       = flag.String("stale", "180d", "highlight items not modified for this long (e.g. 90d, 2y)")
		top         = flag.Int("top", 25, "number of large files in the text report")
		perCategory = flag.Int("per-category", 10, "items per category in the text report")
		depth       = flag.Int("depth", 8, "depth of the report's directory tree")
		workers     = flag.Int("workers", 0, "concurrent directory readers (default 4×CPUs)")
		crossMounts = flag.Bool("cross-mounts", false, "descend into other mounted filesystems")
		jsonOut     = flag.Bool("json", false, "print a JSON report instead of starting the TUI")
		textOut     = flag.Bool("report", false, "print a plain-text report instead of starting the TUI")
		noColor     = flag.Bool("no-color", false, "disable colors in the text report")
		excludes    multiFlag
	)
	flag.Var(&excludes, "exclude", "skip paths matching this glob or path prefix (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: inventur [flags] [dir]\n\n"+
			"Finds large files, folders and clutter below dir (default: your home folder).\n"+
			"Read-only: nothing is ever modified or deleted.\n\nFlags:\n")
		flag.PrintDefaults()
	}

	// Allow flags after the directory argument too.
	var positional []string
	for args := os.Args[1:]; ; {
		flag.CommandLine.Parse(args)
		if flag.NArg() == 0 {
			break
		}
		positional = append(positional, flag.Arg(0))
		args = flag.Args()[1:]
	}
	if len(positional) > 1 {
		fail("expected at most one directory, got %d", len(positional))
	}

	home, _ := os.UserHomeDir()
	root := home
	if len(positional) == 1 {
		root = positional[0]
	}
	opts := scan.Options{
		Root:        root,
		Home:        home,
		MinFile:     mustSize("min-file", *minFile),
		ListMin:     mustSize("list-min", *listMin),
		Workers:     *workers,
		CrossMounts: *crossMounts,
		Excludes:    excludes,
	}
	staleAge, err := parseAge(*stale)
	if err != nil {
		fail("--stale: %v", err)
	}

	if !*jsonOut && !*textOut && isTTY(os.Stdout) && isTTY(os.Stdin) {
		m := tui.New(tui.Config{Scan: opts, Stale: staleAge})
		if _, err := tea.NewProgram(m).Run(); err != nil {
			fail("%v", err)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sc := scan.New(opts)
	done := showProgress(sc)
	res, err := sc.Run(ctx)
	done()
	if err != nil {
		fail("%v", err)
	}
	cfg := report.Config{
		Top:         *top,
		PerCategory: *perCategory,
		MinDir:      mustSize("min-dir", *minDir),
		Depth:       *depth,
		Stale:       staleAge,
		Color:       !*noColor,
		Home:        home,
		Now:         time.Now(),
	}
	if *jsonOut {
		err = report.JSON(os.Stdout, res, cfg)
	} else {
		err = report.Text(os.Stdout, res, cfg)
	}
	if err != nil {
		fail("%v", err)
	}
}

// showProgress prints a status line to stderr while scanning (if it is a
// terminal) and returns a function that stops and clears it.
func showProgress(sc *scan.Scanner) func() {
	if !isTTY(os.Stderr) {
		return func() {}
	}
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				fmt.Fprint(os.Stderr, "\r\x1b[K")
				return
			case <-t.C:
				p := sc.Progress()
				fmt.Fprintf(os.Stderr, "\r\x1b[KScanning… %s files · %s folders · %s",
					format.Count(p.Files), format.Count(p.Dirs), format.Size(p.Bytes))
			}
		}
	}()
	return func() { close(stop); <-finished }
}

// parseAge accepts "180d", "8w", "2y" or a Go duration like "72h".
func parseAge(s string) (time.Duration, error) {
	units := map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour, "y": 365 * 24 * time.Hour}
	for suffix, unit := range units {
		if n, ok := strings.CutSuffix(s, suffix); ok {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid age %q", s)
			}
			return time.Duration(v * float64(unit)), nil
		}
	}
	return time.ParseDuration(s)
}

func mustSize(name, v string) int64 {
	n, err := format.ParseSize(v)
	if err != nil {
		fail("--%s: %v", name, err)
	}
	return n
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "inventur: "+f+"\n", a...)
	os.Exit(1)
}
