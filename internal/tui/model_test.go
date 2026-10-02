package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"inventur/internal/scan"
)

type fakeActions struct{ revealed, copied []string }

func (f *fakeActions) Reveal(p string) error { f.revealed = append(f.revealed, p); return nil }
func (f *fakeActions) Copy(p string) error   { f.copied = append(f.copied, p); return nil }

type harness struct {
	t    *testing.T
	m    *Model
	act  *fakeActions
	root string
	now  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, size int) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, bytes.Repeat([]byte{'x'}, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proj/package.json", 10)
	write("proj/node_modules/a/index.js", 300_000)
	write("logs/big.log", 3_000_000)
	write("logs/other.log", 2_000_000)
	write("models/model.safetensors", 1_500_000)

	opts := scan.Options{Root: root, Home: "/nonexistent-home", MinFile: 1_000_000, ListMin: 1_000_000}
	res, err := scan.New(opts).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, act: &fakeActions{}, root: root, now: time.Now()}
	h.m = New(Config{Scan: opts, Stale: 180 * 24 * time.Hour, Actions: h.act, Now: func() time.Time { return h.now }})
	h.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	h.send(scanDoneMsg{gen: h.m.scanGen, res: res})
	return h
}

// send delivers a message and runs any resulting command (one level deep),
// feeding action results back in.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	if cmd == nil {
		return
	}
	if out, ok := cmd().(actionDoneMsg); ok {
		h.m.Update(out)
	}
}

func (h *harness) key(k string) {
	h.t.Helper()
	switch k {
	case "enter":
		h.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	case "backspace":
		h.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	case "esc":
		h.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	default:
		for _, r := range k {
			h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

func (h *harness) click(x, y int) {
	h.t.Helper()
	h.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func (h *harness) selected() row {
	r, ok := h.m.list().selected()
	if !ok {
		h.t.Fatal("nothing selected")
	}
	return r
}

func (h *harness) screen() string { return ansi.Strip(h.m.View().Content) }

func TestFilesTabRevealOnEnter(t *testing.T) {
	h := newHarness(t)
	if h.m.tab != tabFiles || len(h.m.files.rows) != 3 {
		t.Fatalf("want Files tab with 3 big files, got tab %d with %d rows", h.m.tab, len(h.m.files.rows))
	}
	if got := h.selected().file.Name; got != "big.log" {
		t.Fatalf("largest file first, got %s", got)
	}
	h.key("enter")
	if want := filepath.Join(h.root, "logs/big.log"); len(h.act.revealed) != 1 || h.act.revealed[0] != want {
		t.Fatalf("revealed %v, want %s", h.act.revealed, want)
	}
	if !strings.Contains(h.screen(), "Revealed in Finder") {
		t.Error("flash message missing")
	}
	h.key("y")
	if len(h.act.copied) != 1 {
		t.Errorf("copy not called: %v", h.act.copied)
	}
}

func TestFolderDrillDownAndBack(t *testing.T) {
	h := newHarness(t)
	h.key("2")
	if h.m.tab != tabFolders || h.m.cwd != h.m.res.Root {
		t.Fatal("want Folders tab at root")
	}
	if !h.m.folders.selectWhere(func(r row) bool { return r.kind == rowDir && r.dir.Name == "logs" }) {
		t.Fatal("logs dir not listed")
	}
	h.key("enter")
	if h.m.cwd.Name != "logs" {
		t.Fatalf("cwd = %s, want logs", h.m.cwd.Name)
	}
	if h.m.folders.rows[0].kind != rowUp || h.selected().kind != rowFile {
		t.Error("want ../ row first and the first file selected")
	}
	h.key("backspace")
	if h.m.cwd != h.m.res.Root || h.selected().dir.Name != "logs" {
		t.Errorf("back should return to root with logs selected, got %s / %+v", h.m.cwd.Name, h.selected())
	}
	if !strings.Contains(h.screen(), "logs/") {
		t.Error("folder row not rendered")
	}
}

func TestMouseClickAndDoubleClick(t *testing.T) {
	h := newHarness(t)
	lay := h.m.layout()
	h.click(20, lay.listY+1)
	if h.m.files.cursor != 1 {
		t.Fatalf("click should select row 1, cursor = %d", h.m.files.cursor)
	}
	if len(h.act.revealed) != 0 {
		t.Fatal("single click must not reveal")
	}
	h.click(20, lay.listY+1) // same instant: double-click
	if len(h.act.revealed) != 1 || h.act.revealed[0] != h.selected().file.Path() {
		t.Fatalf("double-click should reveal the clicked file, got %v", h.act.revealed)
	}

	h.now = h.now.Add(time.Second)
	h.click(20, lay.listY+2)
	h.now = h.now.Add(time.Second)
	h.click(20, lay.listY+2) // too slow: no double-click
	if len(h.act.revealed) != 1 {
		t.Errorf("slow clicks must not count as double-click")
	}

	// Clicking a tab switches to it.
	sp := lay.tabs[tabCategories]
	h.click(sp.x0+1, lay.tabsY)
	if h.m.tab != tabCategories {
		t.Errorf("tab click: tab = %d", h.m.tab)
	}
}

func TestBreadcrumbClick(t *testing.T) {
	h := newHarness(t)
	h.key("2")
	h.m.folders.selectWhere(func(r row) bool { return r.kind == rowDir && r.dir.Name == "proj" })
	h.key("enter")
	h.m.folders.selectWhere(func(r row) bool { return r.kind == rowDir && r.dir.Name == "node_modules" })
	h.key("enter")
	if h.m.cwd.Name != "node_modules" {
		t.Fatalf("cwd = %s", h.m.cwd.Name)
	}
	lay := h.m.layout()
	h.click(lay.crumbs[0].x0, lay.contextY) // root crumb
	if h.m.cwd != h.m.res.Root || h.selected().dir.Name != "proj" {
		t.Errorf("crumb click should jump to root with proj selected, got %s", h.m.cwd.Name)
	}
}

func TestGotoFromFiles(t *testing.T) {
	h := newHarness(t)
	h.m.files.selectWhere(func(r row) bool { return r.file.Name == "model.safetensors" })
	h.key("g")
	if h.m.tab != tabFolders || h.m.cwd.Name != "models" {
		t.Fatalf("g should open the file's folder, got tab %d cwd %s", h.m.tab, h.m.cwd.Name)
	}
	if r := h.selected(); r.kind != rowFile || r.file.Name != "model.safetensors" {
		t.Errorf("file should be selected, got %+v", r)
	}
}

func TestCategoriesAndFilter(t *testing.T) {
	h := newHarness(t)
	h.key("3")
	h.key("/")
	if !h.m.filtering {
		t.Fatal("/ should start filtering")
	}
	h.key("log")
	if n := len(h.m.cats.rows); n != 1 || h.m.cats.rows[0].cat.Category.Name != "Log files" {
		t.Fatalf("filter should leave only Log files, got %d rows", n)
	}
	h.key("enter") // leave filter input, keep filter
	h.key("enter") // open category
	if h.m.catSel == nil || len(h.m.inst.rows) != 2 {
		t.Fatalf("want 2 log instances, got %+v", h.m.catSel)
	}
	if !strings.Contains(h.screen(), "‹ Categories") {
		t.Error("back crumb missing")
	}
	h.key("esc")
	if h.m.catSel != nil {
		t.Error("esc should return to the category list")
	}
	h.key("esc")
	if h.m.cats.filter != "" || len(h.m.cats.rows) < 2 {
		t.Error("esc should clear the filter")
	}
}

func TestSortByAge(t *testing.T) {
	h := newHarness(t)
	old := h.m.res.BigFiles[2]
	old.ModTime = 1 // 1970
	h.key("s")
	if got := h.selected(); got.file == nil {
		t.Fatal("selection lost")
	}
	if h.m.files.rows[0].file != old {
		t.Errorf("oldest file should come first when sorting by age")
	}
	if !strings.Contains(h.screen(), "sort: oldest") {
		t.Error("sort indicator missing")
	}
}

func TestViewFitsScreen(t *testing.T) {
	h := newHarness(t)
	for _, tab := range []string{"1", "2", "3"} {
		h.key(tab)
		lines := strings.Split(h.screen(), "\n")
		if len(lines) != h.m.height {
			t.Errorf("tab %s: %d lines, want %d", tab, len(lines), h.m.height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > h.m.width {
				t.Errorf("tab %s line %d too wide (%d): %q", tab, i, w, l)
			}
		}
	}
}
