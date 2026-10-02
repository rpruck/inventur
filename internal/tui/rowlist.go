package tui

import (
	"strings"
	"time"

	"inventur/internal/scan"
)

type rowKind int

const (
	rowDir rowKind = iota
	rowFile
	rowUp    // "../" in the folder browser
	rowSmall // aggregate of a folder's small files
	rowCategory
	rowSkip
)

// row is one selectable line. Exactly one payload field is set, depending on
// kind (rowUp and rowSmall use dir).
type row struct {
	kind  rowKind
	dir   *scan.Node
	file  *scan.File
	cat   *scan.CategoryStat
	skip  scan.Skip
	size  int64
	count int64
	mtime time.Time
	key   string // lowercased text the filter matches against
}

// path returns the filesystem path the row refers to, if any.
func (r row) path() string {
	switch r.kind {
	case rowDir, rowUp, rowSmall:
		return r.dir.Path()
	case rowFile:
		return r.file.Path()
	case rowSkip:
		return r.skip.Path
	}
	return ""
}

// doubleClick is the max interval between two clicks on the same row.
const doubleClick = 400 * time.Millisecond

// rowList owns selection, scrolling, filtering and click handling for a list
// of rows. Tabs only build rows and render them.
type rowList struct {
	all    []row
	rows   []row // all, filtered
	filter string
	cursor int
	offset int
	height int

	lastClick   int
	lastClickAt time.Time
}

// set replaces the rows, keeping the selection on the same item if it is
// still present.
func (l *rowList) set(rows []row) {
	prev, hadPrev := l.selected()
	l.all = rows
	l.apply()
	if hadPrev && l.selectWhere(func(r row) bool { return sameRow(r, prev) }) {
		return
	}
	l.moveTo(l.cursor)
}

func (l *rowList) setFilter(q string) {
	l.filter = strings.ToLower(q)
	l.apply()
	l.moveTo(0)
}

func (l *rowList) apply() {
	if l.filter == "" {
		l.rows = l.all
		return
	}
	l.rows = l.rows[:0:0]
	for _, r := range l.all {
		if r.kind == rowUp || strings.Contains(r.key, l.filter) {
			l.rows = append(l.rows, r)
		}
	}
}

func (l *rowList) selected() (row, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return row{}, false
	}
	return l.rows[l.cursor], true
}

func (l *rowList) move(delta int) { l.moveTo(l.cursor + delta) }

func (l *rowList) moveTo(i int) {
	l.cursor = max(0, min(i, len(l.rows)-1))
	l.ensureVisible()
}

func (l *rowList) setHeight(h int) {
	l.height = max(1, h)
	l.ensureVisible()
}

func (l *rowList) ensureVisible() {
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.height {
		l.offset = l.cursor - l.height + 1
	}
	l.offset = max(0, min(l.offset, len(l.rows)-l.height))
}

// visible returns the index range of rows on screen.
func (l *rowList) visible() (start, end int) {
	return l.offset, min(len(l.rows), l.offset+l.height)
}

// selectWhere moves the cursor to the first row matching pred.
func (l *rowList) selectWhere(pred func(row) bool) bool {
	for i, r := range l.rows {
		if pred(r) {
			l.moveTo(i)
			return true
		}
	}
	return false
}

// click handles a click on screen line y (relative to the list top). It
// selects the row and reports whether this was a double-click.
func (l *rowList) click(y int, now time.Time) (ok, double bool) {
	i := l.offset + y
	if y < 0 || y >= l.height || i >= len(l.rows) {
		return false, false
	}
	double = i == l.lastClick && now.Sub(l.lastClickAt) <= doubleClick
	l.moveTo(i)
	if double {
		l.lastClickAt = time.Time{} // a third click starts over
	} else {
		l.lastClick, l.lastClickAt = i, now
	}
	return true, double
}

// sameRow matches rows by kind and key (path), so selections survive a
// rebuild or a rescan that produced new nodes.
func sameRow(a, b row) bool { return a.kind == b.kind && a.key == b.key }
