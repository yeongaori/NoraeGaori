package ytdlp

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"runtime"

	"noraegaori/internal/config"
	"noraegaori/internal/download"
	"noraegaori/internal/logger"

	"github.com/ProtonMail/go-crypto/openpgp"
)

//go:embed keys/yt-dlp.asc
var ytdlpSigningKey []byte

var signingKeyArmor = ytdlpSigningKey

type GitHubRelease = download.Release

func releaseRepo(channel string) string {
	if channel == config.YtDlpChannelNightly {
		return nightlyRepo
	}
	return stableRepo
}

func latestReleaseRepo(channel string) download.GitHubRepo {
	return download.GitHubRepo{APIURL: githubAPI, WebURL: githubWeb, Repo: releaseRepo(channel)}
}

func releasesURL(channel string) string {
	return fmt.Sprintf("%s/repos/%s/releases", githubAPI, releaseRepo(channel))
}

func GetLatestRelease(channel string) (*GitHubRelease, error) {
	repo := latestReleaseRepo(channel)
	return repo.FetchLatest(checksumAssetName, checksumSigAssetName)
}

func GetReleases(channel string, perPage int) ([]*GitHubRelease, error) {
	return download.FetchReleases(fmt.Sprintf("%s?per_page=%d", releasesURL(channel), perPage))
}

var standaloneAssetNames = map[string]string{
	"linux/amd64":   "yt-dlp_linux",
	"linux/arm64":   "yt-dlp_linux_aarch64",
	"linux/386":     "yt-dlp_linux_i686",
	"windows/amd64": "yt-dlp.exe",
	"windows/386":   "yt-dlp_x86.exe",
	"windows/arm64": "yt-dlp_arm64.exe",
	"darwin/amd64":  "yt-dlp_macos",
	"darwin/arm64":  "yt-dlp_macos",
}

const pythonAssetName = "yt-dlp"

func pickAsset(release *GitHubRelease, goos, goarch string) (*download.Asset, error) {
	candidates := make([]string, 0, 2)
	if name, ok := standaloneAssetNames[goos+"/"+goarch]; ok {
		candidates = append(candidates, name)
	}
	if goos != "windows" {
		candidates = append(candidates, pythonAssetName)
	}

	for _, name := range candidates {
		if asset, ok := release.FindAsset(name); ok {
			return asset, nil
		}
	}

	return nil, fmt.Errorf("no yt-dlp build for %s/%s", goos, goarch)
}

func GetDownloadAsset(release *GitHubRelease) (string, string, error) {
	asset, err := pickAsset(release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", "", err
	}

	logger.Debugf("Found asset: %s (%.2f MB)", asset.Name, float64(asset.Size)/1024/1024)
	return asset.Name, asset.BrowserDownloadURL, nil
}

func VerifySignature(checksums, signature []byte) error {
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
	checksumURL := release.AssetURL(checksumAssetName)
	if checksumURL == "" {
		return nil, fmt.Errorf("release %s has no %s asset", release.TagName, checksumAssetName)
	}

	signatureURL := release.AssetURL(checksumSigAssetName)
	if signatureURL == "" {
		return nil, fmt.Errorf("release %s has no %s asset", release.TagName, checksumSigAssetName)
	}

	checksums, err := download.FetchBytes(checksumURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", checksumAssetName, err)
	}

	signature, err := download.FetchBytes(signatureURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", checksumSigAssetName, err)
	}

	if err := VerifySignature(checksums, signature); err != nil {
		return nil, fmt.Errorf("%s for release %s: %w", checksumAssetName, release.TagName, err)
	}

	logger.Debugf("Verified %s signature for release %s", checksumAssetName, release.TagName)

	return download.ParseChecksums(bytes.NewReader(checksums))
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

	if err := download.CheckDigest(assetName, sum, release.AssetDigest(assetName)); err != nil {
		return "", err
	}

	return sum, nil
}

func DownloadVerified(release *GitHubRelease, assetName, url, destination string) error {
	expected, err := ExpectedChecksum(release, assetName)
	if err != nil {
		return fmt.Errorf("failed to resolve checksum for %s: %w", assetName, err)
	}

	actual, err := download.SaveFile(url, destination)
	if err != nil {
		return err
	}

	if actual != expected {
		_ = os.Remove(destination)
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}

	logger.Debugf("Checksum verified for %s", assetName)
	return nil
}
