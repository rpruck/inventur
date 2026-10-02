package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoMutatingCalls guards the read-only promise: no non-test code may call
// anything that creates, changes or deletes files.
func TestNoMutatingCalls(t *testing.T) {
	forbidden := regexp.MustCompile(`\b(os\.(Remove|RemoveAll|Rename|Chmod|Chown|Lchown|Chtimes|Create|CreateTemp|WriteFile|Truncate|Mkdir|MkdirAll|MkdirTemp|Link|Symlink|OpenFile)|syscall\.(Unlink|Unlinkat|Rmdir|Rename|Truncate|Ftruncate))\(`)
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if m := forbidden.FindString(line); m != "" {
				t.Errorf("%s:%d: mutating call %s", path, i+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
