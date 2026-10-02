package report

import (
	"inventur/internal/category"
	"inventur/internal/scan"
)

// TreeLine is one row of a pruned directory tree.
type TreeLine struct {
	Node  *scan.Node
	Label string // may be a collapsed chain like "a/b/c"
	Depth int
	// Category is the first category instance along the collapsed chain.
	Category *category.Category
}

// Prune returns the directories of at least minSize, depth-first, largest
// first, down to maxDepth levels below the root. A directory that is only a
// wrapper around one big child is collapsed into "parent/child".
func Prune(root *scan.Node, minSize int64, maxDepth int) []TreeLine {
	var out []TreeLine
	var emit func(n *scan.Node, label string, depth int, cat *category.Category)
	emit = func(n *scan.Node, label string, depth int, cat *category.Category) {
		out = append(out, TreeLine{Node: n, Label: label, Depth: depth, Category: cat})
		if depth >= maxDepth {
			return
		}
		for _, c := range big(n, minSize) {
			cur, l, cat := c, c.Name, c.Category
			for {
				bc := big(cur, minSize)
				if len(bc) != 1 || cur.Size-bc[0].Size >= minSize {
					break
				}
				cur = bc[0]
				l += "/" + cur.Name
				if cat == nil {
					cat = cur.Category
				}
			}
			emit(cur, l, depth+1, cat)
		}
	}
	emit(root, root.Path(), 0, root.Category)
	return out
}

// big returns the children of at least minSize (children are sorted by size).
func big(n *scan.Node, minSize int64) []*scan.Node {
	for i, c := range n.Children {
		if c.Size < minSize {
			return n.Children[:i]
		}
	}
	return n.Children
}
