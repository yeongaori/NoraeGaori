package player_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"noraegaori/internal/dependency"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

type ffmpegBuilds struct {
	current  *dependency.Binary
	released []*dependency.Binary
}

func ffmpegBuild(version string) *dependency.Binary {
	return &dependency.Binary{Tool: "ffmpeg", Version: version, Path: "/app/lib/ffmpeg-" + version + "/ffmpeg"}
}

func useFFmpegBuilds(t *testing.T) *ffmpegBuilds {
	t.Helper()

	builds := &ffmpegBuilds{current: ffmpegBuild("2026.09.18.1845")}
	testutil.Swap(t, player.HookAcquireFFmpeg, func() *dependency.Binary { return builds.current })
	testutil.Swap(t, player.HookReleaseFFmpeg, func(binary *dependency.Binary) { builds.released = append(builds.released, binary) })
	return builds
}

type spawnedStream struct {
	binary string
	onExit func()
}

func captureSpawns(t *testing.T) *[]spawnedStream {
	t.Helper()

	spawned := []spawnedStream{}
	testutil.Swap(t, player.HookNewAudioStream, func(binary string, _ []string, _ bool, onExit func()) (player.HookAudioStream, error) {
		spawned = append(spawned, spawnedStream{binary: binary, onExit: onExit})
		return newFakeStream(0), nil
	})
	return &spawned
}

func TestOverlappingStreamsShareOneFFmpegBuild(t *testing.T) {
	builds := useFFmpegBuilds(t)
	old := builds.current
	var pin player.HookFfmpegPin

	first, releaseFirst := pin.HookClaim()
	builds.current = ffmpegBuild("2026.09.25.1845")
	second, releaseSecond := pin.HookClaim()

	if first != old || second != old {
		t.Fatalf("got %s and %s, want both on the build pinned when the first started", first.Version, second.Version)
	}

	releaseFirst()
	third, releaseThird := pin.HookClaim()
	if third != old {
		t.Errorf("got %s, want the old build kept while another stream still runs", third.Version)
	}
	if len(builds.released) != 0 {
		t.Errorf("got %d releases, want none while streams run", len(builds.released))
	}

	releaseSecond()
	releaseThird()
	if len(builds.released) != 1 || builds.released[0] != old {
		t.Errorf("got releases %v, want the old build released once", builds.released)
	}

	next, releaseNext := pin.HookClaim()
	defer releaseNext()
	if next != builds.current {
		t.Errorf("got %s, want the new build after a gap", next.Version)
	}
}

func TestReleasingAStreamTwiceCountsOnce(t *testing.T) {
	builds := useFFmpegBuilds(t)
	var pin player.HookFfmpegPin

	_, releaseFirst := pin.HookClaim()
	_, releaseSecond := pin.HookClaim()
	releaseFirst()
	releaseFirst()

	if *pin.HookLiveStreams() != 1 || len(builds.released) != 0 {
		t.Errorf("got %d live streams and %d releases, want the second stream still counted", *pin.HookLiveStreams(), len(builds.released))
	}
	releaseSecond()
	if *pin.HookLiveStreams() != 0 || *pin.HookBinary() != nil || len(builds.released) != 1 {
		t.Errorf("got %d live streams, pin %v and %d releases, want the pin dropped", *pin.HookLiveStreams(), *pin.HookBinary(), len(builds.released))
	}
}

func TestStartStreamRunsThePinnedBuild(t *testing.T) {
	builds := useFFmpegBuilds(t)
	spawned := captureSpawns(t)
	guildPlayer := &player.GuildPlayer{GuildID: "pinstart"}
	old := builds.current

	if _, err := guildPlayer.HookStartStream([]string{"-i", "a"}, false); err != nil {
		t.Fatalf("startStream returned %v, want nil", err)
	}
	builds.current = ffmpegBuild("2026.09.25.1845")
	if _, err := guildPlayer.HookStartStream([]string{"-i", "b"}, false); err != nil {
		t.Fatalf("startStream returned %v, want nil", err)
	}

	for i, stream := range *spawned {
		if stream.binary != old.Path {
			t.Errorf("stream %d ran %q, want the pinned %q", i, stream.binary, old.Path)
		}
	}

	(*spawned)[0].onExit()
	(*spawned)[1].onExit()
	if _, err := guildPlayer.HookStartStream([]string{"-i", "c"}, false); err != nil {
		t.Fatalf("startStream returned %v, want nil", err)
	}
	if got := (*spawned)[2].binary; got != builds.current.Path {
		t.Errorf("got %q, want the new build once both streams exited", got)
	}
}

func TestStartStreamReleasesAFailedSpawn(t *testing.T) {
	builds := useFFmpegBuilds(t)
	testutil.Swap(t, player.HookNewAudioStream, func(string, []string, bool, func()) (player.HookAudioStream, error) {
		return nil, errFakeStream
	})
	guildPlayer := &player.GuildPlayer{GuildID: "pinfail"}

	if _, err := guildPlayer.HookStartStream(nil, false); !errors.Is(err, errFakeStream) {
		t.Fatalf("got %v, want the spawn error", err)
	}
	if *guildPlayer.HookFfmpeg().HookLiveStreams() != 0 || len(builds.released) != 1 {
		t.Errorf("got %d live streams and %d releases, want the failed spawn released", *guildPlayer.HookFfmpeg().HookLiveStreams(), len(builds.released))
	}
}

func TestStartLiveStreamHandsThePinnedBuildToYtDlp(t *testing.T) {
	builds := useFFmpegBuilds(t)
	pipe := io.NopCloser(strings.NewReader("audio"))
	var ytdlpBuild *dependency.Binary
	testutil.Swap(t, player.HookGetLiveStreamPipe, func(_ string, _ bool, _, _ int, ffmpeg *dependency.Binary) (io.ReadCloser, error) {
		ytdlpBuild = ffmpeg
		return pipe, nil
	})
	var ffmpegBinary string
	var stdin io.ReadCloser
	testutil.Swap(t, player.HookNewAudioStreamPipe, func(binary string, _ []string, input io.ReadCloser, _ bool, _ func()) (player.HookAudioStream, error) {
		ffmpegBinary, stdin = binary, input
		return newFakeStream(0), nil
	})
	guildPlayer := &player.GuildPlayer{GuildID: "pinlive"}

	if _, err := guildPlayer.HookStartLiveStream("https://example.invalid/live", 128000, false, false); err != nil {
		t.Fatalf("startLiveStream returned %v, want nil", err)
	}
	if ytdlpBuild != builds.current || ffmpegBinary != builds.current.Path {
		t.Errorf("got yt-dlp on %v and ffmpeg %q, want both on the pinned build", ytdlpBuild, ffmpegBinary)
	}
	if stdin != pipe {
		t.Error("the yt-dlp pipe was not fed to ffmpeg")
	}
}

func TestStartLiveStreamReleasesOnFailure(t *testing.T) {
	builds := useFFmpegBuilds(t)
	guildPlayer := &player.GuildPlayer{GuildID: "pinlivefail"}
	testutil.Swap(t, player.HookGetLiveStreamPipe, func(string, bool, int, int, *dependency.Binary) (io.ReadCloser, error) {
		return nil, errFakeStream
	})

	if _, err := guildPlayer.HookStartLiveStream("https://example.invalid/live", 128000, false, false); !errors.Is(err, errFakeStream) {
		t.Fatalf("got %v, want the pipe error", err)
	}

	*player.HookGetLiveStreamPipe = func(string, bool, int, int, *dependency.Binary) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}
	testutil.Swap(t, player.HookNewAudioStreamPipe, func(string, []string, io.ReadCloser, bool, func()) (player.HookAudioStream, error) {
		return nil, errFakeStream
	})
	if _, err := guildPlayer.HookStartLiveStream("https://example.invalid/live", 128000, false, false); !errors.Is(err, errFakeStream) {
		t.Fatalf("got %v, want the spawn error", err)
	}

	if *guildPlayer.HookFfmpeg().HookLiveStreams() != 0 || len(builds.released) != 2 {
		t.Errorf("got %d live streams and %d releases, want both failures released", *guildPlayer.HookFfmpeg().HookLiveStreams(), len(builds.released))
	}
}

func TestOpenPlaybackStreamUsesThePlayersPin(t *testing.T) {
	builds := useFFmpegBuilds(t)
	spawned := captureSpawns(t)
	guildPlayer := &player.GuildPlayer{GuildID: "pinopen"}

	if _, err := player.HookOpenPlaybackStream(guildPlayer, &queue.Song{URL: "https://example.invalid/v"}, "https://example.invalid/stream", 0, 128000, false, false); err != nil {
		t.Fatalf("openPlaybackStream returned %v, want nil", err)
	}
	if len(*spawned) != 1 || (*spawned)[0].binary != builds.current.Path || *guildPlayer.HookFfmpeg().HookLiveStreams() != 1 {
		t.Errorf("got spawns %v with %d live streams, want one stream on the pinned build", *spawned, *guildPlayer.HookFfmpeg().HookLiveStreams())
	}
}

func TestCrossfadeIncomingStreamKeepsTheOutgoingBuild(t *testing.T) {
	guildID := "pincrossfade"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	builds := useFFmpegBuilds(t)
	spawned := captureSpawns(t)
	guildPlayer := player.GetPlayer(guildID)
	outgoing, releaseOutgoing := guildPlayer.HookFfmpeg().HookClaim()
	defer releaseOutgoing()

	builds.current = ffmpegBuild("2026.09.25.1845")
	cs := player.HookNewCrossfadeState()
	if planned := cs.HookPlan(guildPlayer, crossfadeEndState(), 100, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade")
	}

	if len(*spawned) != 1 || (*spawned)[0].binary != outgoing.Path {
		t.Errorf("got spawns %v, want the incoming stream on the outgoing build %q", *spawned, outgoing.Path)
	}
}
