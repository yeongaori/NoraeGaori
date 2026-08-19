package ytdlp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

const testAssetName = "yt-dlp-test-binary"

func addAsset(release *GitHubRelease, name, url, digest string) {
	release.Assets = append(release.Assets, struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
		Digest             string `json:"digest"`
	}{Name: name, BrowserDownloadURL: url, Digest: digest})
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func useTestSigningKey(t *testing.T) *openpgp.Entity {
	t.Helper()

	entity, err := openpgp.NewEntity("NoraeGaori Test", "signing", "test@example.invalid", nil)
	if err != nil {
		t.Fatalf("failed to generate a test signing key: %v", err)
	}

	var publicKey bytes.Buffer
	encoder, err := armor.Encode(&publicKey, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("failed to armor the test key: %v", err)
	}
	if err := entity.Serialize(encoder); err != nil {
		t.Fatalf("failed to serialize the test key: %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("failed to close the armor encoder: %v", err)
	}

	previous := signingKeyArmor
	signingKeyArmor = publicKey.Bytes()
	t.Cleanup(func() { signingKeyArmor = previous })

	return entity
}

func signChecksums(t *testing.T, entity *openpgp.Entity, checksums []byte) []byte {
	t.Helper()

	var signature bytes.Buffer
	if err := openpgp.DetachSign(&signature, entity, bytes.NewReader(checksums), nil); err != nil {
		t.Fatalf("failed to sign the test checksums: %v", err)
	}
	return signature.Bytes()
}

func serveRelease(t *testing.T, payload []byte, claimedSum string) (*GitHubRelease, string) {
	t.Helper()

	entity := useTestSigningKey(t)

	var checksums, signature []byte
	if claimedSum != "" {
		checksums = []byte(fmt.Sprintf("%s  %s\n", claimedSum, testAssetName))
		signature = signChecksums(t, entity, checksums)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+testAssetName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	})
	mux.HandleFunc("/"+checksumAssetName, func(w http.ResponseWriter, r *http.Request) {
		if checksums == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(checksums)
	})
	mux.HandleFunc("/"+checksumSigAssetName, func(w http.ResponseWriter, r *http.Request) {
		if signature == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(signature)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	release := &GitHubRelease{TagName: "2026.07.04"}
	addAsset(release, testAssetName, server.URL+"/"+testAssetName, "")
	if claimedSum != "" {
		addAsset(release, checksumAssetName, server.URL+"/"+checksumAssetName, "")
		addAsset(release, checksumSigAssetName, server.URL+"/"+checksumSigAssetName, "")
	}

	return release, filepath.Join(t.TempDir(), "downloaded")
}

func TestDownloadFileReturnsContentDigest(t *testing.T) {
	payload := []byte("yt-dlp binary contents")
	release, destination := serveRelease(t, payload, sha256Hex(payload))

	sum, err := DownloadFile(release.Assets[0].BrowserDownloadURL, destination)
	if err != nil {
		t.Fatalf("DownloadFile returned %v, want nil", err)
	}

	if sum != sha256Hex(payload) {
		t.Errorf("got digest %q, want %q", sum, sha256Hex(payload))
	}

	written, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("failed to read the downloaded file: %v", err)
	}
	if string(written) != string(payload) {
		t.Errorf("got file contents %q, want %q", written, payload)
	}
}

func TestDownloadVerifiedAcceptsMatchingChecksum(t *testing.T) {
	payload := []byte("trusted yt-dlp binary")
	release, destination := serveRelease(t, payload, sha256Hex(payload))

	if err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL, destination); err != nil {
		t.Fatalf("DownloadVerified returned %v, want nil", err)
	}

	written, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("failed to read the verified file: %v", err)
	}
	if string(written) != string(payload) {
		t.Errorf("got file contents %q, want %q", written, payload)
	}
}

func TestDownloadVerifiedRejectsTamperedPayload(t *testing.T) {
	payload := []byte("tampered yt-dlp binary")
	expected := sha256Hex([]byte("the binary the release promised"))
	release, destination := serveRelease(t, payload, expected)

	err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL, destination)
	if err == nil {
		t.Fatal("DownloadVerified returned nil, want a rejection for mismatched content")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q does not identify a checksum mismatch", err)
	}

	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Error("the rejected download was left on disk, so it remains reachable for chmod and exec")
	}
}

func TestDownloadVerifiedRejectsDigestDisagreeingWithSignedSums(t *testing.T) {
	payload := []byte("trusted yt-dlp binary")
	release, destination := serveRelease(t, payload, sha256Hex(payload))
	release.Assets[0].Digest = checksumDigestPrefix + sha256Hex([]byte("something else"))

	err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL, destination)
	if err == nil {
		t.Fatal("DownloadVerified returned nil, want a rejection when the digest contradicts the signed sums")
	}
	if !strings.Contains(err.Error(), "disagrees with the signed") {
		t.Errorf("error %q does not identify the digest disagreement", err)
	}

	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Error("the binary was downloaded despite a contradictory digest")
	}
}

func TestDownloadVerifiedRejectsAnUnsignedChecksumFile(t *testing.T) {
	payload := []byte("yt-dlp binary")
	release, destination := serveRelease(t, payload, sha256Hex(payload))

	release.Assets = release.Assets[:len(release.Assets)-1]

	err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL, destination)
	if err == nil {
		t.Fatal("DownloadVerified returned nil, want a refusal when the checksum file carries no signature")
	}
	if !strings.Contains(err.Error(), checksumSigAssetName) {
		t.Errorf("error %q does not name the missing signature asset", err)
	}

	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Error("the binary was downloaded despite an unsigned checksum file")
	}
}

func TestDownloadVerifiedRejectsAForeignSignature(t *testing.T) {
	payload := []byte("yt-dlp binary")
	release, destination := serveRelease(t, payload, sha256Hex(payload))

	useTestSigningKey(t)

	err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL, destination)
	if err == nil {
		t.Fatal("DownloadVerified returned nil, want a refusal when the sums are signed by an unknown key")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("error %q does not identify a signature failure", err)
	}

	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Error("the binary was downloaded despite an untrusted signature")
	}
}

func TestDownloadVerifiedFailsClosedWithoutChecksumSource(t *testing.T) {
	payload := []byte("unverifiable yt-dlp binary")
	release, destination := serveRelease(t, payload, "")

	err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL, destination)
	if err == nil {
		t.Fatal("DownloadVerified returned nil, want a refusal when no checksum is published")
	}

	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Error("the binary was downloaded despite having no checksum to verify it against")
	}
}

func TestDownloadVerifiedRejectsFailedDownload(t *testing.T) {
	payload := []byte("yt-dlp binary")
	release, destination := serveRelease(t, payload, sha256Hex(payload))

	err := DownloadVerified(release, testAssetName, release.Assets[0].BrowserDownloadURL+"-missing", destination)
	if err == nil {
		t.Fatal("DownloadVerified returned nil, want an error for a failed download")
	}
}

func TestGetDownloadAssetMatchesThisPlatform(t *testing.T) {
	release := &GitHubRelease{TagName: "2026.07.04"}
	for _, name := range []string{"yt-dlp", "yt-dlp.exe", "yt-dlp_macos", "yt-dlp_linux_aarch64"} {
		addAsset(release, name, "https://example.invalid/"+name, "")
	}

	name, url, err := GetDownloadAsset(release)
	if err != nil {
		t.Fatalf("GetDownloadAsset returned %v, want nil", err)
	}
	if name == "" {
		t.Fatal("got an empty asset name")
	}
	if url != "https://example.invalid/"+name {
		t.Errorf("got url %q, want the url of asset %q", url, name)
	}
}

func TestGetDownloadAssetFailsWhenAssetIsAbsent(t *testing.T) {
	release := &GitHubRelease{TagName: "2026.07.04"}
	addAsset(release, "unrelated-asset", "https://example.invalid/unrelated", "")

	if _, _, err := GetDownloadAsset(release); err == nil {
		t.Error("GetDownloadAsset returned nil, want an error when no asset matches")
	}
}
