# inventur

I recently found a 54 GB pgAdmin log file which my Mac's storage settings page did not show because it was hiding as "System Data".
Apart from files like these, I have several GBs of node modules and PyTorch checkpoints lying around which also don't get picked up by the storage settings.
As am writing this I have about 6 GB of storage left and I don't even store photos on my Mac.
In German we call stocktaking "Inventur machen", so here is `inventur` which should find your clutter and help you get rid of it.

**Read-only by design.** inventur never deletes or modifies anything. It reveals items in Finder, and you decide what to delete there. `safety_test.go` enforces this: it fails the build if any non-test code calls a mutating filesystem function.

## Usage

```sh
go build -o inventur .
./inventur                # scan your home folder in the interactive TUI
./inventur ~/Documents    # scan a specific folder
./inventur /              # whole startup disk, incl. system caches and temp files
./inventur --report ~     # plain-text report (also used automatically when piped)
./inventur --json ~ > scan.json
```

### TUI

| Tab | Shows |
|---|---|
| 1 Files | every file ≥ `--min-file` (default 50 MB), largest first |
| 2 Folders | ncdu-style browser: drill into folders, sizes with % bars |
| 3 Categories | clutter categories with totals; open one to list its items |
| 4 Not scanned | folders that couldn't be read (shown only if there are any) |

| Key / mouse | Action |
|---|---|
| `enter` / double-click | open a folder · reveal a file in Finder |
| `o` | reveal the selected item in Finder (`open -R`; the file is never opened) |
| `g` | show the item in the Folders tab |
| `←` / `backspace` | up one folder / back to the category list |
| `/` | filter by path · `esc` clears |
| `s` | sort by size or by age (oldest first) |
| `y` | copy path |
| `r` | rescan (after deleting things in Finder) |
| `tab`, `1`–`4`, click a tab | switch tabs |
| `?` | all keys |

### Useful flags

- `--min-file 1GB`: only list really big files.
- `--stale 1y`: highlight items untouched for a year (default 180d).
- `--exclude PATTERN`: skip paths (glob on name or path, or a path prefix). Repeatable.
- `--cross-mounts`: also descend into other mounted volumes (e.g. the swap volume `/System/Volumes/VM`).

## Notes

- Sizes are **space actually used on disk**: allocated blocks, decimal units, like Finder. Sparse files such as `Docker.raw` and evicted iCloud files therefore count with their real footprint. Hard-linked files are counted once.
- iCloud folders that aren't downloaded locally are skipped. Reading them would trigger a download.
- Some folders are protected by macOS privacy controls. They show up under **Not scanned**. To include them, grant your terminal app Full Disk Access (System Settings › Privacy & Security). Scanning `~/Library` may also trigger one-time macOS prompts about accessing other apps' data.

## Adding clutter rules

All rules live in [`internal/category/rules.go`](internal/category/rules.go):

| Rule table | Matches by |
|---|---|
| `dirNames`, `dirPatterns` | folder name or regex |
| `siblingRules` | folder name plus a marker file in the parent (e.g. `target` next to `Cargo.toml`) |
| `contentMarkers` | a file inside the folder (e.g. `pyvenv.cfg`) |
| `homePaths` | exact paths under `~` |
| `homeChildrenOf` | each child of a folder counted as its own item (e.g. `~/Library/Caches/*`) |
| `absPaths` | exact absolute paths (e.g. `/private/var/vm`) |
| `absChildrenOf` | each child of an absolute folder (e.g. `/Library/Caches/*`) |
| `fileNames`, `filePatterns`, `fileExts` | individual files |

The outermost match wins, so a `__pycache__` inside a venv counts toward the venv.

```sh
go test ./...
```
