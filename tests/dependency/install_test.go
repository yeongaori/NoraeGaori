package dependency_test

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/dependency"
	"noraegaori/internal/dependency/mirror"
)

func TestCompareVersionsComparesNumerically(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.10.0", "2.9.7", 1},
		{"2.9.7", "2.10.0", -1},
		{"22.0.0", "22.0.0", 0},
		{"22", "22.0.0", 0},
		{"22.0.1", "22", 1},
		{"v24.14.1", "v9.0.0", 1},
		{"2026.09.27.1323", "2026.09.25.1845", 1},
		{"1.3.14", "1.3.15", -1},
		{"99999999999999999999.2", "1", 1},
	}

	for _, c := range cases {
		if got := dependency.HookCompareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestToolAcceptsVersionsInsideItsBounds(t *testing.T) {
	bun := dependency.HookBuildTool(dependency.HookToolFields{Minimum: "1.2.11", Maximum: "1.3.14"})
	cases := map[string]bool{"1.2.10": false, "1.2.11": true, "1.3.14": true, "1.3.15": false, "1.4.2": false}

	for version, want := range cases {
		if got := bun.HookAccepts(version); got != want {
			t.Errorf("accepts(%q) = %v, want %v", version, got, want)
		}
	}
	if !(&dependency.HookTool{}).HookAccepts("0.0.1") {
		t.Error("a tool without bounds rejected a version")
	}
}

func TestReadVersionFindsTheVersionInTheOutput(t *testing.T) {
	directory := t.TempDir()
	deno := writeExecutable(t, filepath.Join(directory, "deno"), script("deno 2.9.7 (stable, release, x86_64-unknown-linux-gnu)"))
	silent := writeExecutable(t, filepath.Join(directory, "silent"), script("no numbers here"))

	if got, err := dependency.HookReadVersion(deno, "--version"); err != nil || got != "2.9.7" {
		t.Errorf("got %q, %v, want 2.9.7", got, err)
	}
	if _, err := dependency.HookReadVersion(silent, "--version"); err == nil {
		t.Error("got nil, want an error when the output has no version")
	}
	if _, err := dependency.HookReadVersion(filepath.Join(directory, "missing"), "--version"); err == nil {
		t.Error("got nil, want an error for a missing binary")
	}
}

func TestInstalledVersionsAreNewestFirstAndSkipPartialInstalls(t *testing.T) {
	useWorkingDirectory(t)
	for _, name := range []string{"deno-2.9.7", "deno-2.10.0", "deno-2.3.0", "deno-2.11.0.part", "denort-9.9.9"} {
		if err := os.MkdirAll(filepath.Join(dependency.HookLibDirectory, name), 0755); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	got := dependency.HookInstalledVersions("deno")
	want := []string{"2.10.0", "2.9.7", "2.3.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNewestInstalledSkipsABrokenNewerVersion(t *testing.T) {
	useWorkingDirectory(t)
	writeExecutable(t, filepath.Join(dependency.HookLibDirectory, "deno-2.10.0", "deno"), failingScript)
	writeExecutable(t, filepath.Join(dependency.HookLibDirectory, "deno-2.9.7", "deno"), script("deno 2.9.7"))

	binary, ok := dependency.HookNewestInstalled(stubTool("deno"), linuxAmd64)
	if !ok || binary.Version != "2.9.7" {
		t.Fatalf("got %+v, %v, want the working 2.9.7", binary, ok)
	}
	if !filepath.IsAbs(binary.Path) || *binary.HookIsSystem() {
		t.Errorf("got %+v, want an absolute path to a downloaded binary", binary)
	}

	useWorkingDirectory(t)
	if _, ok := dependency.HookNewestInstalled(stubTool("deno"), linuxAmd64); ok {
		t.Error("got ok with nothing installed")
	}
}

func TestInstallUnpacksAndVerifiesAZip(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	found, served := denoArchive(t, "2.9.7")

	binary, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}

	want, _ := filepath.Abs(filepath.Join(dependency.HookLibDirectory, "deno-2.9.7", "deno"))
	if binary.Path != want || binary.Version != "2.9.7" || binary.Tool != "deno" {
		t.Errorf("got %+v, want deno 2.9.7 at %s", binary, want)
	}
	entries, _ := os.ReadDir(filepath.Join(dependency.HookLibDirectory, "deno-2.9.7"))
	if len(entries) != 1 {
		t.Errorf("got %d files in the install directory, want only the binary", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dependency.HookLibDirectory, "deno-2.9.7"+dependency.HookPartialSuffix)); !os.IsNotExist(err) {
		t.Error("the partial directory was left behind")
	}

	again, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err != nil || again.Path != binary.Path {
		t.Errorf("got %+v, %v, want the installed binary reused", again, err)
	}
	if served.downloads.Load() != 1 {
		t.Errorf("got %d downloads, want 1 because the second install reuses the first", served.downloads.Load())
	}
}

func TestInstallUnpacksBothFFmpegBinariesFromATarball(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	payload := buildTarXz(t, map[string]string{
		"bundle/bin/ffmpeg":  script("ffmpeg version N-121234-g0123456"),
		"bundle/bin/ffprobe": script("ffprobe version N-121234-g0123456"),
		"bundle/bin/ffplay":  script("ffplay"),
		"bundle/LICENSE.txt": "GPL",
	})
	served := serveArchive(t, payload, http.StatusOK)
	found := &mirror.Candidate{
		Version: "2026.09.25.1845",
		URL:     served.server.URL,
		SHA256:  sha256Hex(payload),
		Archive: mirror.TarXzArchive,
		Members: []string{"bundle/bin/ffmpeg", "bundle/bin/ffprobe"},
	}

	binary, err := dependency.HookInstall(dependency.HookBuildTool(dependency.HookToolFields{Name: "ffmpeg", VersionFlag: "-version"}), linuxAmd64, found)
	if err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		info, err := os.Stat(filepath.Join(filepath.Dir(binary.Path), name))
		if err != nil || info.Mode().Perm() != 0755 {
			t.Errorf("%s: got %v, %v, want an executable file", name, info, err)
		}
	}
	for _, name := range []string{"ffplay", "LICENSE.txt"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(binary.Path), name)); !os.IsNotExist(err) {
			t.Errorf("%s was extracted, want only the listed members", name)
		}
	}
}

func requireNothingInstalled(t *testing.T, toolName, version string) {
	t.Helper()

	for _, path := range []string{dependency.HookInstallDirectory(toolName, version), dependency.HookInstallDirectory(toolName, version) + dependency.HookPartialSuffix} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s was left behind", path)
		}
	}
}

func TestInstallRejectsAChecksumMismatch(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	found, _ := denoArchive(t, "2.9.7")
	found.SHA256 = sha256Hex([]byte("the archive the mirror promised"))

	_, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("got %v, want a checksum mismatch", err)
	}
	requireNothingInstalled(t, "deno", "2.9.7")
}

func TestInstallRejectsABinaryThatDoesNotRun(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	payload := buildZip(t, map[string]string{"deno": failingScript})
	served := serveArchive(t, payload, http.StatusOK)
	found := &mirror.Candidate{Version: "2.9.7", URL: served.server.URL, SHA256: sha256Hex(payload), Members: []string{"deno"}}

	if _, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found); err == nil {
		t.Error("got nil, want an error for a binary that fails --version")
	}
	requireNothingInstalled(t, "deno", "2.9.7")
}

func TestInstallRejectsAnArchiveWithoutTheMember(t *testing.T) {
	useWorkingDirectory(t)
	payload := buildZip(t, map[string]string{"denort": script("denort 2.9.7")})
	served := serveArchive(t, payload, http.StatusOK)
	found := &mirror.Candidate{Version: "2.9.7", URL: served.server.URL, SHA256: sha256Hex(payload), Members: []string{"deno"}}

	_, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err == nil || !strings.Contains(err.Error(), "has no deno") {
		t.Errorf("got %v, want the missing member named", err)
	}
	requireNothingInstalled(t, "deno", "2.9.7")
}

func TestInstallRejectsAFailedDownload(t *testing.T) {
	useWorkingDirectory(t)
	served := serveArchive(t, nil, http.StatusBadGateway)
	found := &mirror.Candidate{Version: "2.9.7", URL: served.server.URL, SHA256: sha256Hex(nil), Members: []string{"deno"}}

	if _, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found); err == nil {
		t.Error("got nil, want an error for a failed download")
	}
	requireNothingInstalled(t, "deno", "2.9.7")
}

func TestInstallReplacesABrokenInstallOfTheSameVersion(t *testing.T) {
	useWorkingDirectory(t)
	writeExecutable(t, filepath.Join(dependency.HookLibDirectory, "deno-2.9.7", "deno"), failingScript)
	found, _ := denoArchive(t, "2.9.7")

	binary, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}
	if _, err := dependency.HookRunVersionCommand(binary.Path, "--version"); err != nil {
		t.Errorf("the reinstalled binary does not run: %v", err)
	}
}

func TestInstallFailsWhenLibIsNotADirectory(t *testing.T) {
	useWorkingDirectory(t)
	if err := os.WriteFile(dependency.HookLibDirectory, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", dependency.HookLibDirectory, err)
	}
	found, served := denoArchive(t, "2.9.7")

	if _, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found); err == nil {
		t.Error("got nil, want an error when lib cannot hold the install")
	}
	if served.downloads.Load() != 0 {
		t.Error("the archive was downloaded although it could not be installed")
	}
}

func TestExtractMembersFailsWhenTheDestinationIsMissing(t *testing.T) {
	directory := t.TempDir()
	archive := filepath.Join(directory, "archive")
	if err := os.WriteFile(archive, buildZip(t, map[string]string{"deno": "binary"}), 0644); err != nil {
		t.Fatalf("failed to write the archive: %v", err)
	}

	if err := dependency.HookExtractMembers(archive, mirror.ZipArchive, []string{"deno"}, filepath.Join(directory, "missing"), ignoreProgress); err == nil {
		t.Error("got nil, want an error when the destination does not exist")
	}
}

func TestExtractMembersRejectsDamagedContent(t *testing.T) {
	directory := t.TempDir()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "deno", Method: zip.Store})
	if err != nil {
		t.Fatalf("failed to add the entry: %v", err)
	}
	entry.Write([]byte("deno binary"))
	writer.Close()
	damaged := buffer.Bytes()
	damaged[bytes.Index(damaged, []byte("deno binary"))] ^= 0xFF
	archive := filepath.Join(directory, "archive")
	if err := os.WriteFile(archive, damaged, 0644); err != nil {
		t.Fatalf("failed to write the archive: %v", err)
	}

	if err := dependency.HookExtractMembers(archive, mirror.ZipArchive, []string{"deno"}, directory, ignoreProgress); err == nil {
		t.Error("got nil, want the checksum failure inside the zip reported")
	}
}

func TestExtractMembersRejectsACorruptArchive(t *testing.T) {
	directory := t.TempDir()
	corrupt := filepath.Join(directory, "archive")
	if err := os.WriteFile(corrupt, []byte("not an archive"), 0644); err != nil {
		t.Fatalf("failed to write the archive: %v", err)
	}

	if err := dependency.HookExtractMembers(corrupt, mirror.ZipArchive, []string{"deno"}, directory, ignoreProgress); err == nil {
		t.Error("got nil for a corrupt zip, want an error")
	}
	if err := dependency.HookExtractMembers(corrupt, mirror.TarXzArchive, []string{"ffmpeg"}, directory, ignoreProgress); err == nil {
		t.Error("got nil for a corrupt tar.xz, want an error")
	}
	if err := dependency.HookExtractMembers(filepath.Join(directory, "missing"), mirror.TarXzArchive, []string{"ffmpeg"}, directory, ignoreProgress); err == nil {
		t.Error("got nil for a missing archive, want an error")
	}
}

func denoPayload(t *testing.T, version string) []byte {
	t.Helper()

	return buildZip(t, map[string]string{"deno": script("deno " + version + " (stable, release)")})
}

func servedDeno(t *testing.T, payload []byte) (*mirror.Candidate, *archiveServer) {
	t.Helper()

	served := serveArchive(t, payload, http.StatusOK)
	return &mirror.Candidate{
		Version: "2.9.7",
		URL:     served.server.URL + "/deno.zip",
		SHA256:  sha256Hex(payload),
		Archive: mirror.ZipArchive,
		Members: []string{"deno"},
	}, served
}

func leaveStoppedInstall(t *testing.T, archive []byte, strays ...string) {
	t.Helper()

	partial := dependency.HookInstallDirectory("deno", "2.9.7") + dependency.HookPartialSuffix
	if err := os.MkdirAll(partial, 0755); err != nil {
		t.Fatalf("failed to create %s: %v", partial, err)
	}
	if err := os.WriteFile(filepath.Join(partial, dependency.HookArchiveName), archive, 0644); err != nil {
		t.Fatalf("failed to leave the archive behind: %v", err)
	}
	for _, stray := range strays {
		if err := os.WriteFile(filepath.Join(partial, stray), []byte("half written"), 0755); err != nil {
			t.Fatalf("failed to leave %s behind: %v", stray, err)
		}
	}
}

func TestInstallReusesAVerifiedArchiveLeftByAStoppedRun(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	payload := denoPayload(t, "2.9.7")
	found, served := servedDeno(t, payload)
	leaveStoppedInstall(t, payload, "deno", "denort")

	binary, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}

	if served.downloads.Load() != 0 {
		t.Errorf("got %d downloads, want the verified archive reused", served.downloads.Load())
	}
	if version, err := dependency.HookReadVersion(binary.Path, "--version"); err != nil || version != "2.9.7" {
		t.Errorf("got %q, %v, want the extracted deno 2.9.7", version, err)
	}
	if entries, _ := os.ReadDir(dependency.HookInstallDirectory("deno", "2.9.7")); len(entries) != 1 {
		t.Errorf("got %d files in the install directory, want only the binary without the stopped run's leftovers", len(entries))
	}
	if _, err := os.Stat(dependency.HookInstallDirectory("deno", "2.9.7") + dependency.HookPartialSuffix); !os.IsNotExist(err) {
		t.Error("the partial directory was left behind")
	}
}

func TestInstallDownloadsAgainWhenTheLeftoverArchiveIsIncomplete(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	payload := denoPayload(t, "2.9.7")
	found, served := servedDeno(t, payload)
	leaveStoppedInstall(t, payload[:len(payload)/2])

	if _, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found); err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}
	if served.downloads.Load() != 1 {
		t.Errorf("got %d downloads, want the incomplete archive replaced by one download", served.downloads.Load())
	}
}

func TestInstallDownloadsAgainWhenTheLeftoverArchiveIsForAnotherBuild(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	found, served := servedDeno(t, denoPayload(t, "2.9.7"))
	leaveStoppedInstall(t, denoPayload(t, "2.9.6"))

	binary, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
	if err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}
	if served.downloads.Load() != 1 {
		t.Errorf("got %d downloads, want the other build's archive replaced by one download", served.downloads.Load())
	}
	if version, err := dependency.HookReadVersion(binary.Path, "--version"); err != nil || version != "2.9.7" {
		t.Errorf("got %q, %v, want the served deno 2.9.7", version, err)
	}
}

func TestInstallDownloadsAgainWhenTheLeftoverArchiveCannotBeRead(t *testing.T) {
	useWorkingDirectory(t)
	skipOnWindows(t)
	found, served := servedDeno(t, denoPayload(t, "2.9.7"))
	if err := os.MkdirAll(filepath.Join(dependency.HookInstallDirectory("deno", "2.9.7")+dependency.HookPartialSuffix, dependency.HookArchiveName), 0755); err != nil {
		t.Fatalf("failed to leave a directory in place of the archive: %v", err)
	}

	if _, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found); err != nil {
		t.Fatalf("install returned %v, want nil", err)
	}
	if served.downloads.Load() != 1 {
		t.Errorf("got %d downloads, want the unreadable leftover replaced by one download", served.downloads.Load())
	}
}

func TestInstallFailsWhenTheStoppedRunCannotBeCleared(t *testing.T) {
	skipOnWindows(t)
	for name, check := range map[string]struct {
		mode os.FileMode
		want string
	}{
		"unlistable": {0300, "failed to read"},
		"unwritable": {0500, "failed to clear"},
	} {
		t.Run(name, func(t *testing.T) {
			useWorkingDirectory(t)
			payload := denoPayload(t, "2.9.7")
			found, served := servedDeno(t, payload)
			leaveStoppedInstall(t, payload, "denort")
			partial := dependency.HookInstallDirectory("deno", "2.9.7") + dependency.HookPartialSuffix
			if err := os.Chmod(partial, check.mode); err != nil {
				t.Fatalf("failed to restrict %s: %v", partial, err)
			}
			t.Cleanup(func() { _ = os.Chmod(partial, 0755) })

			_, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found)
			if want := check.want + " " + partial; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("got %v, want %q", err, want)
			}
			if served.downloads.Load() != 0 {
				t.Errorf("got %d downloads, want none while the leftovers are in the way", served.downloads.Load())
			}
		})
	}
}

func TestInstallDropsAReusedArchiveThatFailsToExtract(t *testing.T) {
	useWorkingDirectory(t)
	payload := buildZip(t, map[string]string{"denort": script("denort 2.9.7")})
	found, served := servedDeno(t, payload)
	leaveStoppedInstall(t, payload)

	if _, err := dependency.HookInstall(stubTool("deno"), linuxAmd64, found); err == nil {
		t.Fatal("got nil, want the missing member reported")
	}
	if served.downloads.Load() != 0 {
		t.Errorf("got %d downloads, want the verified archive tried first", served.downloads.Load())
	}
	requireNothingInstalled(t, "deno", "2.9.7")
}
