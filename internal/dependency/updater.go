package dependency

import (
	"time"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/logger"
)

const ffmpegMinimumUpdateGap = 7 * 24 * time.Hour

func Prepare() error {
	target := currentPlatform()
	if err := prepareFFmpeg(target); err != nil {
		return err
	}
	prepareJsRuntime(target)
	return nil
}

func CheckUpdates() {
	removeRetired()
	target := currentPlatform()
	updateFFmpeg(target)
	updateJsRuntime(target)
}

func isFFmpegUpdateDue(installed, latest string) bool {
	installedAt, installedErr := time.Parse(mirror.FFmpegVersionLayout, installed)
	latestAt, latestErr := time.Parse(mirror.FFmpegVersionLayout, latest)
	if installedErr != nil || latestErr != nil {
		return installed != latest
	}
	return latestAt.Sub(installedAt) >= ffmpegMinimumUpdateGap
}

func updateFFmpeg(target mirror.Platform) {
	current := ffmpegSlot.current()
	if current == nil || current.isSystem {
		return
	}

	accept := func(t *tool, found *mirror.Candidate) (*Binary, error) {
		if !isFFmpegUpdateDue(current.Version, found.Version) {
			return current, nil
		}
		return install(t, target, found)
	}

	binary, _ := discover([]*tool{ffmpegTool}, target, accept)
	if binary == nil || binary.Path == current.Path {
		return
	}

	ffmpegSlot.replace(binary)
	removeRetired()
	logger.Infof("Updated ffmpeg from build %s to %s; each server switches at its next playback gap", current.Version, binary.Version)
}

func updateJsRuntime(target mirror.Platform) {
	current := jsRuntimeSlot.current()
	if current != nil && current.isSystem {
		return
	}

	candidates := jsRuntimes
	if current != nil {
		_, priority := findJsRuntime(current.Tool)
		candidates = jsRuntimes[:min(priority+1, len(jsRuntimes))]
	}

	binary, results := discover(candidates, target, installFunc(target))
	if binary == nil || (current != nil && binary.Path == current.Path) {
		return
	}

	jsRuntimeSlot.replace(binary)
	announceDownloadedJsRuntime(binary, results, target)
}
