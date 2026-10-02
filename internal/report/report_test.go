package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"inventur/internal/category"
	"inventur/internal/scan"
)

// tree builds:
//
//	root(10) ─┬─ a(7) ── b(7) ─┬─ c(4)
//	          │                └─ d(3)
//	          └─ e(1)
func tree() *scan.Node {
	root := &scan.Node{Name: "/r", Size: 10}
	a := &scan.Node{Name: "a", Parent: root, Size: 7}
	b := &scan.Node{Name: "b", Parent: a, Size: 7, Category: category.NodeModules}
	c := &scan.Node{Name: "c", Parent: b, Size: 4}
	d := &scan.Node{Name: "d", Parent: b, Size: 3}
	e := &scan.Node{Name: "e", Parent: root, Size: 1}
	root.Children = []*scan.Node{a, e}
	a.Children = []*scan.Node{b}
	b.Children = []*scan.Node{c, d}
	return root
}

func TestPruneCollapsesWrappers(t *testing.T) {
	lines := Prune(tree(), 2, 8)
	var got []string
	for _, l := range lines {
		got = append(got, strings.Repeat(" ", l.Depth)+l.Label)
	}
	want := []string{"/r", " a/b", "  c", "  d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if lines[1].Category != category.NodeModules {
		t.Errorf("collapsed chain should carry b's category")
	}
	if n := len(Prune(tree(), 2, 1)); n != 2 {
		t.Errorf("depth 1: want root + a/b, got %d lines", n)
	}
}

func TestTextAndJSON(t *testing.T) {
	root := tree()
	f := &scan.File{Name: "huge.log", Parent: root.Children[1], Size: 5, ModTime: 1, Category: category.Logs}
	res := &scan.Result{
		Root:       root,
		BigFiles:   []*scan.File{f},
		Categories: []*scan.CategoryStat{{Category: category.Logs, Bytes: 5, Count: 1, Instances: []scan.Entry{{File: f}}}},
		Skipped:    []scan.Skip{{Path: "/r/private", Reason: "permission denied"}},
		Options:    scan.Options{MinFile: 1},
	}
	cfg := Config{Top: 10, PerCategory: 5, MinDir: 2, Depth: 8, Stale: time.Hour, Now: time.Now()}

	var txt bytes.Buffer
	if err := Text(&txt, res, cfg); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/r/e/huge.log", "[Log files]", "a/b/ [Node modules]", "permission denied", "Full Disk Access"} {
		if !strings.Contains(txt.String(), want) {
			t.Errorf("text report missing %q:\n%s", want, txt.String())
		}
	}

	var js bytes.Buffer
	if err := JSON(&js, res, cfg); err != nil {
		t.Fatal(err)
	}
	var parsed jsonReport
	if err := json.Unmarshal(js.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.BigFiles[0].Path != "/r/e/huge.log" || parsed.Categories[0].ID != "logs" || len(parsed.DirTree.Children) != 1 {
		t.Errorf("unexpected JSON: %s", js.String())
	}
}
