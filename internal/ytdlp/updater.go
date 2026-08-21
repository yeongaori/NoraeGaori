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
	"noraegaori/internal/logger"

	"github.com/ProtonMail/go-crypto/openpgp"
)

//go:embed keys/yt-dlp.asc
var ytdlpSigningKey []byte

var signingKeyArmor = ytdlpSigningKey

const (
	stableRepo           = "yt-dlp/yt-dlp"
	nightlyRepo          = "yt-dlp/yt-dlp-nightly-builds"
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

func releaseRepo(channel string) string {
	if channel == config.YtDlpChannelNightly {
		return nightlyRepo
	}
	return stableRepo
}

func latestReleaseURL(channel string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", releaseRepo(channel))
}

func releasesURL(channel string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases", releaseRepo(channel))
}

func GetLatestRelease(channel string) (*GitHubRelease, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", latestReleaseURL(channel), nil)
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

func GetReleases(channel string, perPage int) ([]*GitHubRelease, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("%s?per_page=%d", releasesURL(channel), perPage)
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

func resolveCurrentVersion(versionmanager *VersionManager) string {
	if versionmanager != nil {
		if version := versionmanager.GetActiveVersion(); version != "" {
			return version
		}
	}

	version, err := GetCurrentVersion()
	if err != nil {
		logger.Debugf("No version currently installed: %v", err)
		return ""
	}
	return version
}

func verifyBinaryRuns(binaryPath string) (string, error) {
	output, err := exec.Command(binaryPath, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func installVersionBinary(release *GitHubRelease, version string) (string, string, error) {
	assetName, downloadURL, err := GetDownloadAsset(release)
	if err != nil {
		return "", "", err
	}

	binaryPath := VersionedBinaryPath(version)
	versionDir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create version directory for %s: %w", version, err)
	}

	if err := DownloadVerified(release, assetName, downloadURL, binaryPath); err != nil {
		os.RemoveAll(versionDir)
		return "", "", err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0755); err != nil {
			logger.Warnf("Failed to set permissions: %v", err)
		}
	}

	reportedVersion, err := verifyBinaryRuns(binaryPath)
	if err != nil {
		os.RemoveAll(versionDir)
		return "", "", fmt.Errorf("failed to verify %s after download: %w", version, err)
	}

	return binaryPath, reportedVersion, nil
}

func ensureVersionBinary(release *GitHubRelease, version string) (string, error) {
	binaryPath := VersionedBinaryPath(version)
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		if _, err := verifyBinaryRuns(binaryPath); err != nil {
			os.RemoveAll(filepath.Dir(binaryPath))
			return "", fmt.Errorf("failed to verify the installed %s: %w", version, err)
		}
		return binaryPath, nil
	}

	binaryPath, _, err := installVersionBinary(release, version)
	return binaryPath, err
}

type canaryVerdict int

const (
	canaryActivated canaryVerdict = iota
	canaryPending
	canaryRejected
)

var runCanary = (*VersionManager).RunCanary

func runCanaryAndActivate(versionmanager *VersionManager, version string) canaryVerdict {
	passed, networkErr := runCanary(versionmanager, version)
	if passed {
		versionmanager.SetVersionState(version, StateVerified)
		versionmanager.SetActiveVersion(version)
		logger.Infof("Version %s verified by canary and activated", version)
		return canaryActivated
	}

	if networkErr {
		logger.Warnf("Canary failed due to network, version %s stays pending", version)
		return canaryPending
	}

	logger.Warnf("Canary FAILED for %s, blacklisting", version)
	versionmanager.SetVersionState(version, StateBlacklisted)
	return canaryRejected
}

func activeVersionIsHealthy() bool {
	versionmanager := GetVersionManager()
	if versionmanager == nil {
		return false
	}

	active := versionmanager.GetActiveVersion()
	if active == "" {
		return false
	}

	state, ok := versionmanager.GetVersionState(active)
	if !ok || (state != StateActive && state != StateVerified) {
		return false
	}

	if versionmanager.ActiveVersionIsFailing() {
		logger.Warnf("Active version %s is failing playback", active)
		return false
	}

	return true
}

type channelOutcome struct {
	updated      bool
	canaryFailed bool
}

var updateChannelFn = updateFromChannel

var getReleasesFn = GetReleases

func UpdateYtDlp(force bool) (bool, error) {
	channel := ConfiguredChannel()
	if channel != config.YtDlpChannelAuto {
		outcome, err := updateChannelFn(channel, force)
		return outcome.updated, err
	}

	outcome, err := updateChannelFn(config.YtDlpChannelStable, force)
	if !outcome.canaryFailed && activeVersionIsHealthy() {
		return outcome.updated, err
	}

	logger.Infof("Stable yt-dlp is not usable; trying the nightly channel")
	nightly, err := updateChannelFn(config.YtDlpChannelNightly, force)
	return nightly.updated, err
}

func updateFromChannel(channel string, force bool) (channelOutcome, error) {
	logger.Debugf("Checking for updates on the %s channel...", channel)

	versionmanager := GetVersionManager()
	currentVersion := resolveCurrentVersion(versionmanager)

	release, err := GetLatestRelease(channel)
	if err != nil {
		return channelOutcome{}, fmt.Errorf("failed to fetch release info: %w", err)
	}

	latestVersion := release.TagName

	if !force && currentVersion == latestVersion {
		logger.Debugf("Already up to date (%s)", currentVersion)
		return channelOutcome{}, nil
	}

	if versionmanager != nil {
		if state, ok := versionmanager.GetVersionState(latestVersion); ok {
			if state == StateBlacklisted {
				logger.Infof("Version %s is blacklisted", latestVersion)
				if versionmanager.HasUsableBinary() {
					return channelOutcome{canaryFailed: true}, nil
				}
				logger.Warnf("No usable binary on disk; trying previous releases")
				return installFallbackVersion(versionmanager, channel, latestVersion, release)
			}
			if state == StateActive || state == StateProvisional {
				logger.Debugf("Version %s already registered as %s", latestVersion, state)
				return channelOutcome{}, nil
			}

			if _, statErr := os.Stat(VersionedBinaryPath(latestVersion)); statErr == nil {
				if state == StateVerified {
					logger.Infof("Re-verifying %s before returning to it", latestVersion)
				}
				switch runCanaryAndActivate(versionmanager, latestVersion) {
				case canaryActivated:
					return channelOutcome{updated: true}, nil
				case canaryPending:
					return channelOutcome{}, nil
				default:
					logger.Warnf("Trying previous releases after %s failed canary", latestVersion)
					return installFallbackVersion(versionmanager, channel, latestVersion, release)
				}
			}

			if state == StateVerified {
				logger.Debugf("Version %s is verified but its binary is gone; reinstalling", latestVersion)
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

	logger.Debug("Downloading new version...")
	binaryPath, actualVersion, err := installVersionBinary(release, latestVersion)
	if err != nil {
		return channelOutcome{}, err
	}
	logger.Infof("Downloaded version: %s", actualVersion)

	if versionmanager == nil {
		logger.Infof("Update complete! Version: %s", actualVersion)
		return channelOutcome{updated: true}, nil
	}

	versionmanager.RegisterVersion(latestVersion, binaryPath)

	switch runCanaryAndActivate(versionmanager, latestVersion) {
	case canaryActivated:
		return channelOutcome{updated: true}, nil
	case canaryPending:
		return channelOutcome{}, nil
	default:
		logger.Warnf("Trying previous releases after %s failed canary", latestVersion)
		return installFallbackVersion(versionmanager, channel, latestVersion, release)
	}
}

func installFallbackVersion(versionmanager *VersionManager, channel, latestVersion string, latestRelease *GitHubRelease) (channelOutcome, error) {
	releases, err := getReleasesFn(channel, fallbackReleaseFetch)
	if err != nil {
		return channelOutcome{canaryFailed: true}, fmt.Errorf("failed to fetch release list: %w", err)
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
				if _, statErr := os.Stat(VersionedBinaryPath(ver)); statErr == nil {
					logger.Debugf("Reusing existing %s version %s", state, ver)
					return channelOutcome{updated: true}, nil
				}
			}
		}

		logger.Infof("Fallback candidate %d/%d: trying version %s", considered, maxFallbackAttempts, ver)

		binaryPath, _, installErr := installVersionBinary(rel, ver)
		if installErr != nil {
			logger.Warnf("Fallback candidate %s could not be installed: %v", ver, installErr)
			continue
		}

		versionmanager.RegisterVersion(ver, binaryPath)

		switch runCanaryAndActivate(versionmanager, ver) {
		case canaryActivated:
			return channelOutcome{updated: true}, nil
		case canaryPending:
			logger.Warnf("Aborting the fallback chain after a canary network error on %s", ver)
			return channelOutcome{}, nil
		default:
			continue
		}
	}

	if versionmanager.HasUsableBinary() {
		logger.Warnf("All fallback attempts failed canary; keeping the installed version")
		return channelOutcome{canaryFailed: true}, nil
	}

	logger.Warnf("No usable binary on disk; provisionally activating latest %s as last resort", latestVersion)

	binaryPath, err := ensureVersionBinary(latestRelease, latestVersion)
	if err != nil {
		return channelOutcome{canaryFailed: true}, fmt.Errorf("last-resort: %w", err)
	}

	versionmanager.ProvisionallyActivate(latestVersion, binaryPath)
	return channelOutcome{updated: true, canaryFailed: true}, nil
}

var updateCheckRequests = make(chan struct{}, 1)

func runBackgroundUpdateCheck() {
	versionmanager := GetVersionManager()
	if versionmanager == nil {
		return
	}

	if time.Since(versionmanager.GetLastGitHubCheck()) < minCheckInterval {
		logger.Debugf("Skipping check, last check was %s ago", time.Since(versionmanager.GetLastGitHubCheck()).Round(time.Minute))
		return
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

func RequestUpdateCheck() {
	select {
	case updateCheckRequests <- struct{}{}:
	default:
	}
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
			case <-updateCheckRequests:
				runBackgroundUpdateCheck()
			case <-ticker.C:
				runBackgroundUpdateCheck()
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
