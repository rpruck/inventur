// Package report renders a scan result as a plain-text report or JSON.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"inventur/internal/format"
	"inventur/internal/scan"
)

// Config controls what the reports include.
type Config struct {
	Top         int           // largest files to list
	PerCategory int           // instances to list per category
	MinDir      int64         // smallest directory shown in the tree
	Depth       int           // tree depth
	Stale       time.Duration // older than this is highlighted
	Color       bool
	Home        string
	Now         time.Time
}

var (
	hdr   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A78BFA"))
	dim   = lipgloss.NewStyle().Faint(true)
	warn  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	sizeS = lipgloss.NewStyle().Bold(true)
)

// Text writes the human-readable report.
func Text(w io.Writer, res *scan.Result, cfg Config) error {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	short := func(path string) string { return format.ShortPath(path, cfg.Home) }
	age := func(t time.Time) string {
		s := format.PadLeft(format.Age(t, cfg.Now), 7)
		if !t.IsZero() && cfg.Now.Sub(t) > cfg.Stale {
			return warn.Render(s)
		}
		return s
	}
	section := func(title string) { p("\n%s\n", hdr.Render(title)) }
	root := res.Root

	p("%s %s\n", hdr.Render("inventur report:"), short(root.Path()))
	p("%s on disk · %s files · %s folders · scanned in %s\n",
		sizeS.Render(format.Size(root.Size)), format.Count(root.FileCount), format.Count(root.DirCount),
		res.Duration.Round(100*time.Millisecond))
	if res.Canceled {
		p("%s\n", warn.Render("Scan was canceled — results are incomplete."))
	}

	section(fmt.Sprintf("Largest files (≥ %s)", format.Size(res.Options.MinFile)))
	if len(res.BigFiles) == 0 {
		p("  %s\n", dim.Render("none"))
	}
	for i, f := range res.BigFiles {
		if i == cfg.Top {
			p("  %s\n", dim.Render(fmt.Sprintf("… and %d more", len(res.BigFiles)-i)))
			break
		}
		cat := ""
		if c := f.EffectiveCategory(); c != nil {
			cat = dim.Render(" [" + c.Name + "]")
		}
		sparse := ""
		if f.Apparent > 2*f.Size {
			sparse = dim.Render(fmt.Sprintf(" (%s apparent)", format.Size(f.Apparent)))
		}
		p("  %s %s  %s%s%s\n", sizeS.Render(format.PadLeft(format.Size(f.Size), 9)), age(f.ModTimeTime()), short(f.Path()), sparse, cat)
	}

	section(fmt.Sprintf("Large directories (≥ %s)", format.Size(cfg.MinDir)))
	for _, l := range Prune(root, cfg.MinDir, cfg.Depth) {
		label := l.Label
		if l.Depth == 0 {
			label = short(label)
		}
		cat := ""
		if l.Category != nil {
			cat = dim.Render(" [" + l.Category.Name + "]")
		}
		pct := float64(l.Node.Size) / float64(max(root.Size, 1)) * 100
		p("  %s %s %s  %s%s/%s\n", sizeS.Render(format.PadLeft(format.Size(l.Node.Size), 9)),
			dim.Render(format.PadLeft(fmt.Sprintf("%.0f%%", pct), 4)), age(l.Node.NewestTime()),
			strings.Repeat("  ", l.Depth), label, cat)
	}

	section("Categories")
	if len(res.Categories) == 0 {
		p("  %s\n", dim.Render("none"))
	}
	for _, c := range res.Categories {
		p("  %s %s  %s  %s\n", sizeS.Render(format.PadLeft(format.Size(c.Bytes), 9)),
			format.PadLeft(format.Count(c.Count), 7), format.PadRight(c.Category.Name, 28), dim.Render(c.Category.Hint))
	}
	for _, c := range res.Categories {
		if len(c.Instances) == 0 {
			continue
		}
		section(fmt.Sprintf("%s — %s", c.Category.Name, format.Size(c.Bytes)))
		for i, e := range c.Instances {
			if i == cfg.PerCategory {
				p("  %s\n", dim.Render(fmt.Sprintf("… and %d more", len(c.Instances)-i)))
				break
			}
			path := short(e.Path())
			if e.Dir != nil {
				path += "/"
			}
			p("  %s %s  %s\n", sizeS.Render(format.PadLeft(format.Size(e.Size()), 9)), age(e.ModTime()), path)
		}
	}

	if len(res.Skipped) > 0 || res.Dataless > 0 {
		section(fmt.Sprintf("Not scanned (%d)", len(res.Skipped)))
		for i, s := range res.Skipped {
			if i == cfg.Top {
				p("  %s\n", dim.Render(fmt.Sprintf("… and %d more", len(res.Skipped)-i)))
				break
			}
			p("  %s  %s\n", format.PadRight(s.Reason, 28), short(s.Path))
		}
		if res.Dataless > 0 {
			p("  %s\n", dim.Render(fmt.Sprintf("%d iCloud folders not downloaded locally were skipped.", res.Dataless)))
		}
		if hasPermissionErrors(res) {
			p("  %s\n", dim.Render("Tip: grant your terminal Full Disk Access (System Settings › Privacy & Security) to scan these."))
		}
	}

	out := b.String()
	if !cfg.Color {
		out = ansi.Strip(out)
	}
	_, err := lipgloss.Fprint(w, out)
	return err
}

func hasPermissionErrors(res *scan.Result) bool {
	for _, s := range res.Skipped {
		if s.Reason == "permission denied" {
			return true
		}
	}
	return false
}
