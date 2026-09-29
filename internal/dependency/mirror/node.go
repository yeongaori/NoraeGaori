package mirror

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"noraegaori/internal/download"
)

var (
	nodeArchitectures = map[string]string{"amd64": "x64", "arm64": "arm64", "386": "x86"}
	nodeSystems       = map[string]string{"linux": "linux", "windows": "win", "darwin": "darwin"}
)

type nodeRelease struct {
	Version string          `json:"version"`
	Files   []string        `json:"files"`
	LTS     json.RawMessage `json:"lts"`
}

func (release nodeRelease) isLTS() bool {
	return len(release.LTS) > 0 && string(release.LTS) != "false"
}

type NodeDist struct {
	BaseURL string
}

func (m NodeDist) Name() string {
	return "nodejs.org"
}

func (m NodeDist) Find(target Platform, accepts func(string) bool) (*Candidate, error) {
	architecture, ok := nodeArchitectures[target.GOARCH]
	if !ok {
		return nil, ErrNoBuild
	}
	system, ok := nodeSystems[target.GOOS]
	if !ok {
		return nil, ErrNoBuild
	}

	fileKey := system + "-" + architecture
	extension, archive := ".tar.xz", TarXzArchive
	binaryPath := "bin/node"
	if target.GOOS == "windows" {
		fileKey += "-zip"
		extension, archive = ".zip", ZipArchive
		binaryPath = "node.exe"
	}

	var releases []nodeRelease
	if err := download.FetchJSON(m.BaseURL+"/index.json", &releases); err != nil {
		return nil, err
	}

	for _, release := range releases {
		version := strings.TrimPrefix(release.Version, "v")
		if !release.isLTS() || !accepts(version) || !slices.Contains(release.Files, fileKey) {
			continue
		}

		folder := fmt.Sprintf("node-%s-%s-%s", release.Version, system, architecture)
		assetName := folder + extension
		checksumFile, err := download.FetchBytes(fmt.Sprintf("%s/%s/SHASUMS256.txt", m.BaseURL, release.Version))
		if err != nil {
			return nil, err
		}
		sum, err := checksumFor(checksumFile, assetName)
		if err != nil {
			return nil, err
		}

		return &Candidate{
			Version: version,
			URL:     fmt.Sprintf("%s/%s/%s", m.BaseURL, release.Version, assetName),
			SHA256:  sum,
			Archive: archive,
			Members: []string{folder + "/" + binaryPath},
		}, nil
	}

	return nil, ErrNoBuild
}
