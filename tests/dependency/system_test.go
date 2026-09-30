package dependency_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/dependency"
)

func fakeRuntimes(t *testing.T, versions map[string]string) string {
	t.Helper()

	directory := t.TempDir()
	for name, output := range versions {
		writeExecutable(t, filepath.Join(directory, name), script(output))
	}
	return directory
}

func TestSystemJsRuntimesFollowThePriority(t *testing.T) {
	cases := []struct {
		name     string
		runtimes map[string]string
		want     string
	}{
		{"deno first", map[string]string{"deno": "deno 2.9.7", "node": "v24.14.1", "bun": "1.3.14"}, "deno"},
		{"node before bun", map[string]string{"node": "v24.14.1", "bun": "1.3.14"}, "node"},
		{"bun last", map[string]string{"bun": "1.3.14"}, "bun"},
		{"old deno skipped", map[string]string{"deno": "deno 2.2.12", "node": "v22.0.0"}, "node"},
		{"old node skipped", map[string]string{"node": "v18.20.4", "bun": "1.2.11"}, "bun"},
		{"new bun skipped", map[string]string{"bun": "1.4.2"}, ""},
		{"silent runtime skipped", map[string]string{"deno": "nothing", "node": "v22.23.3"}, "node"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			usePath(t, fakeRuntimes(t, c.runtimes))

			binary, ok := dependency.HookFindSystemJsRuntime()
			if c.want == "" {
				if ok {
					t.Errorf("got %+v, want no usable runtime", binary)
				}
				return
			}
			if !ok || binary.Tool != c.want || !*binary.HookIsSystem() {
				t.Errorf("got %+v, %v, want the system %s", binary, ok, c.want)
			}
		})
	}
}

func TestSystemJsRuntimeReportsItsVersionAndPath(t *testing.T) {
	directory := fakeRuntimes(t, map[string]string{"node": "v22.23.3"})
	usePath(t, directory)

	binary, ok := dependency.HookFindSystemJsRuntime()
	if !ok || binary.Version != "22.23.3" || binary.Path != filepath.Join(directory, "node") {
		t.Errorf("got %+v, want node 22.23.3 at %s", binary, directory)
	}
}

func TestNvmNodeIsAddedToThePathWhenNodeIsMissing(t *testing.T) {
	usePath(t)
	home := os.Getenv("HOME")
	for _, version := range []string{"v9.11.2", "v24.14.1", "v22.23.3"} {
		writeExecutable(t, filepath.Join(home, ".nvm", "versions", "node", version, "bin", "node"), script(version))
	}

	binary, ok := dependency.HookFindSystemJsRuntime()
	if !ok || binary.Version != "24.14.1" {
		t.Errorf("got %+v, %v, want the newest nvm node, not the lexically largest", binary, ok)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), filepath.Join(home, ".nvm", "versions", "node", "v24.14.1", "bin")) {
		t.Errorf("got PATH %q, want the nvm node directory first", os.Getenv("PATH"))
	}
}

func TestNvmIsIgnoredWhenNodeIsAlreadyOnThePath(t *testing.T) {
	usePath(t, fakeRuntimes(t, map[string]string{"node": "v22.23.3"}))
	path := os.Getenv("PATH")
	writeExecutable(t, filepath.Join(os.Getenv("HOME"), ".nvm", "versions", "node", "v24.14.1", "bin", "node"), script("v24.14.1"))

	dependency.HookAddNvmNodeToPath()
	if os.Getenv("PATH") != path {
		t.Errorf("got PATH %q, want it untouched", os.Getenv("PATH"))
	}
}

func TestNvmIsSkippedWithoutAHomeDirectory(t *testing.T) {
	usePath(t)
	t.Setenv("HOME", "")
	path := os.Getenv("PATH")

	dependency.HookAddNvmNodeToPath()
	if os.Getenv("PATH") != path {
		t.Errorf("got PATH %q, want it untouched", os.Getenv("PATH"))
	}
}

func TestSystemFFmpegMustRun(t *testing.T) {
	usePath(t, fakeRuntimes(t, map[string]string{"ffmpeg": "ffmpeg version N-121234-g0123456"}))
	binary, ok := dependency.HookFindSystemFFmpeg()
	if !ok || !*binary.HookIsSystem() || filepath.Base(binary.Path) != "ffmpeg" {
		t.Errorf("got %+v, %v, want the system ffmpeg even without a dotted version", binary, ok)
	}

	directory := t.TempDir()
	writeExecutable(t, filepath.Join(directory, "ffmpeg"), failingScript)
	usePath(t, directory)
	if binary, ok := dependency.HookFindSystemFFmpeg(); ok {
		t.Errorf("got %+v, want a broken system ffmpeg skipped", binary)
	}

	usePath(t)
	if _, ok := dependency.HookFindSystemFFmpeg(); ok {
		t.Error("got ok with no ffmpeg on PATH")
	}
}
