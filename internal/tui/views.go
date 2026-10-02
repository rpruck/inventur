package tui

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"inventur/internal/format"
	"inventur/internal/scan"
)

type tabID int

const (
	tabFiles tabID = iota
	tabFolders
	tabCategories
	tabSkipped
)

var tabNames = [...]string{"Files", "Folders", "Categories", "Not scanned"}

type styles struct {
	title, accent, dim, stale, dir, size, sel, tabOn, tabOff, bar, barEmpty, tag, ok, bad lipgloss.Style
}

func newStyles(dark bool) styles {
	c := lipgloss.LightDark(dark)
	accent := c(lipgloss.Color("#6D28D9"), lipgloss.Color("#A78BFA"))
	dim := c(lipgloss.Color("#6B7280"), lipgloss.Color("#8B93A1"))
	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }
	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(c(lipgloss.Color("#6D28D9"), lipgloss.Color("#7C3AED"))).Padding(0, 1),
		accent:   fg(accent).Bold(true),
		dim:      fg(dim),
		stale:    fg(c(lipgloss.Color("#B45309"), lipgloss.Color("#FBBF24"))),
		dir:      fg(c(lipgloss.Color("#1D4ED8"), lipgloss.Color("#7DB3FF"))).Bold(true),
		size:     lipgloss.NewStyle().Bold(true),
		sel:      lipgloss.NewStyle().Bold(true).Foreground(c(lipgloss.Color("#1F2937"), lipgloss.Color("#F9FAFB"))).Background(c(lipgloss.Color("#DDD6FE"), lipgloss.Color("#4C1D95"))),
		tabOn:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1),
		tabOff:   fg(dim).Padding(0, 1),
		bar:      fg(accent),
		barEmpty: fg(c(lipgloss.Color("#E5E7EB"), lipgloss.Color("#3F3F46"))),
		tag:      fg(c(lipgloss.Color("#0F766E"), lipgloss.Color("#5EEAD4"))),
		ok:       fg(c(lipgloss.Color("#047857"), lipgloss.Color("#34D399"))),
		bad:      fg(c(lipgloss.Color("#B91C1C"), lipgloss.Color("#F87171"))),
	}
}

// ---- row builders ----

func (m *Model) buildFiles() []row {
	rows := make([]row, 0, len(m.res.BigFiles))
	for _, f := range m.res.BigFiles {
		rows = append(rows, m.fileRow(f))
	}
	m.sortRows(rows)
	return rows
}

func (m *Model) buildFolders() []row {
	n := m.cwd
	var rows []row
	for _, c := range n.Children {
		rows = append(rows, m.dirRow(c))
	}
	for _, f := range n.Files {
		rows = append(rows, m.fileRow(f))
	}
	if n.SmallCount > 0 {
		rows = append(rows, row{kind: rowSmall, dir: n, size: n.SmallBytes, count: n.SmallCount, key: "small files"})
	}
	m.sortRows(rows)
	if n.Parent != nil {
		rows = append([]row{{kind: rowUp, dir: n.Parent, key: ".."}}, rows...)
	}
	return rows
}

func (m *Model) buildCategories() []row {
	rows := make([]row, 0, len(m.res.Categories))
	for _, c := range m.res.Categories {
		rows = append(rows, row{kind: rowCategory, cat: c, size: c.Bytes, count: c.Count, key: strings.ToLower(c.Category.Name)})
	}
	return rows
}

func (m *Model) buildInstances() []row {
	rows := make([]row, 0, len(m.catSel.Instances))
	for _, e := range m.catSel.Instances {
		if e.Dir != nil {
			rows = append(rows, m.dirRow(e.Dir))
		} else {
			rows = append(rows, m.fileRow(e.File))
		}
	}
	m.sortRows(rows)
	return rows
}

func (m *Model) buildSkipped() []row {
	rows := make([]row, 0, len(m.res.Skipped))
	for _, s := range m.res.Skipped {
		rows = append(rows, row{kind: rowSkip, skip: s, key: strings.ToLower(m.short(s.Path) + " " + s.Reason)})
	}
	return rows
}

func (m *Model) fileRow(f *scan.File) row {
	return row{kind: rowFile, file: f, size: f.Size, mtime: f.ModTimeTime(), key: strings.ToLower(m.short(f.Path()))}
}

func (m *Model) dirRow(n *scan.Node) row {
	return row{kind: rowDir, dir: n, size: n.Size, mtime: n.NewestTime(), key: strings.ToLower(m.short(n.Path()))}
}

// sortRows orders by size (largest first) or by age (oldest first).
func (m *Model) sortRows(rows []row) {
	if !m.sortByAge {
		slices.SortStableFunc(rows, func(a, b row) int { return cmp.Compare(b.size, a.size) })
		return
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		if a.mtime.IsZero() != b.mtime.IsZero() {
			if a.mtime.IsZero() {
				return 1
			}
			return -1
		}
		return a.mtime.Compare(b.mtime)
	})
}

// ---- row rendering ----

const (
	sizeW = 9
	ageW  = 7
	barW  = 12
)

func (m *Model) renderRow(r row, w int, selected bool) string {
	var s string
	switch {
	case m.tab == tabFolders:
		s = m.folderLine(r, w)
	case r.kind == rowCategory:
		s = m.categoryLine(r, w)
	case r.kind == rowSkip:
		s = m.skipLine(r, w)
	default:
		s = m.pathLine(r, w)
	}
	if selected {
		return m.st.sel.Render(format.PadRight(ansi.Strip(s), w))
	}
	return ansi.Truncate(s, w, "")
}

func (m *Model) sizeCell(b int64) string {
	return m.st.size.Render(format.PadLeft(format.Size(b), sizeW))
}

func (m *Model) ageCell(t time.Time) string {
	s := format.PadLeft(format.Age(t, m.now()), ageW)
	if !t.IsZero() && m.now().Sub(t) > m.stale {
		return m.st.stale.Render(s)
	}
	return s
}

func (m *Model) catTag(r row) string {
	var name string
	switch {
	case r.kind == rowDir && r.dir.Category != nil:
		name = r.dir.Category.Name
	case r.kind == rowFile:
		if c := r.file.EffectiveCategory(); c != nil {
			name = c.Name
		}
	}
	return name
}

// pathLine renders a file or directory with its full path (Files tab and
// category instances).
func (m *Model) pathLine(r row, w int) string {
	catW := 0
	if m.tab == tabFiles && w >= 100 {
		catW = 24
	}
	rest := w - sizeW - ageW - 4 - catW
	path := m.short(r.path())
	if r.kind == rowDir {
		path += "/"
	}
	line := m.sizeCell(r.size) + " " + m.ageCell(r.mtime) + "  "
	if catW > 0 {
		line += m.st.tag.Render(format.PadRight(m.catTag(r), catW-1)) + " "
	}
	return line + format.TruncLeft(path, rest)
}

func (m *Model) folderLine(r row, w int) string {
	if r.kind == rowUp {
		return strings.Repeat(" ", sizeW+1+barW+1+5+1+ageW+2) + m.st.dir.Render("../")
	}
	total := max(m.cwd.Size, 1)
	frac := float64(r.size) / float64(total)
	line := m.sizeCell(r.size) + " " + m.bar(frac) + " " + m.st.dim.Render(format.PadLeft(fmt.Sprintf("%.0f%%", frac*100), 5)) + " "
	rest := w - sizeW - barW - 5 - ageW - 5
	switch r.kind {
	case rowSmall:
		return line + strings.Repeat(" ", ageW) + "  " + m.st.dim.Render(format.Trunc(fmt.Sprintf("(%s smaller files)", format.Count(r.count)), rest))
	case rowDir:
		line += m.ageCell(r.mtime) + "  "
		name := m.st.dir.Render(format.Trunc(r.dir.Name+"/", rest))
		if tag := m.catTag(r); tag != "" {
			name += " " + m.st.tag.Render("["+tag+"]")
		}
		return line + name
	default:
		line += m.ageCell(r.mtime) + "  " + format.Trunc(r.file.Name, rest)
		if tag := m.catTag(r); tag != "" {
			line += " " + m.st.tag.Render("["+tag+"]")
		}
		return line
	}
}

func (m *Model) categoryLine(r row, w int) string {
	const nameW = 30
	line := m.sizeCell(r.size) + " " + format.PadLeft(format.Count(r.count), 8) + "  " + m.st.accent.Render(format.PadRight(r.cat.Category.Name, nameW)) + " "
	return line + m.st.dim.Render(format.Trunc(r.cat.Category.Hint, w-sizeW-8-nameW-4))
}

func (m *Model) skipLine(r row, w int) string {
	const reasonW = 34
	return m.st.stale.Render(format.PadRight(r.skip.Reason, reasonW)) + " " + format.TruncLeft(m.short(r.skip.Path), w-reasonW-1)
}

// bar renders a proportional bar with eighth-block resolution.
func (m *Model) bar(frac float64) string {
	eighths := int(frac*barW*8 + 0.5)
	full, part := eighths/8, eighths%8
	s := strings.Repeat("█", min(full, barW))
	if full < barW && part > 0 {
		s += string([]rune("▏▎▍▌▋▊▉")[part-1])
		full++
	}
	return m.st.bar.Render(s) + m.st.barEmpty.Render(strings.Repeat("─", max(0, barW-full)))
}

func (m *Model) columnHeader() string {
	var s string
	switch {
	case m.tab == tabFiles:
		s = format.PadLeft("Size", sizeW) + " " + format.PadLeft("Age", ageW) + "  "
		if m.width >= 100 {
			s += format.PadRight("Category", 24)
		}
		s += "Path"
	case m.tab == tabFolders:
		s = format.PadLeft("Size", sizeW) + " " + format.PadRight("", barW) + " " + format.PadLeft("%", 5) + " " + format.PadLeft("Age", ageW) + "  Name"
	case m.tab == tabCategories && m.catSel == nil:
		s = format.PadLeft("Size", sizeW) + " " + format.PadLeft("Items", 8) + "  " + format.PadRight("Category", 31) + "Hint"
	case m.tab == tabCategories:
		s = format.PadLeft("Size", sizeW) + " " + format.PadLeft("Age", ageW) + "  Path"
	case m.tab == tabSkipped:
		s = format.PadRight("Reason", 35) + "Path"
	}
	return m.st.dim.Render(s)
}

// ---- header, tabs, context line ----

func (m *Model) header() string {
	root := m.res.Root
	s := m.st.title.Render("inventur") + " " + m.st.accent.Render(m.short(root.Path())) + "  " +
		m.st.size.Render(format.Size(root.Size)) + m.st.dim.Render(" on disk · ") +
		format.Count(root.FileCount) + m.st.dim.Render(" files · ") +
		format.Count(root.DirCount) + m.st.dim.Render(" folders · ") +
		m.st.dim.Render(m.res.Duration.Round(100*time.Millisecond).String())
	if n := len(m.res.Skipped); n > 0 {
		s += "  " + m.st.stale.Render(fmt.Sprintf("⚠ %d not scanned", n))
	}
	return s
}

type span struct {
	x0, x1 int // [x0, x1)
}

// tabBar renders the tabs and returns each tab's horizontal extent.
func (m *Model) tabBar() (string, map[tabID]span) {
	spans := map[tabID]span{}
	var b strings.Builder
	x := 0
	for _, t := range m.tabs() {
		label := fmt.Sprintf("%d %s", int(t)+1, tabNames[t])
		if t == tabSkipped {
			label += fmt.Sprintf(" (%d)", len(m.res.Skipped))
		}
		st := m.st.tabOff
		if t == m.tab {
			st = m.st.tabOn
		}
		r := st.Render(label)
		w := lipgloss.Width(r)
		spans[t] = span{x, x + w}
		b.WriteString(r + " ")
		x += w + 1
	}
	sort := "size"
	if m.sortByAge {
		sort = "oldest"
	}
	right := m.st.dim.Render("sort: " + sort)
	if gap := m.width - x - lipgloss.Width(right); gap > 0 {
		b.WriteString(strings.Repeat(" ", gap) + right)
	}
	return b.String(), spans
}

func (m *Model) tabs() []tabID {
	t := []tabID{tabFiles, tabFolders, tabCategories}
	if len(m.res.Skipped) > 0 {
		t = append(t, tabSkipped)
	}
	return t
}

// crumb is a clickable segment of the context line. node is the folder to
// jump to; back means "leave the category instance list".
type crumb struct {
	span
	node *scan.Node
	back bool
}

// contextLine renders the line above the column header: a breadcrumb in
// the Folders tab, a summary elsewhere.
func (m *Model) contextLine() (string, []crumb) {
	sep := m.st.dim.Render(" › ")
	switch m.tab {
	case tabFiles:
		var total int64
		for _, f := range m.res.BigFiles {
			total += f.Size
		}
		return m.st.dim.Render(fmt.Sprintf("%s files ≥ %s · %s together", format.Count(int64(len(m.res.BigFiles))),
			format.Size(m.res.Options.MinFile), format.Size(total))), nil

	case tabFolders:
		var chain []*scan.Node
		for n := m.cwd; n != nil; n = n.Parent {
			chain = append(chain, n)
		}
		slices.Reverse(chain)
		var b strings.Builder
		var crumbs []crumb
		x := 0
		for i, n := range chain {
			if i > 0 {
				b.WriteString(sep)
				x += 3
			}
			label := n.Name
			if i == 0 {
				label = m.short(n.Path())
			}
			st := m.st.accent
			if n == m.cwd {
				st = m.st.dir
			}
			r := st.Render(label)
			w := lipgloss.Width(r)
			crumbs = append(crumbs, crumb{span: span{x, x + w}, node: n})
			b.WriteString(r)
			x += w
		}
		b.WriteString(m.st.dim.Render(fmt.Sprintf("   %s · %s files", format.Size(m.cwd.Size), format.Count(m.cwd.FileCount))))
		return b.String(), crumbs

	case tabCategories:
		if m.catSel == nil {
			var total int64
			for _, c := range m.res.Categories {
				total += c.Bytes
			}
			return m.st.dim.Render(fmt.Sprintf("%d categories · %s identified as known clutter", len(m.res.Categories), format.Size(total))), nil
		}
		back := m.st.accent.Render("‹ Categories")
		w := lipgloss.Width(back)
		c := m.catSel
		s := back + sep + m.st.dir.Render(c.Category.Name) +
			m.st.dim.Render(fmt.Sprintf("  %s in %s items · %s", format.Size(c.Bytes), format.Count(c.Count), c.Category.Hint))
		return s, []crumb{{span: span{0, w}, back: true}}

	case tabSkipped:
		return m.st.dim.Render("Grant your terminal Full Disk Access (System Settings › Privacy & Security) to scan protected folders."), nil
	}
	return "", nil
}
