package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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

	"noraegaori/internal/config"
	"noraegaori/pkg/logger"

	"github.com/ProtonMail/go-crypto/openpgp"
)

//go:embed keys/yt-dlp.asc
var ytdlpSigningKey []byte

var signingKeyArmor = ytdlpSigningKey

const (
	githubAPIURL         = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"
	githubReleasesURL    = "https://api.github.com/repos/yt-dlp/yt-dlp/releases"
	updateCheckInterval  = 6 * time.Hour
	minCheckInterval     = 1 * time.Hour
	maxFallbackAttempts  = 5
	fallbackReleaseFetch = 15
	checksumAssetName    = "SHA2-256SUMS"
	checksumSigAssetName = "SHA2-256SUMS.sig"
	checksumDigestPrefix = "sha256:"
	defaultDownloadMbps  = 10.0
	downloadTimeout      = 30 * time.Minute
)

type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

func GetLegacyBinaryPath() string {
	binaryName := "yt-dlp"
	if runtime.GOOS == "windows" {
		binaryName = "yt-dlp.exe"
	}
	return filepath.Join("lib", binaryName)
}

func GetBinaryPath() string {
	if versionmanager := GetVersionManager(); versionmanager != nil {
		return versionmanager.ActiveBinaryPath()
	}
	return GetLegacyBinaryPath()
}

func VersionedBinaryPath(version string) string {
	binaryName := "yt-dlp"
	if runtime.GOOS == "windows" {
		binaryName = "yt-dlp.exe"
	}
	return filepath.Join("lib", fmt.Sprintf("yt-dlp-%s", version), binaryName)
}

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

func GetReleases(perPage int) ([]*GitHubRelease, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("%s?per_page=%d", githubReleasesURL, perPage)
	req, err := http.NewRequest("GET", url, nil)
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

	var releases []*GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases list: %w", err)
	}

	return releases, nil
}

func GetDownloadAsset(release *GitHubRelease) (string, string, error) {
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
			return "", "", fmt.Errorf("ARMv7l on Linux is not directly supported")
		default:
			assetName = "yt-dlp"
		}
	default:
		return "", "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			sizeMB := float64(asset.Size) / 1024 / 1024
			logger.Debugf("Found asset: %s (%.2f MB)", asset.Name, sizeMB)
			return asset.Name, asset.BrowserDownloadURL, nil
		}
	}

	return "", "", fmt.Errorf("asset not found: %s", assetName)
}

func parseChecksums(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed checksum line: %q", line)
		}

		sum := strings.ToLower(fields[0])
		decoded, err := hex.DecodeString(sum)
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("malformed checksum for %s: %q", fields[1], fields[0])
		}

		checksums[strings.TrimPrefix(fields[1], "*")] = sum
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read checksums: %w", err)
	}

	if len(checksums) == 0 {
		return nil, fmt.Errorf("%s contained no entries", checksumAssetName)
	}

	return checksums, nil
}

func assetURL(release *GitHubRelease, name string) string {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func fetchAsset(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "yt-dlp-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asset request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset request returned status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func VerifyChecksumSignature(checksums, signature []byte) error {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(signingKeyArmor))
	if err != nil {
		return fmt.Errorf("failed to read the bundled signing key: %w", err)
	}

	if _, err := openpgp.CheckDetachedSignature(keyring, bytes.NewReader(checksums), bytes.NewReader(signature), nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

func fetchChecksums(release *GitHubRelease) (map[string]string, error) {
	checksumURL := assetURL(release, checksumAssetName)
	if checksumURL == "" {
		return nil, fmt.Errorf("release %s has no %s asset", release.TagName, checksumAssetName)
	}

	signatureURL := assetURL(release, checksumSigAssetName)
	if signatureURL == "" {
		return nil, fmt.Errorf("release %s has no %s asset", release.TagName, checksumSigAssetName)
	}

	checksums, err := fetchAsset(checksumURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", checksumAssetName, err)
	}

	signature, err := fetchAsset(signatureURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", checksumSigAssetName, err)
	}

	if err := VerifyChecksumSignature(checksums, signature); err != nil {
		return nil, fmt.Errorf("%s for release %s: %w", checksumAssetName, release.TagName, err)
	}

	logger.Debugf("Verified %s signature for release %s", checksumAssetName, release.TagName)

	return parseChecksums(bytes.NewReader(checksums))
}

func assetDigest(release *GitHubRelease, assetName string) string {
	for _, asset := range release.Assets {
		if asset.Name == assetName && strings.HasPrefix(asset.Digest, checksumDigestPrefix) {
			return strings.ToLower(strings.TrimPrefix(asset.Digest, checksumDigestPrefix))
		}
	}
	return ""
}

func ExpectedChecksum(release *GitHubRelease, assetName string) (string, error) {
	checksums, err := fetchChecksums(release)
	if err != nil {
		return "", err
	}

	sum, ok := checksums[assetName]
	if !ok {
		return "", fmt.Errorf("%s has no entry for %s", checksumAssetName, assetName)
	}

	if digest := assetDigest(release, assetName); digest != "" && digest != sum {
		return "", fmt.Errorf("%s disagrees with the signed %s for %s: %s vs %s", "the GitHub asset digest", checksumAssetName, assetName, digest, sum)
	}

	return sum, nil
}

func DownloadVerified(release *GitHubRelease, assetName, url, destination string) error {
	expected, err := ExpectedChecksum(release, assetName)
	if err != nil {
		return fmt.Errorf("failed to resolve checksum for %s: %w", assetName, err)
	}

	actual, err := DownloadFile(url, destination)
	if err != nil {
		return err
	}

	if actual != expected {
		os.Remove(destination)
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}

	logger.Debugf("Checksum verified for %s", assetName)
	return nil
}

func downloadRateLimit() float64 {
	mbps := defaultDownloadMbps
	if cfg := config.GetConfig(); cfg != nil && cfg.MaxDownloadSpeedMbps > 0 {
		mbps = cfg.MaxDownloadSpeedMbps
	}
	return mbps * 1000 * 1000 / 8
}

func DownloadFile(url, destination string) (string, error) {
	logger.Debugf("Starting download from: %s", url)

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status: %d", resp.StatusCode)
	}

	out, err := os.Create(destination)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	totalSize := resp.ContentLength
	downloaded := int64(0)
	lastProgress := 0

	hasher := sha256.New()
	sink := io.MultiWriter(out, hasher)

	rateLimit := downloadRateLimit()
	buffer := make([]byte, 16*1024)
	chunkDelay := time.Duration(float64(len(buffer)) / rateLimit * float64(time.Second))

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := sink.Write(buffer[:n]); writeErr != nil {
				return "", fmt.Errorf("failed to write to file: %w", writeErr)
			}
			downloaded += int64(n)

			if totalSize > 0 {
				progress := int((downloaded * 100) / totalSize)
				if progress >= lastProgress+10 {
					logger.Debugf("Download progress: %d%%", progress)
					lastProgress = progress
				}
			}

			time.Sleep(chunkDelay)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("download interrupted: %w", err)
		}
	}

	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("failed to flush file: %w", err)
	}

	logger.Debugf("Download completed")
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func UpdateYtDlp(force bool) (bool, error) {
	logger.Debug("Checking for updates...")

	versionmanager := GetVersionManager()

	var currentVersion string
	if versionmanager != nil {
		currentVersion = versionmanager.GetActiveVersion()
	}
	if currentVersion == "" {
		ver, err := GetCurrentVersion()
		if err != nil {
			logger.Debugf("No version currently installed: %v", err)
		} else {
			currentVersion = ver
		}
	}

	release, err := GetLatestRelease()
	if err != nil {
		return false, fmt.Errorf("failed to fetch release info: %w", err)
	}

	latestVersion := release.TagName

	if !force && currentVersion == latestVersion {
		logger.Debugf("Already up to date (%s)", currentVersion)
		return false, nil
	}

	if versionmanager != nil {
		if state, ok := versionmanager.GetVersionState(latestVersion); ok {
			if state == StateBlacklisted {
				logger.Infof("Version %s is blacklisted", latestVersion)
				if versionmanager.HasUsableBinary() {
					return false, nil
				}
				logger.Warnf("No usable binary on disk; trying previous releases")
				return installFallbackVersion(versionmanager, latestVersion, release)
			}
			if state == StateVerified || state == StateActive || state == StateProvisional {
				logger.Debugf("Version %s already registered as %s", latestVersion, state)
				return false, nil
			}

			binaryPath := VersionedBinaryPath(latestVersion)
			if _, statErr := os.Stat(binaryPath); statErr == nil {
				passed, networkErr := versionmanager.RunCanary(latestVersion)
				if !passed {
					if networkErr {
						logger.Warnf("Canary failed due to network, version %s stays pending", latestVersion)
						return false, nil
					}
					logger.Warnf("Canary FAILED for %s, blacklisting and trying previous releases", latestVersion)
					versionmanager.SetVersionState(latestVersion, StateBlacklisted)
					return installFallbackVersion(versionmanager, latestVersion, release)
				}
				versionmanager.SetVersionState(latestVersion, StateVerified)
				versionmanager.SetActiveVersion(latestVersion)
				logger.Infof("Version %s verified by canary and activated", latestVersion)
				return true, nil
			}

		}
	}

	if currentVersion != "" && !force {
		logger.Infof("Update available: %s -> %s", currentVersion, latestVersion)
	} else if force {
		logger.Infof("Force updating to %s", latestVersion)
	} else {
		logger.Infof("Installing version %s", latestVersion)
	}

	assetName, downloadURL, err := GetDownloadAsset(release)
	if err != nil {
		return false, err
	}

	binaryPath := VersionedBinaryPath(latestVersion)
	versionDir := filepath.Dir(binaryPath)

	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create version directory: %w", err)
	}

	logger.Debug("Downloading new version...")
	if err := DownloadVerified(release, assetName, downloadURL, binaryPath); err != nil {

		os.RemoveAll(versionDir)
		return false, err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0755); err != nil {
			logger.Warnf("Failed to set permissions: %v", err)
		}
	}

	cmd := exec.Command(binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		os.RemoveAll(versionDir)
		return false, fmt.Errorf("failed to verify version after download: %w", err)
	}
	actualVersion := strings.TrimSpace(string(output))
	logger.Infof("Downloaded version: %s", actualVersion)

	if versionmanager != nil {
		versionmanager.RegisterVersion(latestVersion, binaryPath)

		passed, networkErr := versionmanager.RunCanary(latestVersion)
		if !passed {
			if networkErr {
				logger.Warnf("Canary failed due to network, version %s stays pending", latestVersion)
				return false, nil
			}
			logger.Warnf("Canary FAILED for %s, blacklisting and trying previous releases", latestVersion)
			versionmanager.SetVersionState(latestVersion, StateBlacklisted)
			return installFallbackVersion(versionmanager, latestVersion, release)
		}

		versionmanager.SetVersionState(latestVersion, StateVerified)
		versionmanager.SetActiveVersion(latestVersion)
		logger.Infof("Version %s verified by canary and activated", latestVersion)
	} else {

		logger.Infof("Update complete! Version: %s", actualVersion)
	}

	return true, nil
}

func installFallbackVersion(versionmanager *VersionManager, latestVersion string, latestRelease *GitHubRelease) (bool, error) {
	releases, err := GetReleases(fallbackReleaseFetch)
	if err != nil {
		return false, fmt.Errorf("failed to fetch release list: %w", err)
	}

	considered := 0
	for _, rel := range releases {
		ver := rel.TagName
		if ver == latestVersion {
			continue
		}
		if considered >= maxFallbackAttempts {
			break
		}
		considered++

		if state, ok := versionmanager.GetVersionState(ver); ok {
			if state == StateBlacklisted {
				logger.Debugf("Fallback candidate %d/%d: %s already blacklisted, skipping", considered, maxFallbackAttempts, ver)
				continue
			}
			if state == StateVerified || state == StateActive {
				binaryPath := VersionedBinaryPath(ver)
				if _, statErr := os.Stat(binaryPath); statErr == nil {
					logger.Debugf("Reusing existing %s version %s", state, ver)
					return true, nil
				}
			}
		}

		logger.Infof("Fallback candidate %d/%d: trying version %s", considered, maxFallbackAttempts, ver)

		assetName, downloadURL, urlErr := GetDownloadAsset(rel)
		if urlErr != nil {
			logger.Warnf("No download URL for %s: %v", ver, urlErr)
			continue
		}

		binaryPath := VersionedBinaryPath(ver)
		versionDir := filepath.Dir(binaryPath)
		if err := os.MkdirAll(versionDir, 0755); err != nil {
			logger.Warnf("Failed to create version dir for %s: %v", ver, err)
			continue
		}

		if err := DownloadVerified(rel, assetName, downloadURL, binaryPath); err != nil {
			logger.Warnf("Download of %s failed: %v", ver, err)
			os.RemoveAll(versionDir)
			continue
		}

		if runtime.GOOS != "windows" {
			if err := os.Chmod(binaryPath, 0755); err != nil {
				logger.Warnf("Failed to set permissions on %s: %v", ver, err)
			}
		}

		cmd := exec.Command(binaryPath, "--version")
		if _, err := cmd.Output(); err != nil {
			logger.Warnf("Version check failed for %s: %v", ver, err)
			os.RemoveAll(versionDir)
			continue
		}

		versionmanager.RegisterVersion(ver, binaryPath)

		passed, networkErr := versionmanager.RunCanary(ver)
		if !passed {
			if networkErr {
				logger.Warnf("Canary network error on fallback %s; aborting fallback chain", ver)
				return false, nil
			}
			logger.Warnf("Fallback %s failed canary, blacklisting", ver)
			versionmanager.SetVersionState(ver, StateBlacklisted)
			continue
		}

		versionmanager.SetVersionState(ver, StateVerified)
		versionmanager.SetActiveVersion(ver)
		logger.Infof("Fallback version %s verified by canary and activated", ver)
		return true, nil
	}

	logger.Warnf("All fallback attempts failed canary; provisionally activating latest %s as last resort", latestVersion)

	assetName, downloadURL, err := GetDownloadAsset(latestRelease)
	if err != nil {
		return false, fmt.Errorf("last-resort: no download URL for %s: %w", latestVersion, err)
	}

	binaryPath := VersionedBinaryPath(latestVersion)
	versionDir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return false, fmt.Errorf("last-resort: failed to create version dir: %w", err)
	}

	if _, statErr := os.Stat(binaryPath); statErr != nil {
		if err := DownloadVerified(latestRelease, assetName, downloadURL, binaryPath); err != nil {
			os.RemoveAll(versionDir)
			return false, fmt.Errorf("last-resort: download failed: %w", err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(binaryPath, 0755); err != nil {
				logger.Warnf("Failed to set permissions: %v", err)
			}
		}
	}

	if _, err := exec.Command(binaryPath, "--version").Output(); err != nil {
		os.RemoveAll(versionDir)
		return false, fmt.Errorf("last-resort: --version check failed: %w", err)
	}

	versionmanager.ProvisionallyActivate(latestVersion, binaryPath)
	return true, nil
}

func StartBackgroundUpdater(ctx context.Context) {
	go func() {
		logger.Debug("Background updater started")
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Debug("Background updater stopped")
				return
			case <-ticker.C:
				versionmanager := GetVersionManager()
				if versionmanager == nil {
					continue
				}

				if time.Since(versionmanager.GetLastGitHubCheck()) < minCheckInterval {
					logger.Debugf("Skipping check, last check was %s ago", time.Since(versionmanager.GetLastGitHubCheck()).Round(time.Minute))
					continue
				}

				logger.Debug("Background update check starting...")
				updated, err := UpdateYtDlp(false)
				if err != nil {
					logger.Errorf("Background update check failed: %v", err)
				} else if updated {
					logger.Info("Background update found new version")
				}

				versionmanager.SetLastGitHubCheck(time.Now())
			}
		}
	}()
}

func MigrateFromLegacyLayout() error {
	versionmanager := GetVersionManager()
	if versionmanager == nil {
		return nil
	}

	if versionmanager.GetActiveVersion() != "" {
		return nil
	}

	legacyPath := GetLegacyBinaryPath()
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return nil
	}

	cmd := exec.Command(legacyPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get legacy binary version: %w", err)
	}
	version := strings.TrimSpace(string(output))

	newPath := VersionedBinaryPath(version)
	newDir := filepath.Dir(newPath)
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return fmt.Errorf("failed to create version directory: %w", err)
	}

	if err := os.Rename(legacyPath, newPath); err != nil {

		if cpErr := copyFile(legacyPath, newPath); cpErr != nil {
			return fmt.Errorf("failed to migrate binary: %w", cpErr)
		}
		os.Remove(legacyPath)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(newPath, 0755); err != nil {
			logger.Warnf("Failed to set permissions on %s: %v", newPath, err)
		}
	}

	versionmanager.RegisterVersion(version, newPath)
	versionmanager.SetActiveVersion(version)

	versionmanager.SaveSuccess(version, "")

	logger.Infof("Migrated legacy binary to versioned layout: %s -> %s", legacyPath, newPath)
	return nil
}

func AutoUpdate() {

	if err := MigrateFromLegacyLayout(); err != nil {
		logger.Warnf("Migration failed: %v", err)
	}

	updated, err := UpdateYtDlp(false)
	if err != nil {
		logger.Errorf("Auto-update check failed: %v", err)
		logger.Warn("Continuing with existing version")
	} else if updated {
		logger.Info("Auto-update completed successfully")
	}

	if versionmanager := GetVersionManager(); versionmanager != nil {
		versionmanager.SetLastGitHubCheck(time.Now())
	}
}

var jsRuntime string

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
			logger.Warnf("Node.js not found, using %s", rt)
			jsRuntime = rt
			return
		}
	}

	logger.Warn("Node.js not found, using no JS runtime")
}

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
	if err := os.Setenv("PATH", nodeBin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		logger.Warnf("Failed to add %s to PATH: %v", nodeBin, err)
		return false
	}
	return true
}

func GetJsRuntime() string {
	return jsRuntime
}

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
