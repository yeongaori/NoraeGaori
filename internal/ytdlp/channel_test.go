package ytdlp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/config"
)

func useChannel(t *testing.T, channel string) {
	t.Helper()

	previous := configuredChannelFn
	configuredChannelFn = func() string { return channel }
	t.Cleanup(func() { configuredChannelFn = previous })
}

func TestChannelOfClassifiesByTagShape(t *testing.T) {
	cases := map[string]string{
		"2026.07.04":        config.YtDlpChannelStable,
		"2026.06.09":        config.YtDlpChannelStable,
		"2026.08.18.122307": config.YtDlpChannelNightly,
		"2026.01.01.000000": config.YtDlpChannelNightly,
	}

	for version, want := range cases {
		if got := ChannelOf(version); got != want {
			t.Errorf("ChannelOf(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestReleaseURLsFollowTheRequestedChannel(t *testing.T) {
	if !strings.Contains(latestReleaseURL(config.YtDlpChannelNightly), "yt-dlp-nightly-builds") {
		t.Errorf("got %q, want the nightly repo", latestReleaseURL(config.YtDlpChannelNightly))
	}
	if !strings.Contains(releasesURL(config.YtDlpChannelNightly), "yt-dlp-nightly-builds") {
		t.Errorf("got %q, want the nightly repo", releasesURL(config.YtDlpChannelNightly))
	}

	if strings.Contains(latestReleaseURL(config.YtDlpChannelStable), "nightly") {
		t.Errorf("got %q, want the stable repo", latestReleaseURL(config.YtDlpChannelStable))
	}
	if !strings.HasSuffix(releasesURL(config.YtDlpChannelStable), "/repos/yt-dlp/yt-dlp/releases") {
		t.Errorf("got %q, want the stable releases endpoint", releasesURL(config.YtDlpChannelStable))
	}

	if !strings.HasSuffix(releasesURL(config.YtDlpChannelAuto), "/repos/yt-dlp/yt-dlp/releases") {
		t.Errorf("got %q, want auto to start from stable", releasesURL(config.YtDlpChannelAuto))
	}
}

func TestSelectBestVersionPrefersTheConfiguredChannel(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateVerified, Successes: 5})
	addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: "lib/b/yt-dlp", State: StateVerified, Successes: 5})

	useChannel(t, config.YtDlpChannelNightly)
	if got := versionmanager.selectBestVersion(); got != "2026.08.18.122307" {
		t.Errorf("got %q on the nightly channel, want the nightly build", got)
	}

	useChannel(t, config.YtDlpChannelStable)
	if got := versionmanager.selectBestVersion(); got != "2026.07.04" {
		t.Errorf("got %q on the stable channel, want the stable build", got)
	}
}

func TestSelectBestVersionFallsBackAcrossChannels(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateVerified, Successes: 5})

	useChannel(t, config.YtDlpChannelNightly)

	if got := versionmanager.selectBestVersion(); got != "2026.07.04" {
		t.Errorf("got %q, want the stable build: a usable binary beats channel purity when rolling back", got)
	}
}

func TestSelectBestVersionStillSkipsBlacklistedBuilds(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: "lib/a/yt-dlp", State: StateBlacklisted, Successes: 5})
	addVersion(versionmanager, "2026.08.17.000000", &VersionEntry{Path: "lib/b/yt-dlp", State: StateVerified, Successes: 2})

	useChannel(t, config.YtDlpChannelNightly)

	if got := versionmanager.selectBestVersion(); got != "2026.08.17.000000" {
		t.Errorf("got %q, want the older nightly that is not blacklisted", got)
	}
}

func recordChannelAttempts(t *testing.T, onAttempt func(channel string)) *[]string {
	t.Helper()

	attempts := []string{}
	previous := updateChannelFn
	updateChannelFn = func(channel string, force bool) (channelOutcome, error) {
		attempts = append(attempts, channel)
		if onAttempt != nil {
			onAttempt(channel)
		}
		return channelOutcome{}, nil
	}
	t.Cleanup(func() { updateChannelFn = previous })

	return &attempts
}

func useVersionManager(t *testing.T) *VersionManager {
	t.Helper()

	versionmanager := newTestVersionManager(t)
	previous := versionMgr
	versionMgr = versionmanager
	t.Cleanup(func() { versionMgr = previous })

	return versionmanager
}

func TestAutoStaysOnStableWhileStableWorks(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)

	attempts := recordChannelAttempts(t, func(string) {
		addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/s/yt-dlp", State: StateActive})
		versionmanager.state.ActiveVersion = "2026.07.04"
	})

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(*attempts) != 1 || (*attempts)[0] != config.YtDlpChannelStable {
		t.Errorf("got attempts %v, want stable only: nightly must not be touched while stable works", *attempts)
	}
}

func TestAutoFallsBackToNightlyWhenStableIsBroken(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/s/yt-dlp", State: StateBlacklisted})

	attempts := recordChannelAttempts(t, func(channel string) {
		if channel == config.YtDlpChannelNightly {
			addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: "lib/n/yt-dlp", State: StateActive})
			versionmanager.state.ActiveVersion = "2026.08.18.122307"
		}
	})

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	want := []string{config.YtDlpChannelStable, config.YtDlpChannelNightly}
	if len(*attempts) != 2 || (*attempts)[0] != want[0] || (*attempts)[1] != want[1] {
		t.Errorf("got attempts %v, want %v: stable must be tried first, then nightly", *attempts, want)
	}
}

func TestAutoReturnsToStableOnceItShipsAWorkingRelease(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)

	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/s/yt-dlp", State: StateBlacklisted})
	addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: "lib/n/yt-dlp", State: StateActive})
	versionmanager.state.ActiveVersion = "2026.08.18.122307"

	attempts := recordChannelAttempts(t, func(channel string) {
		if channel == config.YtDlpChannelStable {
			addVersion(versionmanager, "2026.09.01", &VersionEntry{Path: "lib/s2/yt-dlp", State: StateActive})
			versionmanager.state.ActiveVersion = "2026.09.01"
		}
	})

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(*attempts) != 1 || (*attempts)[0] != config.YtDlpChannelStable {
		t.Errorf("got attempts %v, want stable only once a healthy stable exists", *attempts)
	}
	if got := versionmanager.GetActiveVersion(); got != "2026.09.01" {
		t.Errorf("got active %q, want the bot to move back onto stable", got)
	}
}

func TestAutoRejectsAProvisionallyActivatedStable(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)

	attempts := recordChannelAttempts(t, func(channel string) {
		if channel == config.YtDlpChannelStable {
			addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/s/yt-dlp", State: StateProvisional})
			versionmanager.state.ActiveVersion = "2026.07.04"
		}
	})

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(*attempts) != 2 {
		t.Errorf("got attempts %v, want nightly tried too: a provisional stable never passed its canary", *attempts)
	}
}

func TestExplicitChannelPinSkipsTheAutoPolicy(t *testing.T) {
	useChannel(t, config.YtDlpChannelNightly)
	useVersionManager(t)

	attempts := recordChannelAttempts(t, nil)

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(*attempts) != 1 || (*attempts)[0] != config.YtDlpChannelNightly {
		t.Errorf("got attempts %v, want the pinned nightly channel only", *attempts)
	}
}

func TestProbeStreamReachableAcceptsAServedStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer server.Close()

	if err := probeStreamReachable(context.Background(), server.URL); err != nil {
		t.Errorf("probeStreamReachable returned %v, want nil for a served stream", err)
	}
}

func TestProbeStreamReachableRejectsAForbiddenStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	err := probeStreamReachable(context.Background(), server.URL)
	if err == nil {
		t.Fatal("got nil, want a rejection: a 403 stream URL is exactly the failure playback hits")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error %q does not name the status code", err)
	}
}

func TestProbeStreamReachableSendsNoRangeHeader(t *testing.T) {
	var sawRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRange = r.Header.Get("Range")
		w.Write(make([]byte, 1024))
	}))
	defer server.Close()

	if err := probeStreamReachable(context.Background(), server.URL); err != nil {
		t.Fatalf("probeStreamReachable returned %v, want nil", err)
	}

	if sawRange != "" {
		t.Errorf("probe sent Range=%q; it must match ffmpeg, which sends none and is what googlevideo rejects", sawRange)
	}
}

func TestCanaryProbesRealPlaybackBeforeFixedVideos(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.state.CanaryRing = []string{"playedA", "playedB"}

	ids := versionmanager.getCanaryIDs()
	if len(ids) == 0 {
		t.Fatal("got no canary videos")
	}

	for _, fixed := range fixedCanaryIDs {
		if ids[0] == fixed {
			t.Fatalf("canary starts with the fixed video %q; RunCanary stops at the first success, so an unrepresentative video would mask a broken extractor", fixed)
		}
	}

	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	for _, fixed := range fixedCanaryIDs {
		if !found[fixed] {
			t.Errorf("fixed video %q was dropped; it is the fallback when nothing has played yet", fixed)
		}
	}
}

func TestCanaryFallsBackToFixedVideosOnAFreshInstall(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	ids := versionmanager.getCanaryIDs()
	if len(ids) != len(fixedCanaryIDs) {
		t.Errorf("got %v, want exactly the fixed videos when nothing has played yet", ids)
	}
}

func TestActiveVersionGoesUnhealthyOnRepeatedPlaybackFailures(t *testing.T) {
	versionmanager := useVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/s/yt-dlp", State: StateActive, Successes: 400})
	versionmanager.state.ActiveVersion = "2026.07.04"

	if !activeVersionIsHealthy() {
		t.Fatal("a canary-verified active version should start healthy")
	}

	for _, video := range []string{"vidA", "vidB", "vidC"} {
		versionmanager.SaveError("2026.07.04", video, "ffmpeg produced no audio: exit status 8: 403 Forbidden")
	}

	if activeVersionIsHealthy() {
		t.Error("still healthy after repeated playback failures: the auto policy would never leave the broken version")
	}
}

func TestActiveNightlyStaysHealthyWithoutReprobingNightly(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)
	addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: "lib/n/yt-dlp", State: StateActive, Successes: 40})
	versionmanager.state.ActiveVersion = "2026.08.18.122307"

	attempts := recordChannelAttempts(t, nil)

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(*attempts) != 1 || (*attempts)[0] != config.YtDlpChannelStable {
		t.Errorf("got attempts %v, want stable only: a healthy nightly must not re-probe nightly on every check", *attempts)
	}
}

func TestAutoTriesNightlyWhenStableFailsItsCanary(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)
	addVersion(versionmanager, "2026.08.18.122307", &VersionEntry{Path: "lib/n/yt-dlp", State: StateActive, Successes: 40})
	versionmanager.state.ActiveVersion = "2026.08.18.122307"

	attempts := []string{}
	previous := updateChannelFn
	updateChannelFn = func(channel string, force bool) (channelOutcome, error) {
		attempts = append(attempts, channel)
		return channelOutcome{canaryFailed: channel == config.YtDlpChannelStable}, nil
	}
	t.Cleanup(func() { updateChannelFn = previous })

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(attempts) != 2 || attempts[1] != config.YtDlpChannelNightly {
		t.Errorf("got attempts %v, want nightly tried after stable failed its canary even though the active version is healthy", attempts)
	}
}

func TestAutoMovesToNightlyAfterPlaybackFailures(t *testing.T) {
	useChannel(t, config.YtDlpChannelAuto)
	versionmanager := useVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/s/yt-dlp", State: StateActive, Successes: 400})
	versionmanager.state.ActiveVersion = "2026.07.04"

	for _, video := range []string{"vidA", "vidB", "vidC"} {
		versionmanager.SaveError("2026.07.04", video, "ffmpeg produced no audio: exit status 8: 403 Forbidden")
	}

	attempts := recordChannelAttempts(t, nil)

	if _, err := UpdateYtDlp(false); err != nil {
		t.Fatalf("UpdateYtDlp returned %v, want nil", err)
	}

	if len(*attempts) != 2 || (*attempts)[1] != config.YtDlpChannelNightly {
		t.Errorf("got attempts %v, want stable then nightly once stable is failing playback", *attempts)
	}
}

func TestRequestUpdateCheckNeverBlocks(t *testing.T) {
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			RequestUpdateCheck()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RequestUpdateCheck blocked with no reader draining the channel")
	}
}
