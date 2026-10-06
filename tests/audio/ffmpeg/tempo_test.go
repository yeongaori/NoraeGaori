package ffmpeg_test

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
)

func filterArgument(args []string) string {
	for i, arg := range args {
		if arg == "-af" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestArgsLeaveTheAudioAloneWithoutTempoOrNormalization(t *testing.T) {
	if filter := filterArgument(ffmpeg.Args("input", 0, false, ffmpeg.Tempo{})); filter != "" {
		t.Errorf("filter = %q, want none", filter)
	}
	if filter := filterArgument(ffmpeg.Args("input", 0, false, ffmpeg.Tempo{Speed: 1, HoldSec: 10})); filter != "" {
		t.Errorf("unit speed filter = %q, want none", filter)
	}
	if filter := filterArgument(ffmpeg.Args("input", 0, true, ffmpeg.Tempo{})); !strings.HasPrefix(filter, "dynaudnorm=") {
		t.Errorf("filter = %q, want only the normalizer", filter)
	}
}

func TestTempoHoldsThenStepsBackToNormalSpeed(t *testing.T) {
	filter := filterArgument(ffmpeg.Args("input", 12, true, ffmpeg.Tempo{Speed: 0.98, HoldSec: 10.5}))

	want := "asendcmd=c='10.2900 atempo tempo 0.9850;11.2750 atempo tempo 0.9900;12.2650 atempo tempo 0.9950;13.2600 atempo tempo 1.0000',atempo=0.9800,dynaudnorm="
	if !strings.HasPrefix(filter, want) {
		t.Errorf("filter = %q\nwant prefix %q", filter, want)
	}
}

func TestTempoSpeedsUpTowardNormalToo(t *testing.T) {
	filter := filterArgument(ffmpeg.Args("input", 0, false, ffmpeg.Tempo{Speed: 1.012, HoldSec: 2}))

	want := "asendcmd=c='2.0240 atempo tempo 1.0070;3.0310 atempo tempo 1.0020;4.0330 atempo tempo 1.0000',atempo=1.0120"
	if filter != want {
		t.Errorf("filter = %q\nwant %q", filter, want)
	}
}

func TestTempoDriftCountsTheSourceTimeGainedOrLost(t *testing.T) {
	cases := []struct {
		tempo ffmpeg.Tempo
		drift float64
	}{
		{ffmpeg.Tempo{Speed: 0.98, HoldSec: 10.5}, -0.24},
		{ffmpeg.Tempo{Speed: 1.012, HoldSec: 2}, 0.024 + 0.007 + 0.002},
		{ffmpeg.Tempo{Speed: 1, HoldSec: 10}, 0},
		{ffmpeg.Tempo{}, 0},
	}
	for _, c := range cases {
		if got := c.tempo.Drift(); math.Abs(got-c.drift) > 1e-9 {
			t.Errorf("%+v drifts %.4f s, want %.4f s", c.tempo, got, c.drift)
		}
	}
}

func TestFFmpegFollowsTheTempoSchedule(t *testing.T) {
	requireFFmpeg(t)
	input := filepath.Join(t.TempDir(), "tone.wav")
	if output, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=f=1000:d=30", input).CombinedOutput(); err != nil {
		t.Fatalf("failed to write the test tone: %v %s", err, output)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, input)
	}))
	defer server.Close()

	tempo := ffmpeg.Tempo{Speed: 0.9, HoldSec: 5}
	stream, err := ffmpeg.Start("ffmpeg", ffmpeg.Args(server.URL+"/tone.wav", 2, false, tempo), false, nil)
	if err != nil {
		t.Fatalf("Start returned %v", err)
	}
	defer stream.Stop()

	outputSec := float64(drain(t, stream)) / dsp.FramesPerSecond
	wantSec := 28 - tempo.Drift()
	if math.Abs(outputSec-wantSec) > 0.1 {
		t.Errorf("28s of source played for %.2fs, want %.2fs from the hold and the 0.5%% steps", outputSec, wantSec)
	}
}
