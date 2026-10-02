// Package scan walks a directory tree read-only and builds a size tree with
// clutter categories. It never opens file contents and never modifies
// anything: the only filesystem calls are opening directories for reading,
// reading their entries and lstat.
package scan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"inventur/internal/category"
)

// sfDataless marks iCloud placeholders whose contents are not on disk.
// Reading such a directory would make macOS download it.
const sfDataless = 0x40000000

// dataVolume is the macOS data volume. It shares the root's device ID and is
// reachable from "/" via firmlinks, so scanning it from "/" would double count.
const dataVolume = "/System/Volumes/Data"

// Options configures a scan.
type Options struct {
	Root        string
	Home        string   // for path rules; defaults to $HOME
	MinFile     int64    // files at least this big are reported as big files
	ListMin     int64    // files at least this big are tracked individually
	Workers     int      // max concurrent directory readers
	CrossMounts bool     // descend into other filesystems
	Excludes    []string // glob patterns matched against base name and full path
}

// Result is the outcome of a scan.
type Result struct {
	Root       *Node
	BigFiles   []*File
	Categories []*CategoryStat
	Skipped    []Skip
	Dataless   int64 // iCloud placeholder dirs not descended into
	Started    time.Time
	Duration   time.Duration
	Canceled   bool
	Options    Options
}

// CategoryStat aggregates one category across the tree.
type CategoryStat struct {
	Category  *category.Category
	Bytes     int64
	Count     int64
	Instances []Entry // largest first; small file matches only count in Bytes/Count
}

// Entry is either a directory or a file.
type Entry struct {
	Dir  *Node
	File *File
}

func (e Entry) Path() string {
	if e.Dir != nil {
		return e.Dir.Path()
	}
	return e.File.Path()
}

func (e Entry) Size() int64 {
	if e.Dir != nil {
		return e.Dir.Size
	}
	return e.File.Size
}

func (e Entry) ModTime() time.Time {
	if e.Dir != nil {
		return e.Dir.NewestTime()
	}
	return e.File.ModTimeTime()
}

// Skip is a directory that was not scanned, with the reason.
type Skip struct {
	Path   string
	Reason string
}

// Progress is a snapshot of a running scan.
type Progress struct {
	Files, Dirs, Bytes int64
	Current            string
}

// Scanner runs one scan and exposes its progress.
type Scanner struct {
	opts Options
	cls  *category.Classifier
	ctx  context.Context
	sem  chan struct{}
	wg   sync.WaitGroup
	dev  uint64

	files, dirs, bytes atomic.Int64
	dataless           atomic.Int64
	current            atomic.Pointer[string]

	mu         sync.Mutex
	skipped    []Skip
	hardlinks  map[[2]uint64]struct{}
	smallBytes map[*category.Category]int64
	smallCount map[*category.Category]int64
}

// New prepares a scanner, filling in defaults.
func New(opts Options) *Scanner {
	if opts.Home == "" {
		opts.Home, _ = os.UserHomeDir()
	}
	if opts.Workers <= 0 {
		opts.Workers = 4 * runtime.NumCPU()
	}
	if opts.ListMin <= 0 {
		opts.ListMin = 1_000_000
	}
	if opts.MinFile <= 0 {
		opts.MinFile = 50_000_000
	}
	opts.ListMin = min(opts.ListMin, opts.MinFile)
	if abs, err := filepath.Abs(opts.Root); err == nil {
		opts.Root = abs
	}
	return &Scanner{
		opts:       opts,
		cls:        category.NewClassifier(opts.Home),
		sem:        make(chan struct{}, opts.Workers),
		hardlinks:  map[[2]uint64]struct{}{},
		smallBytes: map[*category.Category]int64{},
		smallCount: map[*category.Category]int64{},
	}
}

// Progress returns a snapshot; safe to call concurrently with Run.
func (s *Scanner) Progress() Progress {
	p := Progress{Files: s.files.Load(), Dirs: s.dirs.Load(), Bytes: s.bytes.Load()}
	if c := s.current.Load(); c != nil {
		p.Current = *c
	}
	return p
}

// Run walks the tree. It returns an error only if the root itself cannot be
// read; everything below is collected into Result.Skipped.
func (s *Scanner) Run(ctx context.Context) (*Result, error) {
	start := time.Now()
	s.ctx = ctx
	root := s.opts.Root
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "scan", Path: root, Err: errors.New("not a directory")}
	}
	st := info.Sys().(*syscall.Stat_t)
	s.dev = uint64(st.Dev)

	rootNode := &Node{Name: root, OwnBytes: st.Blocks * 512, OwnApparent: st.Size, Newest: st.Mtimespec.Sec}
	if cat := s.cls.Dir(root, filepath.Base(root), nil); cat != nil {
		rootNode.Category, rootNode.CatRoot = cat, rootNode
	}
	s.walk(rootNode, root)
	s.wg.Wait()

	finalize(rootNode)
	res := &Result{
		Root:     rootNode,
		Skipped:  s.skipped,
		Dataless: s.dataless.Load(),
		Started:  start,
		Canceled: ctx.Err() != nil,
		Options:  s.opts,
	}
	s.collect(res)
	res.Duration = time.Since(start)
	return res, nil
}

// walk reads one directory, records its files and recurses into its
// subdirectories, in parallel where the worker budget allows.
func (s *Scanner) walk(n *Node, path string) {
	if s.ctx.Err() != nil {
		return
	}
	s.current.Store(&path)
	s.dirs.Add(1)

	entries, err := readDir(path)
	if err != nil {
		n.Err = reason(err)
		s.skip(path, n.Err)
		return
	}

	var names map[string]struct{}
	has := func(name string) bool {
		if names == nil {
			names = make(map[string]struct{}, len(entries))
			for _, e := range entries {
				names[e.Name()] = struct{}{}
			}
		}
		_, ok := names[name]
		return ok
	}
	if n.CatRoot == nil {
		if cat := s.cls.DirContents(has); cat != nil {
			n.Category, n.CatRoot = cat, n
		}
	}

	type sub struct {
		node *Node
		path string
	}
	var subdirs []sub
	var localBytes, localCount map[*category.Category]int64
	var files, bytes int64

	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(path, name)
		if s.excluded(p, name) {
			continue
		}
		info, err := e.Info() // lstat; never follows symlinks
		if err != nil {
			continue // vanished during the scan
		}
		st := info.Sys().(*syscall.Stat_t)
		alloc, apparent, mtime := st.Blocks*512, st.Size, st.Mtimespec.Sec

		if info.IsDir() {
			if r := s.skipDir(p, st); r != "" {
				if r != "dataless" {
					s.skip(p, r)
				}
				continue
			}
			child := &Node{Name: name, Parent: n, OwnBytes: alloc, OwnApparent: apparent, Newest: mtime}
			if n.CatRoot != nil {
				child.CatRoot = n.CatRoot
			} else if cat := s.cls.Dir(p, name, has); cat != nil {
				child.Category, child.CatRoot = cat, child
			}
			n.Children = append(n.Children, child)
			subdirs = append(subdirs, sub{child, p})
			continue
		}

		if st.Nlink > 1 && s.seenHardlink(uint64(st.Dev), st.Ino) {
			continue // another link to this inode was already counted
		}
		n.OwnBytes += alloc
		n.OwnApparent += apparent
		n.OwnFiles++
		n.Newest = max(n.Newest, mtime)
		files++
		bytes += alloc

		var cat *category.Category
		if n.CatRoot == nil && info.Mode().IsRegular() {
			cat = category.File(name)
		}
		if alloc >= s.opts.ListMin && info.Mode().IsRegular() {
			n.Files = append(n.Files, &File{Name: name, Parent: n, Size: alloc, Apparent: apparent, ModTime: mtime, Category: cat})
			continue
		}
		n.SmallCount++
		n.SmallBytes += alloc
		if cat != nil {
			if localBytes == nil {
				localBytes, localCount = map[*category.Category]int64{}, map[*category.Category]int64{}
			}
			localBytes[cat] += alloc
			localCount[cat]++
		}
	}
	s.files.Add(files)
	s.bytes.Add(bytes)
	if localBytes != nil {
		s.mu.Lock()
		for c, b := range localBytes {
			s.smallBytes[c] += b
			s.smallCount[c] += localCount[c]
		}
		s.mu.Unlock()
	}

	for _, d := range subdirs {
		select {
		case s.sem <- struct{}{}:
			s.wg.Add(1)
			go func() {
				defer func() { <-s.sem; s.wg.Done() }()
				s.walk(d.node, d.path)
			}()
		default:
			s.walk(d.node, d.path)
		}
	}
}

// readDir opens a directory read-only and returns its entries unsorted.
func readDir(path string) ([]os.DirEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// skipDir returns a non-empty reason if the directory must not be entered.
func (s *Scanner) skipDir(path string, st *syscall.Stat_t) string {
	if st.Flags&sfDataless != 0 {
		s.dataless.Add(1)
		return "dataless"
	}
	if path == dataVolume && !strings.HasPrefix(s.opts.Root, dataVolume) {
		return "macOS data volume (already scanned via firmlinks)"
	}
	if !s.opts.CrossMounts && uint64(st.Dev) != s.dev {
		return "other filesystem (use --cross-mounts)"
	}
	return ""
}

func (s *Scanner) excluded(path, name string) bool {
	for _, pat := range s.opts.Excludes {
		if ok, _ := filepath.Match(pat, name); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, path); ok {
			return true
		}
		if path == pat || strings.HasPrefix(path, strings.TrimSuffix(pat, "/")+"/") {
			return true
		}
	}
	return false
}

func (s *Scanner) seenHardlink(dev, ino uint64) bool {
	k := [2]uint64{dev, ino}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hardlinks[k]; ok {
		return true
	}
	s.hardlinks[k] = struct{}{}
	return false
}

func (s *Scanner) skip(path, reason string) {
	s.mu.Lock()
	s.skipped = append(s.skipped, Skip{Path: path, Reason: reason})
	s.mu.Unlock()
}

func reason(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		return "vanished during scan"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// collect gathers big files and category instances from the finished tree.
func (s *Scanner) collect(res *Result) {
	stats := map[*category.Category]*CategoryStat{}
	stat := func(c *category.Category) *CategoryStat {
		if st, ok := stats[c]; ok {
			return st
		}
		st := &CategoryStat{Category: c}
		stats[c] = st
		return st
	}
	var visit func(n *Node)
	visit = func(n *Node) {
		if n.Category != nil && n.CatRoot == n {
			st := stat(n.Category)
			st.Bytes += n.Size
			st.Count++
			st.Instances = append(st.Instances, Entry{Dir: n})
		}
		for _, f := range n.Files {
			if f.Size >= s.opts.MinFile {
				res.BigFiles = append(res.BigFiles, f)
			}
			if f.Category != nil {
				st := stat(f.Category)
				st.Bytes += f.Size
				st.Count++
				st.Instances = append(st.Instances, Entry{File: f})
			}
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(res.Root)

	for c, b := range s.smallBytes {
		st := stat(c)
		st.Bytes += b
		st.Count += s.smallCount[c]
	}
	for _, st := range stats {
		slices.SortFunc(st.Instances, func(a, b Entry) int {
			if a.Size() != b.Size() {
				return cmpSize(a.Size(), b.Size(), "", "")
			}
			return strings.Compare(a.Path(), b.Path())
		})
		res.Categories = append(res.Categories, st)
	}
	order := func(c *category.Category) int { return slices.Index(category.All, c) }
	slices.SortFunc(res.Categories, func(a, b *CategoryStat) int {
		if a.Bytes != b.Bytes {
			return cmpSize(a.Bytes, b.Bytes, "", "")
		}
		return order(a.Category) - order(b.Category)
	})
	slices.SortFunc(res.BigFiles, func(a, b *File) int { return cmpSize(a.Size, b.Size, a.Name, b.Name) })
	slices.SortFunc(res.Skipped, func(a, b Skip) int { return strings.Compare(a.Path, b.Path) })
}
