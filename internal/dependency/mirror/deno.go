package mirror

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"noraegaori/internal/download"
)

var (
	denoArchitectures = map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "i686"}
	denoSystems       = map[string]string{"linux": "unknown-linux-gnu", "windows": "pc-windows-msvc", "darwin": "apple-darwin"}
)

func denoAssetName(target Platform) (string, bool) {
	architecture, ok := denoArchitectures[target.GOARCH]
	if !ok {
		return "", false
	}
	system, ok := denoSystems[target.GOOS]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("deno-%s-%s.zip", architecture, system), true
}

func denoCandidate(version, url, sum string, target Platform) *Candidate {
	return &Candidate{
		Version: version,
		URL:     url,
		SHA256:  sum,
		Archive: ZipArchive,
		Members: []string{target.Executable("deno")},
	}
}

type DenoGitHub struct {
	APIURL string
}

func (m DenoGitHub) Name() string {
	return "GitHub denoland/deno"
}

func (m DenoGitHub) Find(target Platform, accepts func(string) bool) (*Candidate, error) {
	release, err := download.FetchRelease(m.APIURL + "/repos/denoland/deno/releases/latest")
	if err != nil {
		return nil, err
	}

	version := strings.TrimPrefix(release.TagName, "v")
	assetName, ok := denoAssetName(target)
	if !ok || !accepts(version) {
		return nil, ErrNoBuild
	}
	asset, ok := release.FindAsset(assetName)
	if !ok {
		return nil, ErrNoBuild
	}

	sum, err := fetchChecksum(release, assetName+".sha256sum", assetName, denoChecksumFor)
	if err != nil {
		return nil, err
	}
	return denoCandidate(version, asset.BrowserDownloadURL, sum, target), nil
}

type DenoCDN struct {
	BaseURL string
}

func (m DenoCDN) Name() string {
	return "dl.deno.land"
}

func (m DenoCDN) Find(target Platform, accepts func(string) bool) (*Candidate, error) {
	latest, err := download.FetchBytes(m.BaseURL + "/release-latest.txt")
	if err != nil {
		return nil, err
	}

	tag := strings.TrimSpace(string(latest))
	version := strings.TrimPrefix(tag, "v")
	assetName, ok := denoAssetName(target)
	if !ok || !accepts(version) {
		return nil, ErrNoBuild
	}

	url := fmt.Sprintf("%s/release/%s/%s", m.BaseURL, tag, assetName)
	checksumFile, err := download.FetchBytes(url + ".sha256sum")
	var statusErr *download.StatusError
	if errors.As(err, &statusErr) && statusErr.Code == http.StatusNotFound {
		return nil, ErrNoBuild
	}
	if err != nil {
		return nil, err
	}
	sum, err := denoChecksumFor(checksumFile, assetName)
	if err != nil {
		return nil, err
	}

	return denoCandidate(version, url, sum, target), nil
}

func denoChecksumFor(checksumFile []byte, assetName string) (string, error) {
	if !bytes.Contains(checksumFile, []byte("Algorithm")) {
		return checksumFor(checksumFile, assetName)
	}

	fields := make(map[string]string, 3)
	for _, line := range strings.Split(string(checksumFile), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	if fields["Algorithm"] != "SHA256" {
		return "", fmt.Errorf("the checksum for %s uses %q, want SHA256", assetName, fields["Algorithm"])
	}
	path := fields["Path"]
	if path[strings.LastIndexAny(path, `\/`)+1:] != assetName {
		return "", fmt.Errorf("the checksum file has no entry for %s", assetName)
	}

	sum := strings.ToLower(fields["Hash"])
	decoded, err := hex.DecodeString(sum)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("malformed checksum for %s: %q", assetName, fields["Hash"])
	}
	return sum, nil
}
