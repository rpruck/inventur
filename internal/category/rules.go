// Package category decides which clutter category a directory or file
// belongs to. All rules live in this file; extend the tables below to teach
// the scanner about new kinds of clutter.
package category

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Category is a kind of clutter. Hint is display text only — the tool never
// acts on it.
type Category struct {
	ID   string
	Name string
	Hint string
}

var (
	NodeModules = &Category{"node_modules", "Node modules", "Regenerable with npm / pnpm / yarn install"}
	PyEnvs      = &Category{"pyenv", "Python environments", "Recreatable from requirements / pyproject"}
	DevCaches   = &Category{"devcache", "Dev tool caches", "Safe to delete; tools recreate them"}
	Build       = &Category{"build", "Build outputs", "Regenerable by rebuilding the project"}
	ML          = &Category{"ml", "ML checkpoints & runs", "Review: keep final models, prune intermediate checkpoints"}
	MLCaches    = &Category{"mlcache", "ML model caches", "Re-downloadable (Hugging Face, Ollama, torch hub, …)"}
	Data        = &Category{"data", "Datasets & data dumps", "Review: may be irreplaceable"}
	PkgCaches   = &Category{"pkgcache", "Package manager caches", "Clear with the package manager's clean command"}
	Xcode       = &Category{"xcode", "Xcode, simulators & Android", "DerivedData/caches are safe; review Archives and simulators"}
	VMs         = &Category{"vm", "Docker & virtual machines", "Prune via docker / VM tooling; disk images may be sparse"}
	Logs        = &Category{"logs", "Log files", "Usually safe to delete; check no process is still writing"}
	Dumps       = &Category{"dumps", "Crash & memory dumps", "Safe to delete once debugged"}
	Installers  = &Category{"installers", "Disk images & installers", "Usually safe once the app is installed"}
	Archives    = &Category{"archives", "Archives", "Review: delete if already extracted"}
	Videos      = &Category{"video", "Videos & recordings", "Personal data — review"}
	Trash       = &Category{"trash", "Trash", "Empty the Trash"}
	IOSBackups  = &Category{"iosbackup", "iOS device backups", "Manage in Finder › device › Manage Backups"}
	AppCaches   = &Category{"appcache", "App caches", "Usually safe; quit the app first, it recreates them"}
	AppData     = &Category{"appdata", "App data", "Review: leftovers of uninstalled apps are common"}
	System      = &Category{"system", "macOS system files", "Managed by macOS: don't delete by hand; a restart frees temp files and swap"}
	Git         = &Category{"git", "Git repositories (.git)", "Informational: try git gc, check LFS objects"}
)

// All lists every category in display order (used to break ties).
var All = []*Category{
	NodeModules, PyEnvs, DevCaches, Build, ML, MLCaches, Data, PkgCaches,
	Xcode, VMs, Logs, Dumps, Installers, Archives, Videos, Trash,
	IOSBackups, AppCaches, AppData, System, Git,
}

// dirNames match a directory by its exact name, anywhere.
var dirNames = map[string]*Category{
	"node_modules": NodeModules,

	"__pycache__":        DevCaches,
	".pytest_cache":      DevCaches,
	".mypy_cache":        DevCaches,
	".ruff_cache":        DevCaches,
	".tox":               DevCaches,
	".nox":               DevCaches,
	".ipynb_checkpoints": DevCaches,
	".hypothesis":        DevCaches,

	".next":         Build,
	".nuxt":         Build,
	".svelte-kit":   Build,
	".turbo":        Build,
	".parcel-cache": Build,
	".angular":      Build,
	".expo":         Build,
	".dart_tool":    Build,
	".terraform":    Build,

	"checkpoints":    ML,
	"wandb":          ML,
	"mlruns":         ML,
	"lightning_logs": ML,
	"ray_results":    ML,
	".neptune":       ML,

	"DiagnosticReports": Dumps,
	".Trashes":          Trash,
	".git":              Git,
}

// dirPatterns match a directory name by regular expression.
var dirPatterns = []struct {
	re  *regexp.Regexp
	cat *Category
}{
	{regexp.MustCompile(`^checkpoint-\d+$`), ML},
	{regexp.MustCompile(`^cmake-build-`), Build},
	{regexp.MustCompile(`\.mlpackage$`), ML},
	{regexp.MustCompile(`\.xcarchive$`), Xcode},
	{regexp.MustCompile(`\.(pvm|utm|vmwarevm)$`), VMs},
	{regexp.MustCompile(`\.sparsebundle$`), Installers},
	{regexp.MustCompile(`^Install macOS .*\.app$`), Installers},
}

// siblingRules match a directory name only if the parent directory contains
// one of the marker files (e.g. "target" next to "Cargo.toml").
var siblingRules = []struct {
	name    string
	markers []string
	cat     *Category
}{
	{"target", []string{"Cargo.toml", "pom.xml", "build.sbt"}, Build},
	{"build", []string{"package.json", "pyproject.toml", "setup.py", "CMakeLists.txt", "build.gradle", "build.gradle.kts"}, Build},
	{"dist", []string{"package.json", "pyproject.toml", "setup.py"}, Build},
	{"out", []string{"package.json", "build.gradle", "build.gradle.kts"}, Build},
	{".build", []string{"Package.swift"}, Build},
	{"Pods", []string{"Podfile"}, Build},
	{".gradle", []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew"}, Build},
}

// contentMarkers categorize a directory by a file it contains.
var contentMarkers = []struct {
	marker string
	cat    *Category
}{
	{"pyvenv.cfg", PyEnvs},
	{"conda-meta", PyEnvs},
	{"CACHEDIR.TAG", DevCaches},
}

// homePaths match exact directories relative to the home directory.
var homePaths = map[string]*Category{
	".cache/huggingface": MLCaches,
	".cache/torch":       MLCaches,
	".ollama/models":     MLCaches,
	".lmstudio":          MLCaches,
	".cache/lm-studio":   MLCaches,
	".keras":             MLCaches,
	".cache/whisper":     MLCaches,

	".npm":                         PkgCaches,
	".pnpm-store":                  PkgCaches,
	"Library/pnpm/store":           PkgCaches,
	".yarn/berry/cache":            PkgCaches,
	"Library/Caches/Yarn":          PkgCaches,
	".bun/install/cache":           PkgCaches,
	".cache/pip":                   PkgCaches,
	"Library/Caches/pip":           PkgCaches,
	".cache/uv":                    PkgCaches,
	".cargo/registry":              PkgCaches,
	"go/pkg/mod":                   PkgCaches,
	"Library/Caches/go-build":      PkgCaches,
	".gradle/caches":               PkgCaches,
	".m2/repository":               PkgCaches,
	"Library/Caches/Homebrew":      PkgCaches,
	"Library/Caches/CocoaPods":     PkgCaches,
	".cocoapods/repos":             PkgCaches,
	"miniconda3/pkgs":              PkgCaches,
	"anaconda3/pkgs":               PkgCaches,
	"miniforge3/pkgs":              PkgCaches,
	".conda/pkgs":                  PkgCaches,
	".cache/pre-commit":            PkgCaches,
	"Library/Caches/ms-playwright": PkgCaches,

	"Library/Developer/Xcode/DerivedData":           Xcode,
	"Library/Developer/Xcode/Archives":              Xcode,
	"Library/Developer/Xcode/iOS DeviceSupport":     Xcode,
	"Library/Developer/Xcode/watchOS DeviceSupport": Xcode,
	"Library/Developer/CoreSimulator/Devices":       Xcode,
	"Library/Developer/CoreSimulator/Caches":        Xcode,
	"Library/Caches/com.apple.dt.Xcode":             Xcode,
	".android/avd":                                  Xcode,
	"Library/Android/sdk/system-images":             Xcode,

	"Library/Containers/com.docker.docker": VMs,
	".docker":                              VMs,
	".orbstack":                            VMs,
	".colima":                              VMs,
	".lima":                                VMs,
	"Library/Containers/com.utmapp.UTM":    VMs,
	"Parallels":                            VMs,
	"Virtual Machines.localized":           VMs,

	"Library/Logs": Logs,
	".Trash":       Trash,

	"Library/Application Support/MobileSync/Backup": IOSBackups,
}

// absPaths match exact absolute directories.
var absPaths = map[string]*Category{
	"/cores":           Dumps,
	"/Library/Logs":    Logs,
	"/private/var/log": Logs,

	"/private/var/folders": System,
	"/private/var/vm":      System,
	"/System/Volumes/VM":   System,
}

// homeChildrenOf makes every immediate child of these directories its own
// instance, unless a more specific rule matches it or lies below it.
var homeChildrenOf = map[string]*Category{
	"Library/Caches":              AppCaches,
	".cache":                      AppCaches,
	"Library/Application Support": AppData,
	"Library/Containers":          AppData,
	"Library/Group Containers":    AppData,
}

// absChildrenOf is homeChildrenOf for absolute directories.
var absChildrenOf = map[string]*Category{
	"/Library/Caches": AppCaches,
}

// fileNames, filePatterns and fileExts categorize individual files.
var fileNames = map[string]*Category{
	"Docker.raw": VMs,
	"nohup.out":  Logs,
}

var filePatterns = []struct {
	re  *regexp.Regexp
	cat *Category
}{
	{regexp.MustCompile(`\.log\.\d+$`), Logs},
	{regexp.MustCompile(`\.log\.(gz|bz2|xz|zst)$`), Logs},
	{regexp.MustCompile(`^pytorch_model.*\.bin$`), ML},
	{regexp.MustCompile(`^events\.out\.tfevents\.`), ML},
	{regexp.MustCompile(`^core\.\d+$`), Dumps},
}

var fileExts = map[string]*Category{
	".log": Logs, ".out": Logs, ".err": Logs,

	".ckpt": ML, ".pt": ML, ".pth": ML, ".safetensors": ML, ".h5": ML, ".hdf5": ML,
	".keras": ML, ".onnx": ML, ".gguf": ML, ".ggml": ML, ".tflite": ML, ".mlmodel": ML, ".pb": ML,

	".parquet": Data, ".arrow": Data, ".feather": Data, ".tfrecord": Data, ".npy": Data, ".npz": Data,
	".sql": Data, ".dump": Data, ".sqlite": Data, ".sqlite3": Data, ".db": Data, ".csv": Data, ".jsonl": Data,

	".vmdk": VMs, ".qcow2": VMs, ".vdi": VMs, ".vhd": VMs, ".vhdx": VMs,

	".hprof": Dumps, ".heapsnapshot": Dumps, ".dmp": Dumps, ".core": Dumps,

	".dmg": Installers, ".iso": Installers, ".pkg": Installers, ".mpkg": Installers, ".img": Installers,
	".xip": Installers, ".ipa": Installers, ".apk": Installers, ".sparseimage": Installers,

	".zip": Archives, ".tar": Archives, ".gz": Archives, ".tgz": Archives, ".bz2": Archives,
	".xz": Archives, ".zst": Archives, ".7z": Archives, ".rar": Archives,

	".mov": Videos, ".mp4": Videos, ".mkv": Videos, ".avi": Videos, ".webm": Videos, ".m4v": Videos,
}

// Classifier holds the path-based rules resolved against a home directory.
type Classifier struct {
	paths      map[string]*Category // absolute dir path -> category
	childrenOf map[string]*Category // absolute dir path -> category for each child
	ruleBelow  map[string]bool      // ancestors of path rules
}

// NewClassifier resolves the path rules against home. HF_HOME, TORCH_HOME
// and OLLAMA_MODELS are honoured if set.
func NewClassifier(home string) *Classifier {
	c := &Classifier{
		paths:      map[string]*Category{},
		childrenOf: map[string]*Category{},
		ruleBelow:  map[string]bool{},
	}
	add := func(p string, cat *Category) {
		p = filepath.Clean(p)
		c.paths[p] = cat
		for d := filepath.Dir(p); d != "/" && d != "."; d = filepath.Dir(d) {
			c.ruleBelow[d] = true
		}
	}
	if home != "" {
		for rel, cat := range homePaths {
			add(filepath.Join(home, rel), cat)
		}
		for rel, cat := range homeChildrenOf {
			c.childrenOf[filepath.Join(home, rel)] = cat
		}
	}
	for p, cat := range absPaths {
		add(p, cat)
	}
	for p, cat := range absChildrenOf {
		c.childrenOf[p] = cat
	}
	for _, env := range []string{"HF_HOME", "TORCH_HOME", "OLLAMA_MODELS"} {
		if v := os.Getenv(env); filepath.IsAbs(v) {
			add(v, MLCaches)
		}
	}
	return c
}

// Dir categorizes a directory from its path, its name and its parent's
// entries (hasSibling reports whether the parent contains a given name).
// It returns nil if no rule matches.
func (c *Classifier) Dir(path, name string, hasSibling func(string) bool) *Category {
	if cat, ok := c.paths[path]; ok {
		return cat
	}
	if cat, ok := dirNames[name]; ok {
		return cat
	}
	for _, p := range dirPatterns {
		if p.re.MatchString(name) {
			return p.cat
		}
	}
	if hasSibling != nil {
		for _, r := range siblingRules {
			if r.name != name {
				continue
			}
			for _, m := range r.markers {
				if hasSibling(m) {
					return r.cat
				}
			}
		}
	}
	if cat, ok := c.childrenOf[filepath.Dir(path)]; ok && !c.ruleBelow[path] {
		return cat
	}
	return nil
}

// DirContents categorizes a directory by marker files it contains.
func (c *Classifier) DirContents(has func(string) bool) *Category {
	for _, m := range contentMarkers {
		if has(m.marker) {
			return m.cat
		}
	}
	return nil
}

// File categorizes a file by its name. It returns nil if no rule matches.
func File(name string) *Category {
	if cat, ok := fileNames[name]; ok {
		return cat
	}
	for _, p := range filePatterns {
		if p.re.MatchString(name) {
			return p.cat
		}
	}
	return fileExts[strings.ToLower(filepath.Ext(name))]
}
