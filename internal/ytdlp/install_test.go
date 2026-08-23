package ytdlp

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"noraegaori/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/config"
)

var platformAssetNames = []string{"yt-dlp", "yt-dlp.exe", "yt-dlp_macos", "yt-dlp_linux_aarch64"}

const workingBinaryScript = "#!/bin/sh\necho 2026.07.04\n"

func serveInstallableRelease(t *testing.T, version string, payload []byte, breakDownload bool) *GitHubRelease {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake executables are not portable to windows")
	}

	return installableRelease(t, useTestSigner(t), version, payload, breakDownload)
}

func serveInstallableReleases(t *testing.T, versions ...string) []*GitHubRelease {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake executables are not portable to windows")
	}

	sign := useTestSigner(t)

	releases := make([]*GitHubRelease, 0, len(versions))
	for _, version := range versions {
		payload := []byte("#!/bin/sh\necho " + version + "\n")
		releases = append(releases, installableRelease(t, sign, version, payload, false))
	}
	return releases
}

func installableRelease(t *testing.T, sign func([]byte) []byte, version string, payload []byte, breakDownload bool) *GitHubRelease {
	t.Helper()

	var sums bytes.Buffer
	for _, name := range platformAssetNames {
		fmt.Fprintf(&sums, "%s  %s\n", sha256Hex(payload), name)
	}
	checksums := sums.Bytes()
	signature := sign(checksums)

	mux := http.NewServeMux()
	for _, name := range platformAssetNames {
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			if breakDownload {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write(payload)
		})
	}
	mux.HandleFunc("/"+checksumAssetName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(checksums)
	})
	mux.HandleFunc("/"+checksumSigAssetName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(signature)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	release := &GitHubRelease{TagName: version}
	for _, name := range platformAssetNames {
		addAsset(release, name, server.URL+"/"+name, "")
	}
	addAsset(release, checksumAssetName, server.URL+"/"+checksumAssetName, "")
	addAsset(release, checksumSigAssetName, server.URL+"/"+checksumSigAssetName, "")

	return release
}

func TestInstallVersionBinaryProducesARunnableBinary(t *testing.T) {
	t.Chdir(t.TempDir())
	release := serveInstallableRelease(t, "2026.07.04", []byte(workingBinaryScript), false)

	binaryPath, reportedVersion, err := installVersionBinary(release, "2026.07.04")
	if err != nil {
		t.Fatalf("installVersionBinary returned %v, want nil", err)
	}

	if binaryPath != VersionedBinaryPath("2026.07.04") {
		t.Errorf("got path %q, want %q", binaryPath, VersionedBinaryPath("2026.07.04"))
	}
	if reportedVersion != "2026.07.04" {
		t.Errorf("got reported version %q, want %q", reportedVersion, "2026.07.04")
	}

	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("the installed binary is missing: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("got mode %v, want 0755 so the binary can be executed", info.Mode().Perm())
	}
}

func TestInstallVersionBinaryRemovesTheVersionDirectoryOnDownloadFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	release := serveInstallableRelease(t, "2026.07.04", []byte(workingBinaryScript), true)

	if _, _, err := installVersionBinary(release, "2026.07.04"); err == nil {
		t.Fatal("installVersionBinary returned nil, want an error for a failed download")
	}

	versionDir := filepath.Dir(VersionedBinaryPath("2026.07.04"))
	if _, err := os.Stat(versionDir); !os.IsNotExist(err) {
		t.Error("the version directory survived a failed download, leaving a partial install on disk")
	}
}

func TestInstallVersionBinaryRemovesTheVersionDirectoryWhenTheBinaryDoesNotRun(t *testing.T) {
	t.Chdir(t.TempDir())
	release := serveInstallableRelease(t, "2026.07.04", []byte("this is not an executable\n"), false)

	_, _, err := installVersionBinary(release, "2026.07.04")
	if err == nil {
		t.Fatal("installVersionBinary returned nil, want an error when the binary cannot run")
	}
	if !strings.Contains(err.Error(), "2026.07.04") {
		t.Errorf("error %q does not name the version that failed", err)
	}

	versionDir := filepath.Dir(VersionedBinaryPath("2026.07.04"))
	if _, statErr := os.Stat(versionDir); !os.IsNotExist(statErr) {
		t.Error("an unrunnable install was left on disk")
	}
}

func TestInstallVersionBinaryFailsWhenNoAssetMatchesThePlatform(t *testing.T) {
	t.Chdir(t.TempDir())
	release := &GitHubRelease{TagName: "2026.07.04"}
	addAsset(release, "unrelated-asset", "https://example.invalid/unrelated", "")

	if _, _, err := installVersionBinary(release, "2026.07.04"); err == nil {
		t.Error("installVersionBinary returned nil, want an error when no asset matches")
	}
}

func TestEnsureVersionBinaryReusesAnInstalledBinary(t *testing.T) {
	t.Chdir(t.TempDir())
	release := serveInstallableRelease(t, "2026.07.04", []byte(workingBinaryScript), true)

	binaryPath := VersionedBinaryPath("2026.07.04")
	writeFakeBinary(t, filepath.Dir(binaryPath), "echo 2026.07.04")

	got, err := ensureVersionBinary(release, "2026.07.04")
	if err != nil {
		t.Fatalf("ensureVersionBinary returned %v, want nil for an already installed binary", err)
	}
	if got != binaryPath {
		t.Errorf("got path %q, want %q", got, binaryPath)
	}
}

func TestEnsureVersionBinaryRemovesAnInstalledBinaryThatDoesNotRun(t *testing.T) {
	t.Chdir(t.TempDir())
	release := serveInstallableRelease(t, "2026.07.04", []byte(workingBinaryScript), true)

	binaryPath := VersionedBinaryPath("2026.07.04")
	writeFakeBinary(t, filepath.Dir(binaryPath), "exit 3")

	if _, err := ensureVersionBinary(release, "2026.07.04"); err == nil {
		t.Fatal("ensureVersionBinary returned nil, want an error when the installed binary fails")
	}

	if _, err := os.Stat(filepath.Dir(binaryPath)); !os.IsNotExist(err) {
		t.Error("a broken install was left on disk")
	}
}

func TestEnsureVersionBinaryInstallsWhenNothingIsPresent(t *testing.T) {
	t.Chdir(t.TempDir())
	release := serveInstallableRelease(t, "2026.07.04", []byte(workingBinaryScript), false)

	binaryPath, err := ensureVersionBinary(release, "2026.07.04")
	if err != nil {
		t.Fatalf("ensureVersionBinary returned %v, want nil", err)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		t.Errorf("the binary was not installed: %v", err)
	}
}

func stubCanary(t *testing.T, passed, networkError bool) {
	t.Helper()

	testutil.Swap(t, &runCanary, func(*VersionManager, string) (bool, bool) { return passed, networkError })
}

func stubCanaryPerVersion(t *testing.T, verdicts map[string]bool) {
	t.Helper()

	testutil.Swap(t, &runCanary, func(_ *VersionManager, version string) (bool, bool) {
		return verdicts[version], false
	})
}

func TestExhaustedFallbackKeepsTheInstalledVersion(t *testing.T) {
	versionmanager := useVersionManager(t)
	stubCanary(t, false, false)

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.08.18.122307"), "echo 2026.08.18.122307")
	addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: path, State: StateActive, Successes: 40})
	versionmanager.state.ActiveVersion = "2026.08.18.122307"

	testutil.Swap(t, &getReleasesFn, func(channel string, perPage int) ([]*GitHubRelease, error) {
		return nil, nil
	})

	outcome, err := installFallbackVersion(versionmanager, config.YtDlpChannelNightly, "2026.08.20.234504", &GitHubRelease{TagName: "2026.08.20.234504"})
	if err != nil {
		t.Fatalf("installFallbackVersion returned %v, want nil", err)
	}
	if !outcome.canaryFailed {
		t.Error("got canaryFailed=false, want the exhausted chain reported so the other channel is tried")
	}
	if outcome.updated {
		t.Error("got updated=true, want false: nothing was installed")
	}

	if got := versionmanager.GetActiveVersion(); got != "2026.08.18.122307" {
		t.Errorf("got active %q, want the installed version kept", got)
	}
	if state, _ := versionmanager.GetVersionState("2026.08.20.234504"); state == StateProvisional {
		t.Error("a version that failed its canary was provisionally activated over a working installed binary")
	}
}

func TestRunCanaryAndActivateActivatesOnSuccess(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/yt-dlp-2026.07.04/yt-dlp")
	stubCanary(t, true, false)

	if verdict := runCanaryAndActivate(versionmanager, "2026.07.04"); verdict != canaryActivated {
		t.Fatalf("got verdict %v, want canaryActivated", verdict)
	}
	if got := versionmanager.GetActiveVersion(); got != "2026.07.04" {
		t.Errorf("got active version %q, want %q", got, "2026.07.04")
	}
	if state := versionmanager.state.Versions["2026.07.04"].State; state != StateActive {
		t.Errorf("got state %q, want %q", state, StateActive)
	}
}

func TestRunCanaryAndActivateLeavesNetworkFailuresPending(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/yt-dlp-2026.07.04/yt-dlp")
	stubCanary(t, false, true)

	if verdict := runCanaryAndActivate(versionmanager, "2026.07.04"); verdict != canaryPending {
		t.Fatalf("got verdict %v, want canaryPending", verdict)
	}
	if state := versionmanager.state.Versions["2026.07.04"].State; state != StatePending {
		t.Errorf("got state %q, want %q, a network failure is not evidence against the binary", state, StatePending)
	}
	if got := versionmanager.GetActiveVersion(); got != "" {
		t.Errorf("got active version %q, want the version to stay unactivated", got)
	}
}

func TestRunCanaryAndActivateBlacklistsRealFailures(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/yt-dlp-2026.07.04/yt-dlp")
	stubCanary(t, false, false)

	if verdict := runCanaryAndActivate(versionmanager, "2026.07.04"); verdict != canaryRejected {
		t.Fatalf("got verdict %v, want canaryRejected", verdict)
	}
	if state := versionmanager.state.Versions["2026.07.04"].State; state != StateBlacklisted {
		t.Errorf("got state %q, want %q", state, StateBlacklisted)
	}
}

func TestResolveCurrentVersionPrefersTheActiveVersion(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})
	versionmanager.state.ActiveVersion = "2026.07.04"

	if got := resolveCurrentVersion(versionmanager); got != "2026.07.04" {
		t.Errorf("got %q, want %q", got, "2026.07.04")
	}
}

func TestResolveCurrentVersionReportsNothingWhenNoBinaryExists(t *testing.T) {
	t.Chdir(t.TempDir())

	if got := resolveCurrentVersion(nil); got != "" {
		t.Errorf("got %q, want an empty version when nothing is installed", got)
	}
}

func stubReleaseList(t *testing.T, releases []*GitHubRelease) {
	t.Helper()

	testutil.Swap(t, &getReleasesFn, func(channel string, perPage int) ([]*GitHubRelease, error) {
		return releases, nil
	})
}

func TestFallbackChainActivatesTheFirstCandidateThatPasses(t *testing.T) {
	versionmanager := useVersionManager(t)

	releases := serveInstallableReleases(t, "2026.09.03", "2026.09.02", "2026.09.01")
	stubReleaseList(t, releases)
	stubCanaryPerVersion(t, map[string]bool{"2026.09.01": true})

	outcome, err := installFallbackVersion(versionmanager, config.YtDlpChannelStable, "2026.09.03", releases[0])
	if err != nil {
		t.Fatalf("installFallbackVersion returned %v, want nil", err)
	}
	if !outcome.updated {
		t.Error("got updated=false, want the passing candidate activated")
	}

	if got := versionmanager.GetActiveVersion(); got != "2026.09.01" {
		t.Errorf("got active %q, want the first candidate whose canary passed", got)
	}
	if state, _ := versionmanager.GetVersionState("2026.09.02"); state != StateBlacklisted {
		t.Errorf("got %q for the rejected candidate, want it blacklisted", state)
	}
}

func TestFallbackChainStopsOnACanaryNetworkError(t *testing.T) {
	versionmanager := useVersionManager(t)

	releases := serveInstallableReleases(t, "2026.09.03", "2026.09.02", "2026.09.01")
	stubReleaseList(t, releases)

	testutil.Swap(t, &runCanary, func(_ *VersionManager, version string) (bool, bool) { return false, true })

	outcome, err := installFallbackVersion(versionmanager, config.YtDlpChannelStable, "2026.09.03", releases[0])
	if err != nil {
		t.Fatalf("installFallbackVersion returned %v, want nil", err)
	}
	if outcome.updated || outcome.canaryFailed {
		t.Errorf("got %+v, want a zero outcome: a network blip must not condemn the channel", outcome)
	}

	if _, ok := versionmanager.GetVersionState("2026.09.01"); ok {
		t.Error("the chain kept trying candidates after a canary network error")
	}
}

func TestFallbackChainProvisionallyActivatesWhenNothingUsableRemains(t *testing.T) {
	versionmanager := useVersionManager(t)

	releases := serveInstallableReleases(t, "2026.09.03", "2026.09.02")
	stubReleaseList(t, releases)
	stubCanary(t, false, false)

	addVersion(versionmanager, "2026.08.19", &VersionEntry{Path: "lib/gone/yt-dlp", State: StateVerified, Successes: 40, BlacklistedAt: time.Now()})
	versionmanager.state.ActiveVersion = "2026.08.19"

	outcome, err := installFallbackVersion(versionmanager, config.YtDlpChannelStable, "2026.09.03", releases[0])
	if err != nil {
		t.Fatalf("installFallbackVersion returned %v, want nil", err)
	}
	if !outcome.updated || !outcome.canaryFailed {
		t.Errorf("got %+v, want {updated:true canaryFailed:true} for the last resort", outcome)
	}

	if got := versionmanager.GetActiveVersion(); got != "2026.09.03" {
		t.Errorf("got active %q, want the latest provisionally activated as a last resort", got)
	}
	state, _ := versionmanager.GetVersionState("2026.09.03")
	if state != StateProvisional {
		t.Errorf("got state %q, want %q: the binary never passed a canary", state, StateProvisional)
	}
	if !versionmanager.state.Versions["2026.09.03"].BlacklistedAt.IsZero() {
		t.Error("the provisional activation left a blacklist timestamp behind")
	}
}
