package dependency

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"noraegaori/tests/testutil"
)

func installedFFmpeg(t *testing.T, version string) *Binary {
	t.Helper()

	path := writeExecutable(t, filepath.Join(libDirectory, "ffmpeg-"+version, "ffmpeg"), script("ffmpeg"))
	return &Binary{Tool: "ffmpeg", Version: version, Path: path}
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
	ffmpegSlot.replace(old)

	held := AcquireFFmpeg()
	if held != old {
		t.Fatalf("got %+v, want the active build", held)
	}

	ffmpegSlot.replace(next)
	removeRetired()
	if !exists(old.Path) {
		t.Fatal("a build still held by a stream was removed")
	}
	if AcquireFFmpeg() != next {
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
	ffmpegSlot.replace(old)
	ffmpegSlot.replace(installedFFmpeg(t, "2026.09.25.1845"))

	testutil.Swap(t, &removeDirectory, func(string) error { return errors.New("the file is in use") })
	removeRetired()
	if len(retiredBinaries) != 1 {
		t.Fatalf("got %d retired builds, want the failed removal kept for a retry", len(retiredBinaries))
	}

	removeDirectory = os.RemoveAll
	removeRetired()
	if exists(old.Path) || len(retiredBinaries) != 0 {
		t.Error("the retry did not remove the retired build")
	}
}

func TestReplaceNeverRetiresSystemBinariesOrTheSamePath(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	system := &Binary{Tool: "ffmpeg", Path: "/usr/bin/ffmpeg", isSystem: true}
	ffmpegSlot.replace(system)
	ffmpegSlot.replace(installedFFmpeg(t, "2026.09.25.1845"))

	same := &Binary{Tool: "ffmpeg", Version: "2026.09.25.1845", Path: ffmpegSlot.current().Path}
	ffmpegSlot.replace(same)

	if len(retiredBinaries) != 0 {
		t.Errorf("got %d retired binaries, want none", len(retiredBinaries))
	}
}

func TestRemoveRetiredKeepsABuildThatBecameActiveAgain(t *testing.T) {
	useWorkingDirectory(t)
	resetState(t)
	testutil.Swap(t, &removeDirectory, func(string) error { return errors.New("the file is in use") })
	old := installedFFmpeg(t, "2026.09.18.1200")
	ffmpegSlot.replace(old)
	ffmpegSlot.replace(installedFFmpeg(t, "2026.09.25.1845"))
	removeRetired()

	removeDirectory = os.RemoveAll
	ffmpegSlot.replace(&Binary{Tool: "ffmpeg", Version: old.Version, Path: old.Path})
	removeRetired()

	if !exists(old.Path) {
		t.Error("the reactivated build was removed")
	}
	for _, binary := range retiredBinaries {
		if binary.Path == old.Path {
			t.Error("the reactivated build is still queued for removal")
		}
	}
}

func TestAcquireFFmpegFallsBackToThePath(t *testing.T) {
	resetState(t)

	binary := AcquireFFmpeg()
	defer binary.Release()
	if binary.Path != "ffmpeg" || !binary.isSystem {
		t.Errorf("got %+v, want ffmpeg from PATH before anything is prepared", binary)
	}
}

func TestFFmpegLocationArgsPointAtTheDownloadedBuild(t *testing.T) {
	downloaded := &Binary{Tool: "ffmpeg", Path: filepath.Join("/app", "lib", "ffmpeg-2026.09.25.1845", "ffmpeg")}

	args := FFmpegLocationArgs(downloaded)
	if len(args) != 2 || args[0] != "--ffmpeg-location" || args[1] != filepath.Join("/app", "lib", "ffmpeg-2026.09.25.1845") {
		t.Errorf("got %v, want --ffmpeg-location with the build directory", args)
	}
	if FFmpegLocationArgs(pathFFmpeg) != nil || FFmpegLocationArgs(nil) != nil {
		t.Error("got arguments for a system ffmpeg, want none so yt-dlp uses PATH")
	}
}
