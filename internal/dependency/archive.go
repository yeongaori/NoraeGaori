package dependency

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/download"
	"noraegaori/internal/logger"

	"github.com/ulikunitz/xz"
)

const archiveReadBufferSize = 1 << 20

var (
	showProgress = logger.Progress
	endProgress  = logger.EndProgress
)

func extractWithProgress(name, archivePath string, found *mirror.Candidate, destination string) error {
	logger.Infof("Extracting %s", name)
	defer endProgress()

	report := func(percent int) {
		showProgress(percent, fmt.Sprintf("Extracting %s: %d%%", name, percent))
	}
	if err := extractMembers(archivePath, found.Archive, found.Members, destination, report); err != nil {
		return err
	}

	logger.Infof("Extracted %s", name)
	return nil
}

func extractMembers(archivePath string, kind mirror.ArchiveKind, members []string, destination string, report func(percent int)) error {
	wanted := make(map[string]bool, len(members))
	for _, member := range members {
		wanted[member] = true
	}

	var err error
	if kind == mirror.TarXzArchive {
		err = extractTarXz(archivePath, wanted, destination, report)
	} else {
		err = extractZip(archivePath, wanted, destination, report)
	}
	if err != nil {
		return err
	}

	for member, missing := range wanted {
		if missing {
			return fmt.Errorf("the archive has no %s", member)
		}
	}
	return nil
}

func writeMember(source io.Reader, member, destination string, wanted map[string]bool) error {
	out, err := os.OpenFile(filepath.Join(destination, path.Base(member)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path.Base(member), err)
	}

	if _, err := io.Copy(out, source); err != nil {
		out.Close()
		return fmt.Errorf("failed to extract %s: %w", member, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", member, err)
	}

	wanted[member] = false
	return nil
}

func extractZip(archivePath string, wanted map[string]bool, destination string, report func(percent int)) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open the zip archive: %w", err)
	}
	defer func() { _ = reader.Close() }()

	progress := &download.ProgressWriter{Report: report}
	for _, file := range reader.File {
		if wanted[file.Name] {
			progress.Total += int64(file.UncompressedSize64)
		}
	}

	for _, file := range reader.File {
		if !wanted[file.Name] {
			continue
		}

		source, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file.Name, err)
		}
		err = writeMember(io.TeeReader(source, progress), file.Name, destination, wanted)
		source.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarXz(archivePath string, wanted map[string]bool, destination string, report func(percent int)) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open the archive: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to read the archive size: %w", err)
	}
	counted := io.TeeReader(file, &download.ProgressWriter{Total: info.Size(), Report: report})
	buffered := bufio.NewReaderSize(counted, archiveReadBufferSize)

	decompressed, err := xz.NewReader(buffered)
	if err != nil {
		return fmt.Errorf("failed to read the xz stream: %w", err)
	}

	reader := tar.NewReader(decompressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			_, _ = io.Copy(io.Discard, buffered)
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read the tar archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || !wanted[header.Name] {
			continue
		}
		if err := writeMember(reader, header.Name, destination, wanted); err != nil {
			return err
		}
	}
}
