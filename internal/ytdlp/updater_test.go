package ytdlp

import (
	"strings"
	"testing"
)

const sampleChecksums = `495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd  yt-dlp
52fe3c26dcf71fbdc85b528589020bb0b8e383155cfa81b64dd447bbe35e24b8  yt-dlp.exe

b6ce97646773070d7a7ffd6bbbdcaecb47c48483909c54c915bf08a7a9b5e0b1 *yt-dlp_linux_aarch64
`

func TestParseChecksums(t *testing.T) {
	checksums, err := parseChecksums(strings.NewReader(sampleChecksums))
	if err != nil {
		t.Fatalf("got error %v, want nil", err)
	}

	want := map[string]string{
		"yt-dlp":               "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd",
		"yt-dlp.exe":           "52fe3c26dcf71fbdc85b528589020bb0b8e383155cfa81b64dd447bbe35e24b8",
		"yt-dlp_linux_aarch64": "b6ce97646773070d7a7ffd6bbbdcaecb47c48483909c54c915bf08a7a9b5e0b1",
	}

	if len(checksums) != len(want) {
		t.Fatalf("got %d entries, want %d", len(checksums), len(want))
	}
	for name, sum := range want {
		if checksums[name] != sum {
			t.Errorf("%s: got %q, want %q", name, checksums[name], sum)
		}
	}
}

func TestParseChecksumsRejectsMalformedInput(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"missing name":  "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd\n",
		"extra field":   "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd  yt-dlp  extra\n",
		"not hex":       "zzzbe29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd  yt-dlp\n",
		"wrong length":  "495be29ff4d9d4e9be7eabdfef2252  yt-dlp\n",
		"sha512 digest": strings.Repeat("a", 128) + "  yt-dlp\n",
	}

	for name, input := range cases {
		if _, err := parseChecksums(strings.NewReader(input)); err == nil {
			t.Errorf("%s: got nil error, want a rejection", name)
		}
	}
}

func TestExpectedChecksumAcceptsAnAgreeingDigest(t *testing.T) {
	payload := []byte("yt-dlp binary")
	release, _ := serveRelease(t, payload, sha256Hex(payload))
	release.Assets[0].Digest = checksumDigestPrefix + strings.ToUpper(sha256Hex(payload))

	sum, err := ExpectedChecksum(release, testAssetName)
	if err != nil {
		t.Fatalf("ExpectedChecksum returned %v, want nil", err)
	}
	if sum != sha256Hex(payload) {
		t.Errorf("got %q, want %q", sum, sha256Hex(payload))
	}
}

func TestExpectedChecksumFailsClosedWithoutSource(t *testing.T) {
	release := &GitHubRelease{TagName: "2026.07.04"}

	if _, err := ExpectedChecksum(release, "yt-dlp"); err == nil {
		t.Error("got nil error, want a failure when neither a digest nor a sums asset exists")
	}
}

func TestDownloadRateLimitFallsBackToDefault(t *testing.T) {
	want := defaultDownloadMbps * 1000 * 1000 / 8
	if got := downloadRateLimit(); got != want {
		t.Errorf("got %v bytes/sec, want %v", got, want)
	}
}
