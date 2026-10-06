package player_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"noraegaori/internal/player"
)

const clickRate = 24000

func clickTrackWave(seconds, silentSeconds int) []byte {
	samples := make([]int16, (seconds+silentSeconds)*clickRate)
	beat := clickRate / 2
	for start := 0; start < seconds*clickRate; start += beat {
		for i := 0; i < 600 && start+i < len(samples); i++ {
			envelope := math.Exp(-float64(i) / 120)
			samples[start+i] = int16(20000 * envelope * math.Sin(2*math.Pi*1000*float64(i)/clickRate))
		}
	}

	var wave bytes.Buffer
	dataBytes := uint32(len(samples) * 2)
	fields := []any{
		[]byte("RIFF"), 36 + dataBytes, []byte("WAVE"), []byte("fmt "),
		uint32(16), uint16(1), uint16(1), uint32(clickRate),
		uint32(clickRate * 2), uint16(2), uint16(16),
		[]byte("data"), dataBytes, samples,
	}
	for _, field := range fields {
		_ = binary.Write(&wave, binary.LittleEndian, field)
	}
	return wave.Bytes()
}

func TestStreamSegmentSeeksAndTrimsTheEnding(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed; skipping the test that drives it")
	}
	wave := clickTrackWave(110, 10)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.ServeContent(writer, request, "clicks.wav", time.Time{}, bytes.NewReader(wave))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	segment, err := player.HookAnalyzeStreamSegment(ctx, server.URL+"/clicks.wav", 30, 90)
	if err != nil {
		t.Fatalf("analyzing the ending returned %v", err)
	}
	if segment.Offset != 30 {
		t.Errorf("offset = %.3f, want the 30 s seek point", segment.Offset)
	}
	if math.Abs(segment.Duration-80) > 1 {
		t.Errorf("duration = %.2f s, want about 80 s: 90 s read from 30 s minus the 10 s silent ending", segment.Duration)
	}
	if math.Abs(segment.BPM-120) > 1 {
		t.Errorf("tempo = %.1f BPM, want the 120 BPM click", segment.BPM)
	}

	head, err := player.HookAnalyzeStreamSegment(ctx, server.URL+"/clicks.wav", 0, 20)
	if err != nil {
		t.Fatalf("analyzing the start returned %v", err)
	}
	if head.Offset != 0 || math.Abs(head.Duration-20) > 0.5 {
		t.Errorf("start = offset %.2f, %.2f s, want offset 0 and the 20 s asked for", head.Offset, head.Duration)
	}
}

func TestStreamSegmentRejectsASilentStretch(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed; skipping the test that drives it")
	}
	wave := clickTrackWave(5, 30)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.ServeContent(writer, request, "quiet.wav", time.Time{}, bytes.NewReader(wave))
	}))
	defer server.Close()

	if _, err := player.HookAnalyzeStreamSegment(context.Background(), server.URL+"/quiet.wav", 10, 20); err == nil {
		t.Error("a silent stretch was analyzed, want an error")
	}
}
