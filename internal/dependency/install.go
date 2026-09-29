package dependency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/download"
	"noraegaori/internal/logger"
)

const (
	libDirectory        = "lib"
	partialSuffix       = ".part"
	archiveName         = "archive"
	versionCheckTimeout = 15 * time.Second
)

var versionPattern = regexp.MustCompile(`\d+(\.\d+)+`)

func compareVersions(a, b string) int {
	left := versionNumbers(a)
	right := versionNumbers(b)
	for i := range max(len(left), len(right)) {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			if l < r {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionNumbers(version string) []int {
	fields := strings.FieldsFunc(version, func(r rune) bool { return r < '0' || r > '9' })
	numbers := make([]int, 0, len(fields))
	for _, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		numbers = append(numbers, number)
	}
	return numbers
}

func runVersionCommand(binaryPath, flag string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionCheckTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, binaryPath, flag).Output()
	return string(output), err
}

func readVersion(binaryPath, flag string) (string, error) {
	output, err := runVersionCommand(binaryPath, flag)
	if err != nil {
		return "", err
	}

	version := versionPattern.FindString(output)
	if version == "" {
		return "", fmt.Errorf("%s printed no version", binaryPath)
	}
	return version, nil
}

func installDirectory(toolName, version string) string {
	return filepath.Join(libDirectory, toolName+"-"+version)
}

func installedBinary(t *tool, target mirror.Platform, version string) (*Binary, bool) {
	binaryPath, err := filepath.Abs(filepath.Join(installDirectory(t.name, version), target.Executable(t.name)))
	if err != nil {
		return nil, false
	}
	if _, err := runVersionCommand(binaryPath, t.versionFlag); err != nil {
		return nil, false
	}
	return &Binary{Tool: t.name, Version: version, Path: binaryPath}, true
}

func installedVersions(toolName string) []string {
	matches, _ := filepath.Glob(filepath.Join(libDirectory, toolName+"-*"))

	prefix := toolName + "-"
	versions := make([]string, 0, len(matches))
	for _, match := range matches {
		name := filepath.Base(match)
		if strings.HasSuffix(name, partialSuffix) {
			continue
		}
		versions = append(versions, strings.TrimPrefix(name, prefix))
	}

	slices.SortFunc(versions, func(a, b string) int { return compareVersions(b, a) })
	return versions
}

func newestInstalled(t *tool, target mirror.Platform) (*Binary, bool) {
	for _, version := range installedVersions(t.name) {
		if binary, ok := installedBinary(t, target, version); ok {
			return binary, true
		}
	}
	return nil, false
}

func installFunc(target mirror.Platform) acceptFunc {
	return func(t *tool, found *mirror.Candidate) (*Binary, error) {
		return install(t, target, found)
	}
}

func install(t *tool, target mirror.Platform, found *mirror.Candidate) (*Binary, error) {
	if binary, ok := installedBinary(t, target, found.Version); ok {
		return binary, nil
	}

	directory := installDirectory(t.name, found.Version)
	partial := directory + partialSuffix
	isDownloaded := hasVerifiedArchive(filepath.Join(partial, archiveName), found.SHA256)
	if err := preparePartial(partial, isDownloaded); err != nil {
		return nil, err
	}
	if isDownloaded {
		logger.Infof("Reusing the downloaded %s", path.Base(found.URL))
	}

	if err := installInto(t, target, found, partial, isDownloaded); err != nil {
		os.RemoveAll(partial)
		return nil, err
	}

	if err := os.RemoveAll(directory); err != nil {
		os.RemoveAll(partial)
		return nil, fmt.Errorf("failed to replace %s: %w", directory, err)
	}
	if err := os.Rename(partial, directory); err != nil {
		os.RemoveAll(partial)
		return nil, fmt.Errorf("failed to move %s into place: %w", directory, err)
	}

	binaryPath, err := filepath.Abs(filepath.Join(directory, target.Executable(t.name)))
	if err != nil {
		return nil, err
	}
	return &Binary{Tool: t.name, Version: found.Version, Path: binaryPath}, nil
}

func hasVerifiedArchive(archivePath, expectedSum string) bool {
	file, err := os.Open(archivePath)
	if err != nil {
		return false
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false
	}
	return hex.EncodeToString(hasher.Sum(nil)) == expectedSum
}

func preparePartial(partial string, isDownloaded bool) error {
	if isDownloaded {
		return clearPartialExcept(partial, archiveName)
	}

	if err := os.RemoveAll(partial); err != nil {
		return fmt.Errorf("failed to clear %s: %w", partial, err)
	}
	if err := os.MkdirAll(partial, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", partial, err)
	}
	return nil
}

func clearPartialExcept(partial, keep string) error {
	entries, err := os.ReadDir(partial)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", partial, err)
	}
	for _, entry := range entries {
		if entry.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(partial, entry.Name())); err != nil {
			return fmt.Errorf("failed to clear %s: %w", partial, err)
		}
	}
	return nil
}

func installInto(t *tool, target mirror.Platform, found *mirror.Candidate, partial string, isDownloaded bool) error {
	archivePath := filepath.Join(partial, archiveName)
	if !isDownloaded {
		sum, err := download.SaveFile(found.URL, archivePath)
		if err != nil {
			return err
		}
		if sum != found.SHA256 {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", path.Base(found.URL), found.SHA256, sum)
		}
	}

	if err := extractWithProgress(path.Base(found.URL), archivePath, found, partial); err != nil {
		return err
	}
	if err := os.Remove(archivePath); err != nil {
		return fmt.Errorf("failed to remove the downloaded archive: %w", err)
	}

	if _, err := runVersionCommand(filepath.Join(partial, target.Executable(t.name)), t.versionFlag); err != nil {
		return fmt.Errorf("the downloaded %s %s does not run: %w", t.name, found.Version, err)
	}
	return nil
}
