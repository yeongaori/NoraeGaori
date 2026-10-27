package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"noraegaori/internal/config"
	"noraegaori/internal/logger"
)

const (
	stableRepo           = "yt-dlp/yt-dlp"
	nightlyRepo          = "yt-dlp/yt-dlp-nightly-builds"
	githubAPI            = "https://api.github.com"
	githubWeb            = "https://github.com"
	updateCheckInterval  = 6 * time.Hour
	minCheckInterval     = 1 * time.Hour
	maxFallbackAttempts  = 5
	fallbackReleaseFetch = 15
	checksumAssetName    = "SHA2-256SUMS"
	checksumSigAssetName = "SHA2-256SUMS.sig"
)

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

	cmd := exec.Command(binaryPath, "--version") // #nosec G204
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func resolveCurrentVersion(versionmanager *VersionManager) string {
	if versionmanager != nil && versionmanager.hasActiveBinary() {
		return versionmanager.GetActiveVersion()
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

var lookPath = exec.LookPath

func hasPython() bool {
	_, err := lookPath("python3")
	return err == nil
}

func installVersionBinary(release *GitHubRelease, version string) (string, string, error) {
	assetName, downloadURL, err := GetDownloadAsset(release)
	if err != nil {
		return "", "", err
	}

	binaryPath := VersionedBinaryPath(version)
	versionDir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(versionDir, 0750); err != nil {
		return "", "", fmt.Errorf("failed to create version directory for %s: %w", version, err)
	}

	if err := DownloadVerified(release, assetName, downloadURL, binaryPath); err != nil {
		_ = os.RemoveAll(versionDir)
		return "", "", err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0755); err != nil { // #nosec G302
			logger.Warnf("Failed to set permissions: %v", err)
		}
	}

	reportedVersion, err := verifyBinaryRuns(binaryPath)
	if err != nil {
		_ = os.RemoveAll(versionDir)
		if assetName == pythonAssetName && !hasPython() {
			return "", "", fmt.Errorf("the Python build of yt-dlp needs python3 to verify %s: %w", version, err)
		}
		return "", "", fmt.Errorf("failed to verify %s after download: %w", version, err)
	}

	return binaryPath, reportedVersion, nil
}

func ensureVersionBinary(release *GitHubRelease, version string) (string, error) {
	binaryPath := VersionedBinaryPath(version)
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		if _, err := verifyBinaryRuns(binaryPath); err != nil {
			_ = os.RemoveAll(filepath.Dir(binaryPath))
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

var getLatestReleaseFn = GetLatestRelease

func UpdateYtDlp(force bool) (bool, error) {
	channel := ConfiguredChannel()
	if channel != config.YtDlpChannelAuto {
		outcome, err := updateChannelFn(channel, force)
		return outcome.updated, err
	}

	stable, stableErr := updateChannelFn(config.YtDlpChannelStable, force)
	if !stable.canaryFailed && activeVersionIsHealthy() {
		return stable.updated, stableErr
	}

	logger.Infof("Stable yt-dlp is not usable; trying the nightly channel")
	nightly, nightlyErr := updateChannelFn(config.YtDlpChannelNightly, force)
	if nightlyErr != nil {
		return nightly.updated, errors.Join(stableErr, nightlyErr)
	}
	return nightly.updated, nil
}

func resolveKnownVersion(versionmanager *VersionManager, channel, latestVersion string, release *GitHubRelease) (channelOutcome, bool, error) {
	state, ok := versionmanager.GetVersionState(latestVersion)
	if !ok {
		return channelOutcome{}, false, nil
	}
	if state == StateBlacklisted {
		logger.Infof("Version %s is blacklisted", latestVersion)
		if versionmanager.HasUsableBinary() {
			return channelOutcome{canaryFailed: true}, true, nil
		}
		logger.Warnf("No usable binary on disk; trying previous releases")
		outcome, err := installFallbackVersion(versionmanager, channel, latestVersion, release)
		return outcome, true, err
	}
	_, statErr := os.Stat(VersionedBinaryPath(latestVersion))
	isInstalled := statErr == nil

	if (state == StateActive || state == StateProvisional) && isInstalled {
		logger.Debugf("Version %s already registered as %s", latestVersion, state)
		return channelOutcome{}, true, nil
	}

	if state == StateVerified && activeVersionIsHealthy() {
		logger.Debugf("Version %s is verified but the active version is healthy; staying put", latestVersion)
		return channelOutcome{}, true, nil
	}

	if isInstalled {
		if state == StateVerified {
			logger.Infof("Re-verifying %s before returning to it", latestVersion)
		}
		outcome, err := activateOrFallback(versionmanager, channel, latestVersion, release)
		return outcome, true, err
	}

	logger.Debugf("Version %s is %s but its binary is gone; reinstalling", latestVersion, state)
	return channelOutcome{}, false, nil
}

func logUpdateIntent(currentVersion, latestVersion string, force bool) {
	if currentVersion != "" && !force {
		logger.Infof("Update available: %s -> %s", currentVersion, latestVersion)
	} else if force {
		logger.Infof("Force updating to %s", latestVersion)
	} else {
		logger.Infof("Installing version %s", latestVersion)
	}
}

func updateFromChannel(channel string, force bool) (channelOutcome, error) {
	logger.Debugf("Checking for updates on the %s channel...", channel)

	versionmanager := GetVersionManager()
	currentVersion := resolveCurrentVersion(versionmanager)

	release, err := getLatestReleaseFn(channel)
	if err != nil {
		return channelOutcome{}, fmt.Errorf("failed to fetch release info: %w", err)
	}

	latestVersion := release.TagName

	if !force && currentVersion == latestVersion {
		logger.Debugf("Already up to date (%s)", currentVersion)
		return channelOutcome{}, nil
	}

	if versionmanager != nil {
		if outcome, isHandled, err := resolveKnownVersion(versionmanager, channel, latestVersion, release); isHandled {
			return outcome, err
		}
	}

	logUpdateIntent(currentVersion, latestVersion, force)

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

	if _, ok := versionmanager.GetVersionState(latestVersion); !ok {
		versionmanager.RegisterVersion(latestVersion, binaryPath)
	}

	return activateOrFallback(versionmanager, channel, latestVersion, release)
}

func activateOrFallback(versionmanager *VersionManager, channel, latestVersion string, release *GitHubRelease) (channelOutcome, error) {
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

		if outcome, isDone := tryFallbackRelease(versionmanager, rel, considered); isDone {
			return outcome, nil
		}
	}

	if versionmanager.HasUsableBinary() {
		logger.Warnf("All fallback attempts failed canary; keeping the installed version")
		return channelOutcome{canaryFailed: true}, nil
	}

	return activateLastResort(versionmanager, latestRelease, latestVersion)
}

func tryFallbackRelease(versionmanager *VersionManager, rel *GitHubRelease, considered int) (channelOutcome, bool) {
	ver := rel.TagName
	if state, ok := versionmanager.GetVersionState(ver); ok {
		if state == StateBlacklisted {
			logger.Debugf("Fallback candidate %d/%d: %s already blacklisted, skipping", considered, maxFallbackAttempts, ver)
			return channelOutcome{}, false
		}
		if state == StateVerified || state == StateActive {
			if _, statErr := os.Stat(VersionedBinaryPath(ver)); statErr == nil {
				logger.Debugf("Reusing existing %s version %s", state, ver)
				return channelOutcome{updated: true}, true
			}
		}
	}

	logger.Infof("Fallback candidate %d/%d: trying version %s", considered, maxFallbackAttempts, ver)

	binaryPath, _, installErr := installVersionBinary(rel, ver)
	if installErr != nil {
		logger.Warnf("Fallback candidate %s could not be installed: %v", ver, installErr)
		return channelOutcome{}, false
	}

	versionmanager.RegisterVersion(ver, binaryPath)

	switch runCanaryAndActivate(versionmanager, ver) {
	case canaryActivated:
		return channelOutcome{updated: true}, true
	case canaryPending:
		logger.Warnf("Aborting the fallback chain after a canary network error on %s", ver)
		return channelOutcome{}, true
	default:
		return channelOutcome{}, false
	}
}

func activateLastResort(versionmanager *VersionManager, latestRelease *GitHubRelease, latestVersion string) (channelOutcome, error) {
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

	versionmanager.Cleanup()
	versionmanager.SetLastGitHubCheck(time.Now())
}

func RequestUpdateCheck() {
	select {
	case updateCheckRequests <- struct{}{}:
	default:
	}
}

func StartBackgroundUpdater(ctx context.Context, onSchedule func()) {
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
				onSchedule()
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

	cmd := exec.Command(legacyPath, "--version") // #nosec G204
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get legacy binary version: %w", err)
	}
	version := strings.TrimSpace(string(output))

	newPath := VersionedBinaryPath(version)
	newDir := filepath.Dir(newPath)
	if err := os.MkdirAll(newDir, 0750); err != nil {
		return fmt.Errorf("failed to create version directory: %w", err)
	}

	if err := os.Rename(legacyPath, newPath); err != nil {

		if cpErr := copyFile(legacyPath, newPath); cpErr != nil {
			return fmt.Errorf("failed to migrate binary: %w", cpErr)
		}
		_ = os.Remove(legacyPath)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(newPath, 0755); err != nil { // #nosec G302
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

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src) // #nosec G304
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst) // #nosec G304
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}
