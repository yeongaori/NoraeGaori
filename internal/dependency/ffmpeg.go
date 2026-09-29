package dependency

import (
	"fmt"
	"path/filepath"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/logger"
)

var ffmpegTool = &tool{
	name:        "ffmpeg",
	displayName: "ffmpeg",
	versionFlag: "-version",
	mirrors: []mirror.Mirror{
		mirror.FFmpegGitHub{APIURL: mirror.GitHubAPI, Repo: "yt-dlp/FFmpeg-Builds"},
		mirror.FFmpegGitHub{APIURL: mirror.GitHubAPI, Repo: "BtbN/FFmpeg-Builds"},
	},
}

var pathFFmpeg = &Binary{Tool: "ffmpeg", Path: "ffmpeg", isSystem: true}

func AcquireFFmpeg() *Binary {
	for {
		binary := ffmpegSlot.current()
		if binary == nil {
			return pathFFmpeg.acquire()
		}

		binary.acquire()
		if !binary.retired.Load() || ffmpegSlot.current() == binary {
			return binary
		}
		binary.Release()
	}
}

func FFmpegLocationArgs(binary *Binary) []string {
	if binary == nil || binary.isSystem {
		return nil
	}
	return []string{"--ffmpeg-location", filepath.Dir(binary.Path)}
}

func prepareFFmpeg(target mirror.Platform) error {
	if binary, ok := findSystemFFmpeg(); ok {
		ffmpegSlot.replace(binary)
		logger.Infof("Using the system ffmpeg at %s", binary.Path)
		return nil
	}

	if binary, ok := newestInstalled(ffmpegTool, target); ok {
		ffmpegSlot.replace(binary)
		logger.Infof("Using the downloaded ffmpeg build %s", binary.Version)
		return nil
	}

	binary, results := discover([]*tool{ffmpegTool}, target, installFunc(target))
	if binary != nil {
		ffmpegSlot.replace(binary)
		logger.Infof("Downloaded ffmpeg build %s", binary.Version)
		return nil
	}

	if results[0].outcome == outcomeNoBuild {
		return fmt.Errorf("ffmpeg is required but not found, and no download exists for %s; install it with the system package manager (for example: sudo apt install ffmpeg)", target)
	}
	return fmt.Errorf("ffmpeg is required but not found, and the download failed: %v; install it with the system package manager or check the network", results[0].err)
}
