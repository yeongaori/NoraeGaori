package download

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

func ParseChecksums(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed checksum line: %q", line)
		}

		sum := strings.ToLower(fields[0])
		decoded, err := hex.DecodeString(sum)
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("malformed checksum for %s: %q", fields[1], fields[0])
		}

		checksums[strings.TrimPrefix(fields[1], "*")] = sum
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read checksums: %w", err)
	}

	if len(checksums) == 0 {
		return nil, fmt.Errorf("the checksum file contained no entries")
	}

	return checksums, nil
}

func CheckDigest(assetName, published, digest string) error {
	if digest != "" && digest != published {
		return fmt.Errorf("the GitHub asset digest disagrees with the published checksum for %s: %s vs %s", assetName, digest, published)
	}
	return nil
}
