package dependency_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"noraegaori/internal/dependency"
	"noraegaori/tests/testutil"
)

func installedFFmpeg(t *testing.T, version string) *dependency.Binary {
	t.Helper()

	path := writeExecutable(t, filepath.Join(dependency.HookLibDirectory, "ffmpeg-"+version, "ffmpeg"), script("ffmpeg"))
	return &dependency.Binary{Tool: "ffmpeg", Version: version, Path: path}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestReplacingFFmpegKeepsAHeldBuildUntilItIsReleased(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	old := installedFFmpeg(t, "2026.09.18.1200")
	next := installedFFmpeg(t, "2026.09.25.1845")
	dependency.HookFfmpegSlot.HookReplace(old)

	held := dependency.AcquireFFmpeg()
	if held != old {
		t.Fatalf("got %+v, want the active build", held)
	}

	dependency.HookFfmpegSlot.HookReplace(next)
	dependency.HookRemoveRetired()
	if !exists(old.Path) {
		t.Fatal("a build still held by a stream was removed")
	}
	if dependency.AcquireFFmpeg() != next {
		t.Error("new streams did not get the new build")
	}

	held.Release()
	if exists(old.Path) {
		t.Error("the retired build was kept after its last holder released it")
	}
	if !exists(next.Path) {
		t.Error("the active build was removed")
	}
}

func TestRemoveRetiredRetriesAFailedRemoval(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	old := installedFFmpeg(t, "2026.09.18.1200")
	dependency.HookFfmpegSlot.HookReplace(old)
	dependency.HookFfmpegSlot.HookReplace(installedFFmpeg(t, "2026.09.25.1845"))

	testutil.Swap(t, dependency.HookRemoveDirectory, func(string) error { return errors.New("the file is in use") })
	dependency.HookRemoveRetired()
	if len(*dependency.HookRetiredBinaries) != 1 {
		t.Fatalf("got %d retired builds, want the failed removal kept for a retry", len(*dependency.HookRetiredBinaries))
	}

	*dependency.HookRemoveDirectory = os.RemoveAll
	dependency.HookRemoveRetired()
	if exists(old.Path) || len(*dependency.HookRetiredBinaries) != 0 {
		t.Error("the retry did not remove the retired build")
	}
}

func TestReplaceNeverRetiresSystemBinariesOrTheSamePath(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	system := dependency.HookBuildBinary(dependency.HookBinaryFields{Tool: "ffmpeg", Path: "/usr/bin/ffmpeg", IsSystem: true})
	dependency.HookFfmpegSlot.HookReplace(system)
	dependency.HookFfmpegSlot.HookReplace(installedFFmpeg(t, "2026.09.25.1845"))

	same := &dependency.Binary{Tool: "ffmpeg", Version: "2026.09.25.1845", Path: dependency.HookFfmpegSlot.HookCurrent().Path}
	dependency.HookFfmpegSlot.HookReplace(same)

	if len(*dependency.HookRetiredBinaries) != 0 {
		t.Errorf("got %d retired binaries, want none", len(*dependency.HookRetiredBinaries))
	}
}

func TestRemoveRetiredKeepsABuildThatBecameActiveAgain(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	testutil.Swap(t, dependency.HookRemoveDirectory, func(string) error { return errors.New("the file is in use") })
	old := installedFFmpeg(t, "2026.09.18.1200")
	dependency.HookFfmpegSlot.HookReplace(old)
	dependency.HookFfmpegSlot.HookReplace(installedFFmpeg(t, "2026.09.25.1845"))
	dependency.HookRemoveRetired()

	*dependency.HookRemoveDirectory = os.RemoveAll
	dependency.HookFfmpegSlot.HookReplace(&dependency.Binary{Tool: "ffmpeg", Version: old.Version, Path: old.Path})
	dependency.HookRemoveRetired()

	if !exists(old.Path) {
		t.Error("the reactivated build was removed")
	}
	for _, binary := range *dependency.HookRetiredBinaries {
		if binary.Path == old.Path {
			t.Error("the reactivated build is still queued for removal")
		}
	}
}

func TestAcquireFFmpegFallsBackToThePath(t *testing.T) {
	resetState(t)

	binary := dependency.AcquireFFmpeg()
	defer binary.Release()
	if binary.Path != "ffmpeg" || !*binary.HookIsSystem() {
		t.Errorf("got %+v, want ffmpeg from PATH before anything is prepared", binary)
	}
}

func TestFFmpegLocationArgsPointAtTheDownloadedBuild(t *testing.T) {
	downloaded := &dependency.Binary{Tool: "ffmpeg", Path: filepath.Join("/app", "lib", "ffmpeg-2026.09.25.1845", "ffmpeg")}

	args := dependency.FFmpegLocationArgs(downloaded)
	if len(args) != 2 || args[0] != "--ffmpeg-location" || args[1] != filepath.Join("/app", "lib", "ffmpeg-2026.09.25.1845") {
		t.Errorf("got %v, want --ffmpeg-location with the build directory", args)
	}
	if dependency.FFmpegLocationArgs(*dependency.HookPathFFmpeg) != nil || dependency.FFmpegLocationArgs(nil) != nil {
		t.Error("got arguments for a system ffmpeg, want none so yt-dlp uses PATH")
	}
}
