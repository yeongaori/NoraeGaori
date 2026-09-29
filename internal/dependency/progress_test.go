package dependency

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/tests/testutil"
)

func ignoreProgress(int) {}

func recordProgress() (func(int), *[]int) {
	reports := []int{}
	return func(percent int) { reports = append(reports, percent) }, &reports
}

func requireClimbToFull(t *testing.T, reports []int) {
	t.Helper()

	if len(reports) == 0 || reports[len(reports)-1] != 100 {
		t.Fatalf("got %v, want the progress to end at 100", reports)
	}
	for i := 1; i < len(reports); i++ {
		if reports[i] <= reports[i-1] {
			t.Fatalf("got %v, want strictly increasing percentages", reports)
		}
	}
}

func incompressible(size int) string {
	source := rand.New(rand.NewPCG(7, 11))
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(source.UintN(256))
	}
	return string(content)
}

func writeArchive(t *testing.T, payload []byte) string {
	t.Helper()

	archive := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(archive, payload, 0644); err != nil {
		t.Fatalf("failed to write the archive: %v", err)
	}
	return archive
}

func TestZipProgressCountsOnlyTheExtractedMembers(t *testing.T) {
	archive := writeArchive(t, buildZip(t, map[string]string{
		"a-deno":   strings.Repeat("a", 300),
		"b-deno":   strings.Repeat("b", 700),
		"c-denort": strings.Repeat("c", 9000),
	}))
	report, reports := recordProgress()

	if err := extractMembers(archive, mirror.ZipArchive, []string{"a-deno", "b-deno"}, t.TempDir(), report); err != nil {
		t.Fatalf("extractMembers returned %v, want nil", err)
	}

	requireClimbToFull(t, *reports)
	if (*reports)[0] > 30 {
		t.Errorf("got %v, want the first report no later than 30%% after the 300-byte member", *reports)
	}
}

func TestTarXzProgressReachesFullOnlyAfterTheLastMember(t *testing.T) {
	archive := writeArchive(t, buildTarXz(t, map[string]string{
		"bundle/bin/a-ffmpeg":  strings.Repeat("\x00", 4<<20),
		"bundle/bin/b-ffprobe": incompressible(5 << 19),
	}))
	destination := t.TempDir()
	lastMemberAtFull := false
	reports := []int{}
	report := func(percent int) {
		reports = append(reports, percent)
		if percent == 100 {
			_, err := os.Stat(filepath.Join(destination, "b-ffprobe"))
			lastMemberAtFull = err == nil
		}
	}

	if err := extractMembers(archive, mirror.TarXzArchive, []string{"bundle/bin/a-ffmpeg", "bundle/bin/b-ffprobe"}, destination, report); err != nil {
		t.Fatalf("extractMembers returned %v, want nil", err)
	}

	requireClimbToFull(t, reports)
	if !lastMemberAtFull {
		t.Error("100% was reported before the last member was reached, so progress is not measured on the compressed archive")
	}
}

func TestTarXzProgressReachesFullWhenTheTarballEndsBeforeTheFile(t *testing.T) {
	tarball := buildTarXz(t, map[string]string{"bundle/bin/ffmpeg": "binary"})
	trailer := buildTarXz(t, map[string]string{"bundle/bin/extra": incompressible(3 << 20)})
	archive := writeArchive(t, append(tarball, trailer...))
	report, reports := recordProgress()

	if err := extractMembers(archive, mirror.TarXzArchive, []string{"bundle/bin/ffmpeg"}, t.TempDir(), report); err != nil {
		t.Fatalf("extractMembers returned %v, want nil", err)
	}

	requireClimbToFull(t, *reports)
}

func readSyscalls(t *testing.T) int {
	t.Helper()

	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Fatalf("failed to read /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, found := strings.CutPrefix(line, "syscr: "); found {
			count, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("malformed syscr line %q", line)
			}
			return count
		}
	}
	t.Fatal("/proc/self/io has no syscr line")
	return 0
}

func TestTarXzExtractionReadsTheArchiveInLargeChunks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("read syscalls are counted through /proc/self/io")
	}
	archive := writeArchive(t, buildTarXz(t, map[string]string{"bundle/bin/ffmpeg": incompressible(2 << 20)}))
	destination := t.TempDir()

	before := readSyscalls(t)
	if err := extractMembers(archive, mirror.TarXzArchive, []string{"bundle/bin/ffmpeg"}, destination, ignoreProgress); err != nil {
		t.Fatalf("extractMembers returned %v, want nil", err)
	}
	reads := readSyscalls(t) - before

	if reads >= 1000 {
		t.Errorf("extracting a 2 MiB archive took %d read syscalls, want fewer than 1000", reads)
	}
}

func TestZipWithoutTheMembersReportsNothing(t *testing.T) {
	archive := writeArchive(t, buildZip(t, map[string]string{"denort": "binary"}))
	report, reports := recordProgress()

	if err := extractMembers(archive, mirror.ZipArchive, []string{"deno"}, t.TempDir(), report); err == nil {
		t.Fatal("got nil, want the missing member reported")
	}
	if len(*reports) != 0 {
		t.Errorf("got %v, want no progress when nothing is extracted", *reports)
	}
}

func stubExtractionProgress(t *testing.T) (*[]string, *int) {
	t.Helper()

	messages := []string{}
	ended := 0
	testutil.Swap(t, &showProgress, func(percent int, message string) { messages = append(messages, message) })
	testutil.Swap(t, &endProgress, func() { ended++ })
	return &messages, &ended
}

func TestExtractWithProgressAnnouncesEachStep(t *testing.T) {
	useWorkingDirectory(t)
	read := captureLog(t)
	messages, ended := stubExtractionProgress(t)
	archive := writeArchive(t, buildZip(t, map[string]string{"deno": strings.Repeat("d", 2000)}))
	found := &mirror.Candidate{Archive: mirror.ZipArchive, Members: []string{"deno"}}

	if err := extractWithProgress("deno-x86_64-unknown-linux-gnu.zip", archive, found, t.TempDir()); err != nil {
		t.Fatalf("extractWithProgress returned %v, want nil", err)
	}

	if len(*messages) == 0 || (*messages)[len(*messages)-1] != "Extracting deno-x86_64-unknown-linux-gnu.zip: 100%" {
		t.Errorf("got %v, want the last update at 100%%", *messages)
	}
	if *ended != 1 {
		t.Errorf("ended the progress line %d times, want once", *ended)
	}
	log := read()
	if !strings.Contains(log, "Extracting deno-x86_64-unknown-linux-gnu.zip") || !strings.Contains(log, "Extracted deno-x86_64-unknown-linux-gnu.zip") {
		t.Errorf("got log %q, want the start and finish lines", log)
	}
}

func TestExtractWithProgressEndsTheLineOnFailure(t *testing.T) {
	useWorkingDirectory(t)
	read := captureLog(t)
	_, ended := stubExtractionProgress(t)
	archive := writeArchive(t, buildZip(t, map[string]string{"denort": "binary"}))
	found := &mirror.Candidate{Archive: mirror.ZipArchive, Members: []string{"deno"}}

	if err := extractWithProgress("deno.zip", archive, found, t.TempDir()); err == nil {
		t.Fatal("got nil, want the missing member reported")
	}
	if *ended != 1 {
		t.Errorf("ended the progress line %d times, want once even when extraction fails", *ended)
	}
	if strings.Contains(read(), "Extracted deno.zip") {
		t.Error("a failed extraction was logged as finished")
	}
}
