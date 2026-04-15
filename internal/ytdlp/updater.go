package ytdlp

import (
	"context"
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
	githubAPIURL        = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"
	updateCheckInterval = 6 * time.Hour
	minCheckInterval    = 1 * time.Hour
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

// GetLegacyBinaryPath returns the old-style platform-specific yt-dlp binary path
func GetLegacyBinaryPath() string {
	binaryName := "yt-dlp"
	if runtime.GOOS == "windows" {
		binaryName = "yt-dlp.exe"
	}
	return filepath.Join("lib", binaryName)
}

// GetBinaryPath returns the path to the yt-dlp binary.
// If the VersionManager is initialized, delegates to it.
// Otherwise falls back to the legacy flat path.
func GetBinaryPath() string {
	if versionmanager := GetVersionManager(); versionmanager != nil {
		return versionmanager.ActiveBinaryPath()
	}
	return GetLegacyBinaryPath()
}

// VersionedBinaryPath returns the path for a specific yt-dlp version
func VersionedBinaryPath(version string) string {
	binaryName := "yt-dlp"
	if runtime.GOOS == "windows" {
		binaryName = "yt-dlp.exe"
	}
	return filepath.Join("lib", fmt.Sprintf("yt-dlp-%s", version), binaryName)
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

	// Throttle to 256 KB/s to avoid interfering with audio streaming
	const downloadRateLimit = 256 * 1024
	buffer := make([]byte, 16*1024)
	chunkDelay := time.Duration(float64(len(buffer)) / float64(downloadRateLimit) * float64(time.Second))

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

			time.Sleep(chunkDelay)
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

// UpdateYtDlp downloads a new yt-dlp version to a versioned directory.
// If VersionManager is active, registers the version and runs canary.
// Returns (updated bool, error).
func UpdateYtDlp(force bool) (bool, error) {
	logger.Debugf("[yt-dlp] Update Process Started")
	logger.Info("[yt-dlp] Checking for updates...")

	versionmanager := GetVersionManager()

	// Check current version
	var currentVersion string
	if versionmanager != nil {
		currentVersion = versionmanager.GetActiveVersion()
	}
	if currentVersion == "" {
		ver, err := GetCurrentVersion()
		if err != nil {
			logger.Debugf("[yt-dlp] No version currently installed: %v", err)
		} else {
			currentVersion = ver
		}
	}

	// Fetch latest release info
	release, err := GetLatestRelease()
	if err != nil {
		return false, fmt.Errorf("failed to fetch release info: %w", err)
	}

	latestVersion := release.TagName

	// Compare versions
	if !force && currentVersion == latestVersion {
		logger.Infof("[yt-dlp] Already up to date (%s)", currentVersion)
		return false, nil
	}

	// Check if this version is already registered (blacklisted or otherwise)
	if versionmanager != nil {
		if state, ok := versionmanager.GetVersionState(latestVersion); ok {
			if state == StateBlacklisted {
				logger.Infof("[yt-dlp] Version %s is blacklisted, skipping", latestVersion)
				return false, nil
			}
			if state == StateVerified || state == StateActive {
				logger.Debugf("[yt-dlp] Version %s already registered as %s", latestVersion, state)
				return false, nil
			}
			// StatePending: binary already on disk, just re-run canary
			binaryPath := VersionedBinaryPath(latestVersion)
			if _, statErr := os.Stat(binaryPath); statErr == nil {
				passed, networkErr := versionmanager.RunCanary(latestVersion)
				if !passed {
					if networkErr {
						logger.Warnf("[yt-dlp] Canary failed due to network, version %s stays pending", latestVersion)
					} else {
						logger.Warnf("[yt-dlp] Canary FAILED for %s, blacklisting", latestVersion)
						versionmanager.SetVersionState(latestVersion, StateBlacklisted)
					}
					return false, nil
				}
				versionmanager.SetVersionState(latestVersion, StateVerified)
				logger.Infof("[yt-dlp] Version %s verified by canary, will activate on next song", latestVersion)
				return true, nil
			}
			// Binary missing despite pending state, re-download below
		}
	}

	if currentVersion != "" && !force {
		logger.Infof("[yt-dlp] Update available: %s -> %s", currentVersion, latestVersion)
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

	// Use versioned path
	binaryPath := VersionedBinaryPath(latestVersion)
	versionDir := filepath.Dir(binaryPath)

	// Create version directory
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create version directory: %w", err)
	}

	// Download new version
	logger.Info("[yt-dlp] Downloading new version...")
	if err := DownloadFile(downloadURL, binaryPath); err != nil {
		// Clean up failed download directory
		os.RemoveAll(versionDir)
		return false, err
	}

	// Set executable permissions
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0755); err != nil {
			logger.Warnf("[yt-dlp] Failed to set permissions: %v", err)
		}
	}

	// Verify basic installation (--version check)
	cmd := exec.Command(binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		os.RemoveAll(versionDir)
		return false, fmt.Errorf("failed to verify version after download: %w", err)
	}
	actualVersion := strings.TrimSpace(string(output))
	logger.Infof("[yt-dlp] Downloaded version: %s", actualVersion)

	// Register with VersionManager and run canary
	if versionmanager != nil {
		versionmanager.RegisterVersion(latestVersion, binaryPath)

		passed, networkErr := versionmanager.RunCanary(latestVersion)
		if !passed {
			if networkErr {
				logger.Warnf("[yt-dlp] Canary failed due to network, version %s stays pending", latestVersion)
			} else {
				logger.Warnf("[yt-dlp] Canary FAILED for %s, blacklisting", latestVersion)
				versionmanager.SetVersionState(latestVersion, StateBlacklisted)
			}
			return false, nil
		}

		versionmanager.SetVersionState(latestVersion, StateVerified)
		logger.Infof("[yt-dlp] Version %s verified by canary, will activate on next song", latestVersion)
	} else {
		// No VersionManager (shouldn't happen in normal flow), legacy behavior
		logger.Infof("[yt-dlp] Update complete! Version: %s", actualVersion)
	}

	return true, nil
}

// StartBackgroundUpdater runs a background goroutine that checks for yt-dlp updates
// at regular intervals. Stops when ctx is cancelled.
func StartBackgroundUpdater(ctx context.Context) {
	go func() {
		logger.Info("[yt-dlp] Background updater started (interval: 6h)")
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("[yt-dlp] Background updater stopped")
				return
			case <-ticker.C:
				versionmanager := GetVersionManager()
				if versionmanager == nil {
					continue
				}

				// Respect minimum interval (prevent rapid checks on restarts)
				if time.Since(versionmanager.GetLastGitHubCheck()) < minCheckInterval {
					logger.Debugf("[yt-dlp] Skipping check, last check was %s ago", time.Since(versionmanager.GetLastGitHubCheck()).Round(time.Minute))
					continue
				}

				logger.Info("[yt-dlp] Background update check starting...")
				updated, err := UpdateYtDlp(false)
				if err != nil {
					logger.Errorf("[yt-dlp] Background update check failed: %v", err)
				} else if updated {
					logger.Info("[yt-dlp] Background update check found new version")
				}

				versionmanager.SetLastGitHubCheck(time.Now())
			}
		}
	}()
}

// MigrateFromLegacyLayout detects the old flat lib/yt-dlp layout and migrates
// to a versioned directory. Called once from main() on startup.
func MigrateFromLegacyLayout() error {
	versionmanager := GetVersionManager()
	if versionmanager == nil {
		return nil
	}

	// If there's already an active version, no migration needed
	if versionmanager.GetActiveVersion() != "" {
		return nil
	}

	legacyPath := GetLegacyBinaryPath()
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return nil // No legacy binary, nothing to migrate
	}

	// Get version of legacy binary
	cmd := exec.Command(legacyPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get legacy binary version: %w", err)
	}
	version := strings.TrimSpace(string(output))

	// Create versioned directory
	newPath := VersionedBinaryPath(version)
	newDir := filepath.Dir(newPath)
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return fmt.Errorf("failed to create version directory: %w", err)
	}

	// Move binary
	if err := os.Rename(legacyPath, newPath); err != nil {
		// If rename fails (cross-device), fall back to copy+delete
		if cpErr := copyFile(legacyPath, newPath); cpErr != nil {
			return fmt.Errorf("failed to migrate binary: %w", cpErr)
		}
		os.Remove(legacyPath)
	}

	// Set executable permissions
	if runtime.GOOS != "windows" {
		os.Chmod(newPath, 0755)
	}

	// Register and activate
	versionmanager.RegisterVersion(version, newPath)
	versionmanager.SetActiveVersion(version)

	// Seed with 1 success (it was working before migration)
	versionmanager.SaveSuccess(version, "")

	logger.Infof("[yt-dlp] Migrated legacy binary to versioned layout: %s -> %s", legacyPath, newPath)
	return nil
}

// AutoUpdate performs an initial update check on startup.
// After migration, checks for new versions and runs canary.
func AutoUpdate() {
	// Migrate from legacy layout if needed
	if err := MigrateFromLegacyLayout(); err != nil {
		logger.Warnf("[yt-dlp] Migration failed: %v", err)
	}

	updated, err := UpdateYtDlp(false)
	if err != nil {
		logger.Errorf("[yt-dlp] Auto-update check failed: %v", err)
		logger.Warn("[yt-dlp] Continuing with existing version")
	} else if updated {
		logger.Info("[yt-dlp] Auto-update completed successfully")
	}

	// Update check timestamp
	if versionmanager := GetVersionManager(); versionmanager != nil {
		versionmanager.SetLastGitHubCheck(time.Now())
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
