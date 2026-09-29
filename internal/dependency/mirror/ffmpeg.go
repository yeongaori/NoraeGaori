package mirror

import (
	"fmt"
	"time"

	"noraegaori/internal/download"
)

const FFmpegVersionLayout = "2006.01.02.1504"

var ffmpegPlatforms = map[string]string{
	"linux/amd64":   "linux64",
	"linux/arm64":   "linuxarm64",
	"linux/386":     "linux32",
	"windows/amd64": "win64",
	"windows/386":   "win32",
	"windows/arm64": "winarm64",
}

type FFmpegGitHub struct {
	APIURL string
	WebURL string
	Repo   string
}

func (m FFmpegGitHub) Name() string {
	return "GitHub " + m.Repo
}

func (m FFmpegGitHub) Find(target Platform, accepts func(string) bool) (*Candidate, error) {
	release, err := download.GitHubRepo{APIURL: m.APIURL, WebURL: m.WebURL, Repo: m.Repo}.FetchLatest("checksums.sha256")
	if err != nil {
		return nil, err
	}

	published, err := time.Parse(time.RFC3339, release.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("release %s has no publish time: %w", release.TagName, err)
	}
	version := published.UTC().Format(FFmpegVersionLayout)

	name, ok := ffmpegPlatforms[target.String()]
	if !ok || !accepts(version) {
		return nil, ErrNoBuild
	}

	folder := fmt.Sprintf("ffmpeg-master-latest-%s-gpl", name)
	extension, archive := ".tar.xz", TarXzArchive
	if target.GOOS == "windows" {
		extension, archive = ".zip", ZipArchive
	}
	assetName := folder + extension

	asset, ok := release.FindAsset(assetName)
	if !ok {
		return nil, ErrNoBuild
	}
	sum, err := fetchChecksum(release, "checksums.sha256", assetName, checksumFor)
	if err != nil {
		return nil, err
	}

	return &Candidate{
		Version: version,
		URL:     asset.BrowserDownloadURL,
		SHA256:  sum,
		Archive: archive,
		Members: []string{
			folder + "/bin/" + target.Executable("ffmpeg"),
			folder + "/bin/" + target.Executable("ffprobe"),
		},
	}, nil
}
