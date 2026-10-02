package category

import "testing"

func TestDir(t *testing.T) {
	const home = "/Users/me"
	c := NewClassifier(home)
	siblings := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, s := range names {
				if s == n {
					return true
				}
			}
			return false
		}
	}
	tests := []struct {
		path string
		sib  func(string) bool
		want *Category
	}{
		{"/x/node_modules", nil, NodeModules},
		{"/x/__pycache__", nil, DevCaches},
		{"/x/checkpoint-1500", nil, ML},
		{"/x/checkpoint-final", nil, nil},
		{"/x/wandb", nil, ML},
		{"/x/target", siblings("Cargo.toml"), Build},
		{"/x/target", siblings("README.md"), nil},
		{"/x/build", siblings("package.json"), Build},
		{"/x/build", nil, nil},
		{"/x/.build", siblings("Package.swift"), Build},
		{"/x/.git", nil, Git},
		{"/x/Install macOS Sequoia.app", nil, Installers},
		{home + "/.cache/huggingface", nil, MLCaches},
		{home + "/Library/Caches/Homebrew", nil, PkgCaches},           // specific beats children-of
		{home + "/Library/Caches/com.spotify.client", nil, AppCaches}, // children-of
		{home + "/Library/Caches", nil, nil},                          // the container itself is not an instance
		{home + "/Library/Application Support/Slack", nil, AppData},
		{home + "/Library/Application Support/MobileSync", nil, nil}, // a rule lies below it
		{home + "/Library/Application Support/MobileSync/Backup", nil, IOSBackups},
		{home + "/Library/Containers/com.docker.docker", nil, VMs},
		{home + "/.Trash", nil, Trash},
		{"/Library/Caches/com.apple.iconservices.store", nil, AppCaches},
		{"/Library/Caches", nil, nil},
		{"/private/var/folders", nil, System},
		{"/private/var/vm", nil, System},
		{"/System/Volumes/VM", nil, System},
		{"/x/plain", nil, nil},
	}
	for _, tt := range tests {
		name := tt.path[len(dirOf(tt.path))+1:]
		if got := c.Dir(tt.path, name, tt.sib); got != tt.want {
			t.Errorf("Dir(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}

func TestDirContents(t *testing.T) {
	c := NewClassifier("/Users/me")
	has := func(name string) func(string) bool { return func(n string) bool { return n == name } }
	if got := c.DirContents(has("pyvenv.cfg")); got != PyEnvs {
		t.Errorf("pyvenv.cfg -> %v", got)
	}
	if got := c.DirContents(has("conda-meta")); got != PyEnvs {
		t.Errorf("conda-meta -> %v", got)
	}
	if got := c.DirContents(has("CACHEDIR.TAG")); got != DevCaches {
		t.Errorf("CACHEDIR.TAG -> %v", got)
	}
	if got := c.DirContents(has("main.go")); got != nil {
		t.Errorf("main.go -> %v", got)
	}
}

func TestFile(t *testing.T) {
	tests := map[string]*Category{
		"server.log":                    Logs,
		"app.log.3":                     Logs,
		"app.log.gz":                    Logs, // log before archive
		"nohup.out":                     Logs,
		"model.safetensors":             ML,
		"pytorch_model-00001.bin":       ML,
		"events.out.tfevents.1700.host": ML,
		"weights.CKPT":                  ML,
		"data.parquet":                  Data,
		"Docker.raw":                    VMs,
		"ubuntu.qcow2":                  VMs,
		"core.12345":                    Dumps,
		"Heap.heapsnapshot":             Dumps,
		"Xcode.xip":                     Installers,
		"backup.tar.gz":                 Archives,
		"screen recording.mov":          Videos,
		"main.go":                       nil,
		"random.bin":                    nil,
	}
	for name, want := range tests {
		if got := File(name); got != want {
			t.Errorf("File(%q) = %v, want %v", name, got, want)
		}
	}
}
