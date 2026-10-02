package report

import (
	"encoding/json"
	"io"
	"time"

	"inventur/internal/scan"
)

type jsonReport struct {
	Root          string         `json:"root"`
	ScannedAt     time.Time      `json:"scanned_at"`
	DurationMS    int64          `json:"duration_ms"`
	Canceled      bool           `json:"canceled"`
	Bytes         int64          `json:"bytes"`
	ApparentBytes int64          `json:"apparent_bytes"`
	Files         int64          `json:"files"`
	Dirs          int64          `json:"dirs"`
	BigFiles      []jsonFile     `json:"big_files"`
	DirTree       jsonDir        `json:"dir_tree"`
	Categories    []jsonCategory `json:"categories"`
	Skipped       []jsonSkip     `json:"skipped"`
	DatalessDirs  int64          `json:"dataless_dirs_skipped"`
}

type jsonFile struct {
	Path          string    `json:"path"`
	Bytes         int64     `json:"bytes"`
	ApparentBytes int64     `json:"apparent_bytes"`
	Modified      time.Time `json:"modified"`
	Category      string    `json:"category,omitempty"`
}

type jsonDir struct {
	Path     string    `json:"path"`
	Bytes    int64     `json:"bytes"`
	Files    int64     `json:"files"`
	Newest   time.Time `json:"newest_modified"`
	Category string    `json:"category,omitempty"`
	Children []jsonDir `json:"children,omitempty"`
}

type jsonCategory struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Hint      string         `json:"hint"`
	Bytes     int64          `json:"bytes"`
	Count     int64          `json:"count"`
	Instances []jsonInstance `json:"instances"`
}

type jsonInstance struct {
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

type jsonSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// JSON writes the full machine-readable report.
func JSON(w io.Writer, res *scan.Result, cfg Config) error {
	root := res.Root
	r := jsonReport{
		Root:          root.Path(),
		ScannedAt:     res.Started,
		DurationMS:    res.Duration.Milliseconds(),
		Canceled:      res.Canceled,
		Bytes:         root.Size,
		ApparentBytes: root.Apparent,
		Files:         root.FileCount,
		Dirs:          root.DirCount,
		BigFiles:      []jsonFile{},
		DirTree:       dirTree(root, cfg.MinDir, cfg.Depth),
		Categories:    []jsonCategory{},
		Skipped:       []jsonSkip{},
		DatalessDirs:  res.Dataless,
	}
	for _, f := range res.BigFiles {
		jf := jsonFile{Path: f.Path(), Bytes: f.Size, ApparentBytes: f.Apparent, Modified: f.ModTimeTime()}
		if c := f.EffectiveCategory(); c != nil {
			jf.Category = c.ID
		}
		r.BigFiles = append(r.BigFiles, jf)
	}
	for _, c := range res.Categories {
		jc := jsonCategory{ID: c.Category.ID, Name: c.Category.Name, Hint: c.Category.Hint, Bytes: c.Bytes, Count: c.Count, Instances: []jsonInstance{}}
		for _, e := range c.Instances {
			typ := "file"
			if e.Dir != nil {
				typ = "dir"
			}
			jc.Instances = append(jc.Instances, jsonInstance{Path: e.Path(), Type: typ, Bytes: e.Size(), Modified: e.ModTime()})
		}
		r.Categories = append(r.Categories, jc)
	}
	for _, s := range res.Skipped {
		r.Skipped = append(r.Skipped, jsonSkip(s))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func dirTree(n *scan.Node, minSize int64, depth int) jsonDir {
	d := jsonDir{Path: n.Path(), Bytes: n.Size, Files: n.FileCount, Newest: n.NewestTime()}
	if n.Category != nil {
		d.Category = n.Category.ID
	}
	if depth > 0 {
		for _, c := range big(n, minSize) {
			d.Children = append(d.Children, dirTree(c, minSize, depth-1))
		}
	}
	return d
}
