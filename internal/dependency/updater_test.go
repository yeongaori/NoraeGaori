package dependency

import (
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/dependency/mirror"
)

func installableFFmpeg(t *testing.T, version string) *stubMirror {
	t.Helper()

	payload := buildZip(t, map[string]string{
		"bundle/bin/ffmpeg":  script("ffmpeg version N-121234"),
		"bundle/bin/ffprobe": script("ffprobe version N-121234"),
	})
	served := serveArchive(t, payload, 200)
	return &stubMirror{label: "ffmpeg-mirror", found: &mirror.Candidate{
		Version: version,
		URL:     served.server.URL,
		SHA256:  sha256Hex(payload),
		Archive: mirror.ZipArchive,
		Members: []string{"bundle/bin/ffmpeg", "bundle/bin/ffprobe"},
	}}
}

func TestPrepareStopsWhenFFmpegHasNoBuild(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	usePath(t)
	useFFmpegTool(t, noBuildMirror("ffmpeg-github"))
	deno := installableRuntime(t, "deno", "2.9.7")
	useJsRuntimes(t, stubTool("deno", deno))

	err := prepareFFmpeg(mirror.Platform{GOOS: "linux", GOARCH: "386"})
	if err == nil || !strings.Contains(err.Error(), "no download exists for linux/386; install it with the system package manager") {
		t.Errorf("got %v, want the no-build error with an install hint", err)
	}

	if err := Prepare(); err == nil {
		t.Error("Prepare returned nil without ffmpeg, want the bot stopped")
	}
	if deno.calls.Load() != 0 {
		t.Error("the JS runtime was resolved after the fatal ffmpeg error")
	}
}

func TestPrepareStopsWhenTheFFmpegDownloadFails(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	usePath(t)
	useFFmpegTool(t, unreachableMirror("ffmpeg-github"), unreachableMirror("btbn"))

	err := prepareFFmpeg(linuxAmd64)
	if err == nil || !strings.Contains(err.Error(), "the download failed: ffmpeg-github is unreachable; install it with the system package manager or check the network") {
		t.Errorf("got %v, want the download failure reported", err)
	}
}

func TestPrepareUsesAnInstalledFFmpegWhenOffline(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	usePath(t)
	path := writeExecutable(t, filepath.Join(libDirectory, "ffmpeg-2026.09.25.1845", "ffmpeg"), script("ffmpeg version N-121234"))
	unreachable := unreachableMirror("ffmpeg-github")
	useFFmpegTool(t, unreachable)

	if err := prepareFFmpeg(linuxAmd64); err != nil {
		t.Fatalf("prepareFFmpeg returned %v, want the installed build used", err)
	}
	if got := ffmpegSlot.current(); got == nil || !strings.HasSuffix(got.Path, path) {
		t.Errorf("got %+v, want the installed build", got)
	}
	if unreachable.calls.Load() != 0 {
		t.Error("the mirrors were checked although a build is installed")
	}
}

func TestPrepareUsesTheSystemFFmpegAndContinuesWithoutAJsRuntime(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := captureLog(t)
	usePath(t, fakeRuntimes(t, map[string]string{"ffmpeg": "ffmpeg version 7.1.1"}))
	useFFmpegTool(t, unreachableMirror("ffmpeg-github"))
	useJsRuntimes(t, stubTool("deno", noBuildMirror("deno-github")))

	if err := Prepare(); err != nil {
		t.Fatalf("Prepare returned %v, want nil because a missing JS runtime is only a warning", err)
	}
	if got := ffmpegSlot.current(); got == nil || !got.isSystem {
		t.Errorf("got %+v, want the system ffmpeg", got)
	}
	if !strings.Contains(read(), "No JavaScript runtime is available") {
		t.Errorf("got log %q, want the JS runtime warning", read())
	}
}

func TestPrepareDownloadsFFmpegWhenNothingIsInstalled(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	usePath(t)
	useFFmpegTool(t, noBuildMirror("ffmpeg-github"), installableFFmpeg(t, "2026.09.27.1323"))

	if err := prepareFFmpeg(linuxAmd64); err != nil {
		t.Fatalf("prepareFFmpeg returned %v, want the second mirror's build", err)
	}
	got := ffmpegSlot.current()
	if got == nil || got.Version != "2026.09.27.1323" || got.isSystem {
		t.Fatalf("got %+v, want the downloaded build", got)
	}
	if !exists(filepath.Join(filepath.Dir(got.Path), "ffprobe")) {
		t.Error("ffprobe was not installed next to ffmpeg")
	}
}

func TestFFmpegUpdatesWaitAWeek(t *testing.T) {
	cases := map[string]bool{
		"2026.09.18.1845": false,
		"2026.09.25.1745": false,
		"2026.09.25.1844": false,
		"2026.09.25.1845": true,
		"2026.09.25.1945": true,
	}

	for latest, want := range cases {
		if got := isFFmpegUpdateDue("2026.09.18.1845", latest); got != want {
			t.Errorf("isFFmpegUpdateDue(2026.09.18.1845, %s) = %v, want %v", latest, got, want)
		}
	}
	if !isFFmpegUpdateDue("custom", "2026.09.25.1845") || isFFmpegUpdateDue("custom", "custom") {
		t.Error("an unparsable version must update only when it differs")
	}
}

func activateDownloaded(t *testing.T, s *slot, toolName, version string) *Binary {
	t.Helper()

	path := writeExecutable(t, filepath.Join(libDirectory, toolName+"-"+version, toolName), script(toolName+" "+version))
	absolute, _ := filepath.Abs(path)
	binary := &Binary{Tool: toolName, Version: version, Path: absolute}
	s.replace(binary)
	return binary
}

func TestCheckUpdatesReplacesFFmpegOnceAWeekHasPassed(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	old := activateDownloaded(t, &ffmpegSlot, "ffmpeg", "2026.09.18.1845")
	useJsRuntimes(t)

	useFFmpegTool(t, installableFFmpeg(t, "2026.09.25.1844"))
	CheckUpdates()
	if ffmpegSlot.current() != old {
		t.Fatalf("got %+v, want the build kept before a week has passed", ffmpegSlot.current())
	}

	useFFmpegTool(t, installableFFmpeg(t, "2026.09.25.1845"))
	CheckUpdates()
	if got := ffmpegSlot.current(); got.Version != "2026.09.25.1845" {
		t.Fatalf("got %+v, want the week-newer build", got)
	}
	if exists(old.Path) {
		t.Error("the unheld old build was not removed")
	}
}

func TestCheckUpdatesLeavesSystemBinariesAlone(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	ffmpeg := &Binary{Tool: "ffmpeg", Path: "/usr/bin/ffmpeg", isSystem: true}
	node := &Binary{Tool: "node", Path: "/usr/bin/node", isSystem: true}
	ffmpegSlot.replace(ffmpeg)
	jsRuntimeSlot.replace(node)
	ffmpegMirror := installableFFmpeg(t, "2026.09.25.1845")
	denoMirror := installableRuntime(t, "deno", "2.9.7")
	useFFmpegTool(t, ffmpegMirror)
	useJsRuntimes(t, stubTool("deno", denoMirror))

	CheckUpdates()

	if ffmpegSlot.current() != ffmpeg || jsRuntimeSlot.current() != node {
		t.Error("a system binary was replaced")
	}
	if ffmpegMirror.calls.Load() != 0 || denoMirror.calls.Load() != 0 {
		t.Error("the mirrors were checked for system binaries")
	}
}

func TestCheckUpdatesMovesUpToDenoOnceItIsAvailable(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	read := captureLog(t)
	node := activateDownloaded(t, &jsRuntimeSlot, "node", "22.23.3")
	useJsRuntimes(t, stubTool("deno", installableRuntime(t, "deno", "2.9.7")), stubTool("node", installableRuntime(t, "node", "22.23.3")))

	updateJsRuntime(linuxAmd64)
	removeRetired()

	if got := JsRuntimeArg(); !strings.HasPrefix(got, "deno:") {
		t.Errorf("got %q, want the newly available deno", got)
	}
	if exists(node.Path) {
		t.Error("the replaced node was not removed")
	}
	if !strings.Contains(read(), "Using the downloaded deno 2.9.7") {
		t.Errorf("got log %q, want the switch announced", read())
	}
}

func TestCheckUpdatesNeverDropsToALowerPriorityRuntime(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	deno := activateDownloaded(t, &jsRuntimeSlot, "deno", "2.9.7")
	node := installableRuntime(t, "node", "24.14.1")
	useJsRuntimes(t, stubTool("deno", unreachableMirror("deno-github")), stubTool("node", node))

	updateJsRuntime(linuxAmd64)

	if jsRuntimeSlot.current() != deno {
		t.Errorf("got %+v, want deno kept while its mirrors are unreachable", jsRuntimeSlot.current())
	}
	if node.calls.Load() != 0 {
		t.Error("a lower-priority runtime was checked")
	}
}

func TestCheckUpdatesUpgradesTheSameRuntime(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	activateDownloaded(t, &jsRuntimeSlot, "deno", "2.9.6")
	useJsRuntimes(t, stubTool("deno", installableRuntime(t, "deno", "2.9.7")))

	updateJsRuntime(linuxAmd64)
	if got := jsRuntimeSlot.current(); got.Version != "2.9.7" {
		t.Errorf("got %+v, want deno 2.9.7", got)
	}

	current := jsRuntimeSlot.current()
	updateJsRuntime(linuxAmd64)
	if jsRuntimeSlot.current() != current {
		t.Error("an up-to-date runtime was replaced")
	}
}

func TestCheckUpdatesChecksEveryRuntimeForAnUnknownOne(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	activateDownloaded(t, &jsRuntimeSlot, "quickjs", "0.10.0")
	useJsRuntimes(t, stubTool("deno", noBuildMirror("deno-github")), stubTool("node", installableRuntime(t, "node", "22.23.3")))

	updateJsRuntime(linuxAmd64)

	if got := JsRuntimeArg(); !strings.HasPrefix(got, "node:") {
		t.Errorf("got %q, want the whole priority list checked", got)
	}
}

func TestCheckUpdatesFindsARuntimeWhenNoneWasAvailable(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	useJsRuntimes(t, stubTool("deno", installableRuntime(t, "deno", "2.9.7")))

	updateJsRuntime(linuxAmd64)

	if got := JsRuntimeArg(); !strings.HasPrefix(got, "deno:") {
		t.Errorf("got %q, want deno found on a later check", got)
	}
}
