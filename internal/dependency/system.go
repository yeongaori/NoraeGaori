package dependency

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"noraegaori/internal/logger"
)

func findSystemTool(t *tool) (*Binary, bool) {
	binaryPath, err := exec.LookPath(t.name)
	if err != nil {
		return nil, false
	}

	version, err := readVersion(binaryPath, t.versionFlag)
	if err != nil {
		logger.Debugf("Skipping the system %s at %s: %v", t.displayName, binaryPath, err)
		return nil, false
	}
	if !t.accepts(version) {
		logger.Debugf("Skipping the system %s %s at %s: yt-dlp does not support this version", t.displayName, version, binaryPath)
		return nil, false
	}

	return &Binary{Tool: t.name, Version: version, Path: binaryPath, isSystem: true}, true
}

func findSystemJsRuntime() (*Binary, bool) {
	addNvmNodeToPath()

	for _, t := range jsRuntimes {
		if binary, ok := findSystemTool(t); ok {
			return binary, true
		}
	}
	return nil, false
}

func findSystemFFmpeg() (*Binary, bool) {
	binaryPath, err := exec.LookPath(ffmpegTool.name)
	if err != nil {
		return nil, false
	}
	if _, err := runVersionCommand(binaryPath, ffmpegTool.versionFlag); err != nil {
		logger.Warnf("Skipping the system ffmpeg at %s: %v", binaryPath, err)
		return nil, false
	}
	return &Binary{Tool: ffmpegTool.name, Path: binaryPath, isSystem: true}, true
}

func addNvmNodeToPath() {
	if _, err := exec.LookPath("node"); err == nil {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"))
	if len(matches) == 0 {
		return
	}

	newest := slices.MaxFunc(matches, func(a, b string) int {
		return compareVersions(filepath.Base(filepath.Dir(filepath.Dir(a))), filepath.Base(filepath.Dir(filepath.Dir(b))))
	})
	nodeDirectory := filepath.Dir(newest)
	if err := os.Setenv("PATH", nodeDirectory+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		logger.Warnf("Failed to add %s to PATH: %v", nodeDirectory, err)
	}
}
