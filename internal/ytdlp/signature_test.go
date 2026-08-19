package ytdlp

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadSignedFixtures(t *testing.T) (checksums, signature []byte) {
	t.Helper()

	checksums, err := os.ReadFile(filepath.Join("testdata", "SHA2-256SUMS"))
	if err != nil {
		t.Fatalf("failed to read the checksum fixture: %v", err)
	}

	encoded, err := os.ReadFile(filepath.Join("testdata", "SHA2-256SUMS.sig.base64"))
	if err != nil {
		t.Fatalf("failed to read the signature fixture: %v", err)
	}

	signature, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(encoded)), ""))
	if err != nil {
		t.Fatalf("failed to decode the signature fixture: %v", err)
	}

	return checksums, signature
}

func TestSigningKeyIsEmbedded(t *testing.T) {
	if len(ytdlpSigningKey) == 0 {
		t.Fatal("ytdlpSigningKey is empty, so the //go:embed directive above it was lost")
	}
	if !bytes.Contains(ytdlpSigningKey, []byte("BEGIN PGP PUBLIC KEY BLOCK")) {
		t.Error("the embedded key is not an armored PGP public key block")
	}
}

func TestVerifyChecksumSignatureAcceptsTheRealSignature(t *testing.T) {
	checksums, signature := loadSignedFixtures(t)

	if err := VerifyChecksumSignature(checksums, signature); err != nil {
		t.Fatalf("VerifyChecksumSignature returned %v, want nil for a genuine signature", err)
	}
}

func TestVerifyChecksumSignatureRejectsTamperedChecksums(t *testing.T) {
	checksums, signature := loadSignedFixtures(t)

	tampered := bytes.Replace(checksums,
		[]byte("495be29ff4d9d4e9be7eabdfef225221e5d5282e77f2f505abc6dca80349f3fd"),
		[]byte("0000000000000000000000000000000000000000000000000000000000000000"), 1)
	if bytes.Equal(tampered, checksums) {
		t.Fatal("the fixture was not modified, so this test would pass vacuously")
	}

	if err := VerifyChecksumSignature(tampered, signature); err == nil {
		t.Error("VerifyChecksumSignature accepted a tampered checksum file")
	}
}

func TestVerifyChecksumSignatureRejectsASingleFlippedByte(t *testing.T) {
	checksums, signature := loadSignedFixtures(t)

	tampered := make([]byte, len(checksums))
	copy(tampered, checksums)
	tampered[0] ^= 0x01

	if err := VerifyChecksumSignature(tampered, signature); err == nil {
		t.Error("VerifyChecksumSignature accepted a checksum file with one flipped byte")
	}
}

func TestVerifyChecksumSignatureRejectsCorruptSignature(t *testing.T) {
	checksums, signature := loadSignedFixtures(t)

	cases := map[string][]byte{
		"empty":     {},
		"truncated": signature[:len(signature)/2],
		"garbage":   bytes.Repeat([]byte{0xAB}, len(signature)),
	}

	for name, sig := range cases {
		if err := VerifyChecksumSignature(checksums, sig); err == nil {
			t.Errorf("%s signature was accepted, want a rejection", name)
		}
	}
}

func TestVerifyChecksumSignatureRejectsAForeignSignature(t *testing.T) {
	checksums, signature := loadSignedFixtures(t)

	flipped := make([]byte, len(signature))
	copy(flipped, signature)
	flipped[len(flipped)-1] ^= 0xFF

	if err := VerifyChecksumSignature(checksums, flipped); err == nil {
		t.Error("a signature with a corrupted trailing byte was accepted")
	}
}

func TestFixtureChecksumsParseAndCoverEveryPlatform(t *testing.T) {
	checksums, _ := loadSignedFixtures(t)

	parsed, err := parseChecksums(bytes.NewReader(checksums))
	if err != nil {
		t.Fatalf("parseChecksums returned %v, want nil", err)
	}

	for _, asset := range []string{"yt-dlp", "yt-dlp.exe", "yt-dlp_macos", "yt-dlp_linux_aarch64"} {
		sum, ok := parsed[asset]
		if !ok {
			t.Errorf("the signed checksum file has no entry for %s", asset)
			continue
		}
		if len(sum) != 64 {
			t.Errorf("%s has checksum %q, want 64 hex characters", asset, sum)
		}
	}
}
