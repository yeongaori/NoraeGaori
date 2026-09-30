package dependency_test

import (
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/dependency"
	"noraegaori/internal/dependency/mirror"
	"noraegaori/tests/testutil/logtest"
)

func installableRuntime(t *testing.T, executable, version string) *stubMirror {
	t.Helper()

	payload := buildZip(t, map[string]string{executable: script(executable + " " + version)})
	served := serveArchive(t, payload, 200)
	return &stubMirror{label: executable + "-mirror", found: &mirror.Candidate{
		Version: version,
		URL:     served.server.URL,
		SHA256:  sha256Hex(payload),
		Archive: mirror.ZipArchive,
		Members: []string{executable},
	}}
}

func TestJsRuntimeArgNamesTheActiveRuntime(t *testing.T) {
	resetState(t)
	if got := dependency.JsRuntimeArg(); got != "" {
		t.Errorf("got %q with no runtime, want empty", got)
	}

	dependency.HookJsRuntimeSlot.HookReplace(dependency.HookBuildBinary(dependency.HookBinaryFields{Tool: "node", Path: "/usr/bin/node", IsSystem: true}))
	if got := dependency.JsRuntimeArg(); got != "node" {
		t.Errorf("got %q, want the bare name for a system runtime", got)
	}

	dependency.HookJsRuntimeSlot.HookReplace(&dependency.Binary{Tool: "deno", Path: "/app/lib/deno-2.9.7/deno"})
	if got := dependency.JsRuntimeArg(); got != "deno:/app/lib/deno-2.9.7/deno" {
		t.Errorf("got %q, want the downloaded path", got)
	}
}

func TestPrepareJsRuntimePrefersTheSystemRuntime(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := logtest.Capture(t)
	deno := installableRuntime(t, "deno", "2.9.7")
	useJsRuntimes(t, stubTool("deno", deno), stubTool("node"))
	*(*dependency.HookJsRuntimes)[1].HookDisplayName() = "Node.js"
	usePath(t, fakeRuntimes(t, map[string]string{"node": "v24.14.1"}))

	dependency.HookPrepareJsRuntime(linuxAmd64)

	if got := dependency.JsRuntimeArg(); got != "node" {
		t.Errorf("got %q, want the system node", got)
	}
	if deno.calls.Load() != 0 {
		t.Error("the mirrors were checked although a system runtime exists")
	}
	if !strings.Contains(read(), "Using the system Node.js 24.14.1 for yt-dlp; install Deno for the best YouTube support") {
		t.Errorf("got log %q, want the system-not-Deno warning", read())
	}
}

func TestPrepareJsRuntimeIsQuietAboutASystemDeno(t *testing.T) {
	resetState(t)
	read := logtest.Capture(t)
	usePath(t, fakeRuntimes(t, map[string]string{"deno": "deno 2.9.7"}))

	dependency.HookPrepareJsRuntime(linuxAmd64)

	if got := dependency.JsRuntimeArg(); got != "deno" {
		t.Errorf("got %q, want the system deno", got)
	}
	if strings.Contains(read(), "WARN") {
		t.Errorf("got log %q, want no warning for Deno", read())
	}
}

func TestPrepareJsRuntimeDownloadsDeno(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := logtest.Capture(t)
	usePath(t)
	useJsRuntimes(t, stubTool("deno", installableRuntime(t, "deno", "2.9.7")))

	dependency.HookPrepareJsRuntime(linuxAmd64)

	want, _ := filepath.Abs(filepath.Join(dependency.HookLibDirectory, "deno-2.9.7", "deno"))
	if got := dependency.JsRuntimeArg(); got != "deno:"+want {
		t.Errorf("got %q, want the downloaded deno", got)
	}
	if strings.Contains(read(), "WARN") {
		t.Errorf("got log %q, want no warning for a downloaded Deno", read())
	}
}

func TestPrepareJsRuntimeWarnsWhenDenoHasNoBuild(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := logtest.Capture(t)
	usePath(t)
	useJsRuntimes(t, dependency.HookBuildTool(dependency.HookToolFields{Name: "deno", DisplayName: "Deno", Mirrors: []mirror.Mirror{noBuildMirror("deno-github")}}),
		dependency.HookBuildTool(dependency.HookToolFields{Name: "node", DisplayName: "Node.js", VersionFlag: "--version", Mirrors: []mirror.Mirror{installableRuntime(t, "node.exe", "22.23.3")}}))
	windows386 := mirror.Platform{GOOS: "windows", GOARCH: "386"}

	dependency.HookPrepareJsRuntime(windows386)

	if got := dependency.JsRuntimeArg(); !strings.HasPrefix(got, "node:") {
		t.Errorf("got %q, want the downloaded node", got)
	}
	log := read()
	if !strings.Contains(log, "Deno has no build for windows/386, so the downloaded Node.js 22.23.3 is used for yt-dlp instead") {
		t.Errorf("got log %q, want the no-build warning", log)
	}
	if strings.Contains(log, "tried again") {
		t.Errorf("got log %q, want no retry promise when Deno has no build", log)
	}
}

func TestPrepareJsRuntimeWarnsWhenDenoIsUnreachable(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := logtest.Capture(t)
	usePath(t)
	useJsRuntimes(t, dependency.HookBuildTool(dependency.HookToolFields{Name: "deno", DisplayName: "Deno", Mirrors: []mirror.Mirror{unreachableMirror("deno-github")}}),
		dependency.HookBuildTool(dependency.HookToolFields{Name: "node", DisplayName: "Node.js", VersionFlag: "--version", Mirrors: []mirror.Mirror{installableRuntime(t, "node", "24.14.1")}}))

	dependency.HookPrepareJsRuntime(linuxAmd64)

	log := read()
	if !strings.Contains(log, "Deno mirrors are unreachable (deno-github is unreachable), so the downloaded Node.js 24.14.1 is used for yt-dlp instead; Deno will be tried again on the next update check") {
		t.Errorf("got log %q, want the unreachable warning", log)
	}
}

func TestPrepareJsRuntimeUsesAnInstalledRuntimeWhenOffline(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	usePath(t)
	writeExecutable(t, filepath.Join(dependency.HookLibDirectory, "node-22.23.3", "node"), script("v22.23.3"))
	useJsRuntimes(t, stubTool("deno", unreachableMirror("deno-github")), stubTool("node", unreachableMirror("nodejs")))

	dependency.HookPrepareJsRuntime(linuxAmd64)

	if got := dependency.JsRuntimeArg(); !strings.HasSuffix(got, filepath.Join("node-22.23.3", "node")) {
		t.Errorf("got %q, want the installed node", got)
	}
}

func TestPrepareJsRuntimeWarnsWhenNothingIsAvailable(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := logtest.Capture(t)
	usePath(t)
	useJsRuntimes(t, stubTool("deno", noBuildMirror("deno-github")), stubTool("node", noBuildMirror("nodejs")))

	dependency.HookPrepareJsRuntime(mirror.Platform{GOOS: "linux", GOARCH: "386"})

	if got := dependency.JsRuntimeArg(); got != "" {
		t.Errorf("got %q, want no runtime", got)
	}
	if !strings.Contains(read(), "No JavaScript runtime is available for linux/386; install Deno") {
		t.Errorf("got log %q, want the nothing-available warning", read())
	}
}
