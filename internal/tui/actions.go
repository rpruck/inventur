package tui

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// Actions are the only side effects the TUI can trigger. Neither touches the
// scanned files: Reveal only shows an item in Finder, Copy puts a path on the
// clipboard. There is deliberately no delete action.
type Actions interface {
	Reveal(path string) error
	Copy(text string) error
}

// ErrGone is returned when an item no longer exists (e.g. deleted in Finder).
var ErrGone = errors.New("no longer exists (deleted?)")

// System implements Actions with macOS tools.
type System struct{}

// Reveal selects the item in a Finder window ("open -R"). It never opens the
// file itself, so double-clicking a 50 GB log does not launch an editor.
func (System) Reveal(path string) error {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrGone
		}
		return err
	}
	return exec.Command("open", "-R", path).Run()
}

// Copy puts text on the clipboard via pbcopy.
func (System) Copy(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
