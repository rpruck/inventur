package scan

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"inventur/internal/category"
)

// fixture builds a small tree covering the tricky cases and returns its root.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	file := func(rel string, size int) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte{'x'}, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file("proj/package.json", 10)
	file("proj/node_modules/a/index.js", 10_000)
	file("proj/node_modules/b/node_modules/c/index.js", 10_000) // nested: counted once
	file("proj/build/out.bin", 2_000_000)                       // build next to package.json
	file("rust/Cargo.toml", 10)
	file("rust/target/debug/app", 10_000)
	file("notrust/target/data.txt", 10) // no Cargo.toml: not a build dir
	file(".venv/pyvenv.cfg", 10)
	file(".venv/lib/__pycache__/x.pyc", 10) // inside venv: venv wins
	file("models/model.safetensors", 2_000_000)
	file("logs/big.log", 3_000_000)
	file("logs/small.log", 100)
	file("hard/a.bin", 1_500_000)
	if err := os.Link(filepath.Join(root, "hard/a.bin"), filepath.Join(root, "hard/b.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(root, "loop/up")); err != nil {
		t.Fatal(err)
	}
	file("locked/secret.txt", 10)
	if err := os.Chmod(filepath.Join(root, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "locked"), 0o755) })
	return root
}

func run(t *testing.T, opts Options) *Result {
	t.Helper()
	opts.MinFile, opts.ListMin = 2_500_000, 1_000_000
	if opts.Home == "" {
		opts.Home = "/nonexistent-home"
	}
	res, err := New(opts).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func catStat(res *Result, c *category.Category) *CategoryStat {
	for _, s := range res.Categories {
		if s.Category == c {
			return s
		}
	}
	return nil
}

func TestScanCategories(t *testing.T) {
	root := fixture(t)
	res := run(t, Options{Root: root})

	if s := catStat(res, category.NodeModules); s == nil || s.Count != 1 {
		t.Errorf("node_modules: want 1 instance (nested counted once), got %+v", s)
	}
	if s := catStat(res, category.Build); s == nil || s.Count != 2 {
		t.Errorf("build outputs: want proj/build and rust/target, got %+v", s)
	}
	if n := res.Root.Find(filepath.Join(root, "notrust/target")); n == nil || n.Category != nil {
		t.Errorf("notrust/target should be scanned but not categorized: %+v", n)
	}
	if s := catStat(res, category.PyEnvs); s == nil || s.Count != 1 {
		t.Errorf("python envs: want 1, got %+v", s)
	}
	if s := catStat(res, category.DevCaches); s != nil {
		t.Errorf("__pycache__ inside a venv must count toward the venv, got dev caches %+v", s)
	}
	if s := catStat(res, category.ML); s == nil || s.Count != 1 || len(s.Instances) != 1 || s.Instances[0].File.Name != "model.safetensors" {
		t.Errorf("ML: want model.safetensors, got %+v", s)
	}
	logs := catStat(res, category.Logs)
	if logs == nil || logs.Count != 2 || len(logs.Instances) != 1 {
		t.Fatalf("logs: want 2 matches with 1 listed instance (small one aggregated), got %+v", logs)
	}
	if logs.Bytes < 3_000_000 {
		t.Errorf("logs bytes = %d, want ≥ 3 MB", logs.Bytes)
	}
}

func TestScanFilesAndTotals(t *testing.T) {
	root := fixture(t)
	res := run(t, Options{Root: root})

	if len(res.BigFiles) != 1 || res.BigFiles[0].Name != "big.log" {
		t.Fatalf("big files: want only big.log, got %v", names(res.BigFiles))
	}
	if got := res.BigFiles[0].Path(); got != filepath.Join(root, "logs/big.log") {
		t.Errorf("path = %q", got)
	}
	hard := res.Root.Find(filepath.Join(root, "hard"))
	if hard == nil || hard.OwnFiles != 1 {
		t.Errorf("hardlinked file should be counted once, got %+v", hard)
	}
	loop := res.Root.Find(filepath.Join(root, "loop"))
	if loop == nil || len(loop.Children) != 0 || loop.OwnFiles != 1 {
		t.Errorf("symlink must not be followed: %+v", loop)
	}
	logs := res.Root.Find(filepath.Join(root, "logs"))
	if len(logs.Files) != 1 || logs.SmallCount != 1 {
		t.Errorf("logs: want 1 listed file and 1 small file, got %d / %d", len(logs.Files), logs.SmallCount)
	}

	var sum int64
	for _, c := range res.Root.Children {
		sum += c.Size
	}
	if res.Root.Size != sum+res.Root.OwnBytes {
		t.Errorf("root size %d != children %d + own %d", res.Root.Size, sum, res.Root.OwnBytes)
	}
	for i := 1; i < len(res.Root.Children); i++ {
		if res.Root.Children[i-1].Size < res.Root.Children[i].Size {
			t.Errorf("children not sorted by size")
		}
	}
}

func TestScanSkipsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	root := fixture(t)
	res := run(t, Options{Root: root})
	locked := filepath.Join(root, "locked")
	found := false
	for _, s := range res.Skipped {
		if s.Path == locked && s.Reason == "permission denied" {
			found = true
		}
	}
	if !found {
		t.Errorf("locked dir not reported: %+v", res.Skipped)
	}
	if n := res.Root.Find(locked); n == nil || n.Err == "" {
		t.Errorf("locked node should carry the error: %+v", n)
	}
}

func TestScanExcludeAndCancel(t *testing.T) {
	root := fixture(t)
	res := run(t, Options{Root: root, Excludes: []string{"node_modules", filepath.Join(root, "rust")}})
	if catStat(res, category.NodeModules) != nil {
		t.Error("excluded node_modules still reported")
	}
	if res.Root.Find(filepath.Join(root, "rust")) != nil {
		t.Error("excluded path prefix still scanned")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := New(Options{Root: root}).Run(ctx)
	if err != nil || !res.Canceled {
		t.Errorf("want canceled result, got %v %v", res, err)
	}
}

// TestScanIsReadOnly snapshots the fixture before and after a scan.
func TestScanIsReadOnly(t *testing.T) {
	root := fixture(t)
	before := snapshot(t, root)
	run(t, Options{Root: root})
	after := snapshot(t, root)
	if before != after {
		t.Errorf("tree changed during scan:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func snapshot(t *testing.T, root string) string {
	var lines []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			lines = append(lines, p+" ERR")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		lines = append(lines, fmt.Sprintf("%s %v %d %d", p, info.Mode(), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(lines)
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func names(fs []*File) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}
