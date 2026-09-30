package download_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"noraegaori/internal/download"
)

func sampleRelease() *download.Release {
	return &download.Release{
		TagName: "v2.9.7",
		Assets: []download.Asset{
			{Name: "deno-x86_64-unknown-linux-gnu.zip", BrowserDownloadURL: "https://example.invalid/linux", Digest: "sha256:ABCDEF"},
			{Name: "deno-x86_64-pc-windows-msvc.zip", BrowserDownloadURL: "https://example.invalid/windows", Digest: "sha512:abcdef"},
		},
	}
}

func TestFindAssetReturnsTheNamedAsset(t *testing.T) {
	release := sampleRelease()

	asset, ok := release.FindAsset("deno-x86_64-pc-windows-msvc.zip")
	if !ok || asset.BrowserDownloadURL != "https://example.invalid/windows" {
		t.Errorf("got %v, %v, want the windows asset", asset, ok)
	}
	if _, ok := release.FindAsset("deno-i686-unknown-linux-gnu.zip"); ok {
		t.Error("got ok for an asset the release does not publish")
	}
}

func TestAssetURLIsEmptyForAMissingAsset(t *testing.T) {
	release := sampleRelease()

	if got := release.AssetURL("deno-x86_64-unknown-linux-gnu.zip"); got != "https://example.invalid/linux" {
		t.Errorf("got %q, want the linux asset URL", got)
	}
	if got := release.AssetURL("missing.zip"); got != "" {
		t.Errorf("got %q, want an empty URL", got)
	}
}

func TestAssetDigestReadsOnlySHA256Digests(t *testing.T) {
	release := sampleRelease()

	if got := release.AssetDigest("deno-x86_64-unknown-linux-gnu.zip"); got != "abcdef" {
		t.Errorf("got %q, want the lower-cased sha256 digest", got)
	}
	if got := release.AssetDigest("deno-x86_64-pc-windows-msvc.zip"); got != "" {
		t.Errorf("got %q, want no digest for a sha512 value", got)
	}
	if got := release.AssetDigest("missing.zip"); got != "" {
		t.Errorf("got %q, want no digest for a missing asset", got)
	}
}

func serveBody(t *testing.T, status int, body string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != download.HookUserAgent {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestFetchReleaseDecodesTheRelease(t *testing.T) {
	url := serveBody(t, http.StatusOK, `{"tag_name":"v2.9.7","name":"v2.9.7","published_at":"2026-09-24T12:28:11Z","assets":[{"name":"deno.zip","browser_download_url":"https://example.invalid/deno.zip","size":41,"digest":"sha256:ab"}]}`)

	release, err := download.FetchRelease(url)
	if err != nil {
		t.Fatalf("FetchRelease returned %v, want nil", err)
	}
	if release.TagName != "v2.9.7" || release.PublishedAt != "2026-09-24T12:28:11Z" {
		t.Errorf("got %+v, want the tag and publish time decoded", release)
	}
	if len(release.Assets) != 1 || release.Assets[0].Size != 41 || release.Assets[0].Digest != "sha256:ab" {
		t.Errorf("got assets %+v, want the one asset decoded", release.Assets)
	}
}

func TestFetchReleasesDecodesTheList(t *testing.T) {
	url := serveBody(t, http.StatusOK, `[{"tag_name":"bun-v1.4.2"},{"tag_name":"bun-v1.3.14"}]`)

	releases, err := download.FetchReleases(url)
	if err != nil {
		t.Fatalf("FetchReleases returned %v, want nil", err)
	}
	if len(releases) != 2 || releases[1].TagName != "bun-v1.3.14" {
		t.Errorf("got %v, want both releases in order", releases)
	}
}

func TestFetchReportsTheHTTPStatus(t *testing.T) {
	url := serveBody(t, http.StatusNotFound, "")

	_, err := download.FetchBytes(url)
	var statusErr *download.StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusNotFound {
		t.Fatalf("got %v, want a StatusError with 404", err)
	}
	if got := statusErr.Error(); got != url+" returned status 404" {
		t.Errorf("got %q, want the URL and status named", got)
	}
	if _, err := download.FetchRelease(url); !errors.As(err, &statusErr) {
		t.Errorf("got %v from FetchRelease, want a StatusError", err)
	}
	if _, err := download.FetchReleases(url); !errors.As(err, &statusErr) {
		t.Errorf("got %v from FetchReleases, want a StatusError", err)
	}
}

func TestFetchJSONRejectsMalformedBodies(t *testing.T) {
	url := serveBody(t, http.StatusOK, "not json")

	var target map[string]string
	err := download.FetchJSON(url, &target)
	var statusErr *download.StatusError
	if err == nil || errors.As(err, &statusErr) {
		t.Errorf("got %v, want a parse error", err)
	}
}

func TestFetchBytesReturnsTheBody(t *testing.T) {
	url := serveBody(t, http.StatusOK, "v2.9.7\n")

	body, err := download.FetchBytes(url)
	if err != nil || string(body) != "v2.9.7\n" {
		t.Errorf("got %q, %v, want the body", body, err)
	}
}

func TestFetchRejectsAMalformedURL(t *testing.T) {
	if _, err := download.FetchBytes("://no-scheme"); err == nil {
		t.Error("got nil, want an error for a malformed URL")
	}
}

func TestFetchFailsOnAnUnreachableHost(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	_, err := download.FetchBytes(url)
	var statusErr *download.StatusError
	if err == nil || errors.As(err, &statusErr) {
		t.Errorf("got %v, want a connection error rather than a status", err)
	}
}
