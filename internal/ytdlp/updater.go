package ytdlp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"noraegaori/pkg/logger"
)

const (
	githubAPIURL      = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"
	updateCheckFile   = "data/.ytdlp_last_check"
	updateCheckPeriod = 7 * 24 * time.Hour // Check weekly
)

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// GetBinaryPath returns the platform-specific yt-dlp binary path
func GetBinaryPath() string {
	binaryName := "yt-dlp"
	if runtime.GOOS == "windows" {
		binaryName = "yt-dlp.exe"
	}
	return filepath.Join("lib", binaryName)
}

// GetCurrentVersion returns the currently installed yt-dlp version
func GetCurrentVersion() (string, error) {
	binaryPath := GetBinaryPath()
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("binary does not exist")
	}

	cmd := exec.Command(binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetLatestRelease fetches the latest release information from GitHub
func GetLatestRelease() (*GitHubRelease, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", githubAPIURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "yt-dlp-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	return &release, nil
}

// GetDownloadURL determines the appropriate download URL based on platform and architecture
func GetDownloadURL(release *GitHubRelease) (string, error) {
	var assetName string

	switch runtime.GOOS {
	case "windows":
		assetName = "yt-dlp.exe"
	case "darwin":
		assetName = "yt-dlp_macos"
	case "linux":
		switch runtime.GOARCH {
		case "arm64", "aarch64":
			assetName = "yt-dlp_linux_aarch64"
		case "arm":
			return "", fmt.Errorf("Linux ARMv7l is not directly supported")
		default:
			assetName = "yt-dlp"
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			sizeMB := float64(asset.Size) / 1024 / 1024
			logger.Debugf("[yt-dlp] Found asset: %s (%.2f MB)", asset.Name, sizeMB)
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("asset not found: %s", assetName)
}

// DownloadFile downloads a file from the specified URL to the destination
func DownloadFile(url, destination string) error {
	logger.Debugf("[yt-dlp] Starting download from: %s", url)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status: %d", resp.StatusCode)
	}

	// Create destination file
	out, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Download with progress logging
	totalSize := resp.ContentLength
	downloaded := int64(0)
	lastProgress := 0

	buffer := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := out.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to file: %w", writeErr)
			}
			downloaded += int64(n)

			if totalSize > 0 {
				progress := int((downloaded * 100) / totalSize)
				if progress >= lastProgress+10 {
					logger.Debugf("[yt-dlp] Download progress: %d%%", progress)
					lastProgress = progress
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("download interrupted: %w", err)
		}
	}

	logger.Debugf("[yt-dlp] Download completed")
	return nil
}

// UpdateYtDlp automatically updates yt-dlp
func UpdateYtDlp(force bool) (bool, error) {
	logger.Debugf("[yt-dlp] Update Process Started")
	logger.Info("[yt-dlp] Checking for updates...")

	// Check current version
	currentVersion, err := GetCurrentVersion()
	if err != nil {
		logger.Debugf("[yt-dlp] No version currently installed: %v", err)
		currentVersion = ""
	}

	// Fetch latest release info
	release, err := GetLatestRelease()
	if err != nil {
		return false, fmt.Errorf("failed to fetch release info: %w", err)
	}

	latestVersion := release.TagName

	// Compare versions (if not forcing)
	if !force && currentVersion == latestVersion {
		logger.Infof("[yt-dlp] Already up to date (%s)", currentVersion)
		return false, nil
	}

	if currentVersion != "" && !force {
		logger.Infof("[yt-dlp] Update available: %s → %s", currentVersion, latestVersion)
	} else if force {
		logger.Infof("[yt-dlp] Force updating to %s", latestVersion)
	} else {
		logger.Infof("[yt-dlp] Installing version %s", latestVersion)
	}

	// Determine download URL
	downloadURL, err := GetDownloadURL(release)
	if err != nil {
		return false, err
	}

	binaryPath := GetBinaryPath()
	backupPath := binaryPath + ".backup"

	// Create lib directory if it doesn't exist
	libDir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create lib directory: %w", err)
	}

	// Backup existing file if present
	if _, err := os.Stat(binaryPath); err == nil {
		logger.Debugf("[yt-dlp] Creating backup of existing binary...")
		if err := copyFile(binaryPath, backupPath); err != nil {
			logger.Warnf("[yt-dlp] Failed to create backup: %v", err)
		} else {
			logger.Debugf("[yt-dlp] Backup created")
		}
	}

	// Download new version
	logger.Info("[yt-dlp] Downloading new version...")
	if err := DownloadFile(downloadURL, binaryPath); err != nil {
		// Restore from backup on failure
		if _, statErr := os.Stat(backupPath); statErr == nil {
			logger.Warn("[yt-dlp] Restoring from backup...")
			copyFile(backupPath, binaryPath)
			os.Remove(backupPath)
		}
		return false, err
	}

	// Set executable permissions (Unix-like systems)
	if runtime.GOOS != "windows" {
		logger.Debugf("[yt-dlp] Setting executable permissions...")
		if err := os.Chmod(binaryPath, 0755); err != nil {
			logger.Warnf("[yt-dlp] Failed to set permissions: %v", err)
		}
	}

	// Verify installation
	logger.Debugf("[yt-dlp] Verifying installation...")
	newVersion, err := GetCurrentVersion()
	if err != nil {
		// Restore from backup on verification failure
		if _, statErr := os.Stat(backupPath); statErr == nil {
			logger.Warn("[yt-dlp] Verification failed, restoring from backup...")
			copyFile(backupPath, binaryPath)
			os.Remove(backupPath)
		}
		return false, fmt.Errorf("failed to verify version after update: %w", err)
	}

	logger.Infof("[yt-dlp] Update complete! Version: %s", newVersion)

	// Remove backup file
	if _, err := os.Stat(backupPath); err == nil {
		os.Remove(backupPath)
	}

	logger.Debugf("[yt-dlp] ===== Update Process Completed Successfully =====")
	return true, nil
}

// ShouldCheckForUpdate checks if enough time has passed since last check
func ShouldCheckForUpdate() bool {
	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
		return true // Check anyway if we can't track
	}

	info, err := os.Stat(updateCheckFile)
	if os.IsNotExist(err) {
		return true // No check file, should check
	}
	if err != nil {
		return true // Check anyway on error
	}

	timeSinceLastCheck := time.Since(info.ModTime())
	return timeSinceLastCheck >= updateCheckPeriod
}

// UpdateLastCheckTime updates the timestamp of the last update check
func UpdateLastCheckTime() error {
	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
		return err
	}

	// Touch the file to update its modification time
	file, err := os.OpenFile(updateCheckFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	now := time.Now()
	return os.Chtimes(updateCheckFile, now, now)
}

// AutoUpdate performs an automatic update check on startup (always checks)
func AutoUpdate() {
	updated, err := UpdateYtDlp(false)
	if err != nil {
		logger.Errorf("[yt-dlp] Auto-update check failed: %v", err)
		logger.Warn("[yt-dlp] Continuing with existing version")
	} else if updated {
		logger.Info("[yt-dlp] Auto-update completed successfully")
	}

	// Update check timestamp
	if err := UpdateLastCheckTime(); err != nil {
		logger.Debugf("[yt-dlp] Failed to update check timestamp: %v", err)
	}
}

var jsRuntime string

// DetectJsRuntime checks for node (incl. nvm) → deno → bun, logs warnings
func DetectJsRuntime() {
	if _, err := exec.LookPath("node"); err == nil {
		jsRuntime = "node"
		return
	}

	if tryNvm() {
		jsRuntime = "node"
		return
	}

	for _, rt := range []string{"deno", "bun"} {
		if _, err := exec.LookPath(rt); err == nil {
			logger.Warnf("[yt-dlp] Node.js not found, using %s", rt)
			jsRuntime = rt
			return
		}
	}

	logger.Warn("[yt-dlp] Node.js not found, using no JS runtime")
}

// tryNvm finds node under ~/.nvm and prepends its bin dir to PATH
func tryNvm() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"))
	if err != nil || len(matches) == 0 {
		return false
	}
	sort.Strings(matches)
	nodeBin := filepath.Dir(matches[len(matches)-1])
	os.Setenv("PATH", nodeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return true
}

// GetJsRuntime returns the detected runtime ("node", "deno", "bun", or "")
func GetJsRuntime() string {
	return jsRuntime
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}
