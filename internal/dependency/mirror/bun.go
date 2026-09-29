package mirror

import (
	"fmt"
	"strings"

	"noraegaori/internal/download"
)

const (
	bunReleasesPerPage = 100
	bunMaximumPages    = 5
)

var (
	bunArchitectures = map[string]string{"amd64": "x64", "arm64": "aarch64", "386": "x86"}
	bunSystems       = map[string]string{"linux": "linux", "windows": "windows", "darwin": "darwin"}
)

type BunGitHub struct {
	APIURL string
}

func (m BunGitHub) Name() string {
	return "GitHub oven-sh/bun"
}

func (m BunGitHub) Find(target Platform, accepts func(string) bool) (*Candidate, error) {
	architecture, ok := bunArchitectures[target.GOARCH]
	if !ok {
		return nil, ErrNoBuild
	}
	system, ok := bunSystems[target.GOOS]
	if !ok {
		return nil, ErrNoBuild
	}
	folder := fmt.Sprintf("bun-%s-%s", system, architecture)
	assetName := folder + ".zip"

	for page := 1; page <= bunMaximumPages; page++ {
		releases, err := download.FetchReleases(fmt.Sprintf("%s/repos/oven-sh/bun/releases?per_page=%d&page=%d", m.APIURL, bunReleasesPerPage, page))
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			break
		}

		for _, release := range releases {
			version := strings.TrimPrefix(release.TagName, "bun-v")
			asset, ok := release.FindAsset(assetName)
			if !ok || !accepts(version) {
				continue
			}

			sum, err := fetchChecksum(release, "SHASUMS256.txt", assetName, checksumFor)
			if err != nil {
				return nil, err
			}
			return &Candidate{
				Version: version,
				URL:     asset.BrowserDownloadURL,
				SHA256:  sum,
				Archive: ZipArchive,
				Members: []string{folder + "/" + target.Executable("bun")},
			}, nil
		}
	}

	return nil, ErrNoBuild
}
