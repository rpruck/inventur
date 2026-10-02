package scan

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"inventur/internal/category"
)

// Node is a directory in the scanned tree.
type Node struct {
	Name   string // base name; the root holds its full path
	Parent *Node

	Children []*Node
	Files    []*File // files of at least Options.ListMin, largest first

	SmallCount int64 // files below ListMin, aggregated
	SmallBytes int64

	OwnBytes    int64 // allocated bytes of this dir's own entries (incl. the dir itself)
	OwnApparent int64
	OwnFiles    int64

	// Subtree totals, filled in by finalize.
	Size      int64
	Apparent  int64
	FileCount int64
	DirCount  int64
	Newest    int64 // unix seconds of the newest mtime in the subtree

	// Category is set if this directory is a category instance. CatRoot points
	// to the nearest categorized ancestor-or-self (nil if none).
	Category *category.Category
	CatRoot  *Node

	Err string // set if the directory could not be read
}

// File is a single file large enough to be tracked individually.
type File struct {
	Name     string
	Parent   *Node
	Size     int64 // allocated bytes on disk
	Apparent int64 // logical size (st_size)
	ModTime  int64 // unix seconds
	Category *category.Category
}

// Path returns the absolute path of the directory.
func (n *Node) Path() string {
	if n.Parent == nil {
		return n.Name
	}
	var parts []string
	for c := n; c.Parent != nil; c = c.Parent {
		parts = append(parts, c.Name)
	}
	slices.Reverse(parts)
	return filepath.Join(append([]string{rootOf(n).Name}, parts...)...)
}

// NewestTime returns the newest modification time in the subtree.
func (n *Node) NewestTime() time.Time { return unixTime(n.Newest) }

// EffectiveCategory returns the category this directory is counted under.
func (n *Node) EffectiveCategory() *category.Category {
	if n.CatRoot != nil {
		return n.CatRoot.Category
	}
	return nil
}

// Find returns the descendant at the given absolute path, or nil.
func (n *Node) Find(path string) *Node {
	root := n.Path()
	if path == root {
		return n
	}
	rel, ok := strings.CutPrefix(path, strings.TrimSuffix(root, "/")+"/")
	if !ok {
		return nil
	}
	cur := n
	for _, part := range strings.Split(rel, "/") {
		var next *Node
		for _, c := range cur.Children {
			if c.Name == part {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// Path returns the absolute path of the file.
func (f *File) Path() string { return filepath.Join(f.Parent.Path(), f.Name) }

// ModTimeTime returns the modification time.
func (f *File) ModTimeTime() time.Time { return unixTime(f.ModTime) }

// EffectiveCategory returns the enclosing categorized directory's category,
// or the file's own category.
func (f *File) EffectiveCategory() *category.Category {
	if c := f.Parent.EffectiveCategory(); c != nil {
		return c
	}
	return f.Category
}

func rootOf(n *Node) *Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

func unixTime(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}

// finalize computes subtree totals bottom-up and sorts children and files
// largest first.
func finalize(n *Node) {
	n.Size, n.Apparent, n.FileCount, n.DirCount = n.OwnBytes, n.OwnApparent, n.OwnFiles, 0
	for _, c := range n.Children {
		finalize(c)
		n.Size += c.Size
		n.Apparent += c.Apparent
		n.FileCount += c.FileCount
		n.DirCount += 1 + c.DirCount
		n.Newest = max(n.Newest, c.Newest)
	}
	slices.SortFunc(n.Children, func(a, b *Node) int { return cmpSize(a.Size, b.Size, a.Name, b.Name) })
	slices.SortFunc(n.Files, func(a, b *File) int { return cmpSize(a.Size, b.Size, a.Name, b.Name) })
}

// cmpSize orders by size descending, then name ascending.
func cmpSize(a, b int64, an, bn string) int {
	if a != b {
		if a > b {
			return -1
		}
		return 1
	}
	return strings.Compare(an, bn)
}
