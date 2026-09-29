package mirror

import (
	"bytes"
	"errors"
	"fmt"

	"noraegaori/internal/download"
)

const GitHubAPI = "https://api.github.com"

var ErrNoBuild = errors.New("no build for this platform")

type Platform struct {
	GOOS   string
	GOARCH string
}

func (target Platform) String() string {
	return target.GOOS + "/" + target.GOARCH
}

func (target Platform) Executable(name string) string {
	if target.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

type ArchiveKind int

const (
	ZipArchive ArchiveKind = iota
	TarXzArchive
)

type Candidate struct {
	Version string
	URL     string
	SHA256  string
	Archive ArchiveKind
	Members []string
}

type Mirror interface {
	Name() string
	Find(target Platform, accepts func(version string) bool) (*Candidate, error)
}

func checksumFor(checksumFile []byte, assetName string) (string, error) {
	checksums, err := download.ParseChecksums(bytes.NewReader(checksumFile))
	if err != nil {
		return "", err
	}
	sum, ok := checksums[assetName]
	if !ok {
		return "", fmt.Errorf("the checksum file has no entry for %s", assetName)
	}
	return sum, nil
}

type checksumReader func(checksumFile []byte, assetName string) (string, error)

func fetchChecksum(release *download.Release, checksumAsset, assetName string, read checksumReader) (string, error) {
	sumURL := release.AssetURL(checksumAsset)
	if sumURL == "" {
		return "", fmt.Errorf("release %s has no %s", release.TagName, checksumAsset)
	}
	checksumFile, err := download.FetchBytes(sumURL)
	if err != nil {
		return "", err
	}
	sum, err := read(checksumFile, assetName)
	if err != nil {
		return "", err
	}
	if err := download.CheckDigest(assetName, sum, release.AssetDigest(assetName)); err != nil {
		return "", err
	}
	return sum, nil
}
