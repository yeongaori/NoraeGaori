package download_test

import (
	"strings"
	"testing"

	"noraegaori/internal/download"
)

const sampleChecksums = `495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd  yt-dlp
52FE3C26DCF71FBDC85B528589020BB0B8E383155CFA81B64DD447BBE35E24B8  yt-dlp.exe

b6ce97646773070d7a7ffd6bbbdcaecb47c48483909c54c915bf08a7a9b5e0b1 *yt-dlp_linux_aarch64
`

func TestParseChecksums(t *testing.T) {
	checksums, err := download.ParseChecksums(strings.NewReader(sampleChecksums))
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
		if _, err := download.ParseChecksums(strings.NewReader(input)); err == nil {
			t.Errorf("%s: got nil error, want a rejection", name)
		}
	}
}

func TestCheckDigestAcceptsAMatchingOrMissingDigest(t *testing.T) {
	sum := "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd"

	if err := download.CheckDigest("deno.zip", sum, sum); err != nil {
		t.Errorf("got %v for a matching digest, want nil", err)
	}
	if err := download.CheckDigest("deno.zip", sum, ""); err != nil {
		t.Errorf("got %v when the release has no digest, want nil", err)
	}
}

func TestCheckDigestRejectsADisagreeingDigest(t *testing.T) {
	published := "495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd"
	digest := "52fe3c26dcf71fbdc85b528589020bb0b8e383155cfa81b64dd447bbe35e24b8"

	err := download.CheckDigest("deno.zip", published, digest)
	if err == nil {
		t.Fatal("got nil, want a rejection when the digest contradicts the published checksum")
	}
	if !strings.Contains(err.Error(), "deno.zip") {
		t.Errorf("got %q, want the asset named", err)
	}
}
