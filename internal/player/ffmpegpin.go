package player

import (
	"sync"

	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/dependency"
)

var (
	acquireFFmpeg = dependency.AcquireFFmpeg
	releaseFFmpeg = (*dependency.Binary).Release
)

type ffmpegPin struct {
	mu          sync.Mutex
	binary      *dependency.Binary
	liveStreams int
}

func (pin *ffmpegPin) claim() (*dependency.Binary, func()) {
	pin.mu.Lock()
	defer pin.mu.Unlock()

	if pin.liveStreams == 0 {
		pin.binary = acquireFFmpeg()
	}
	pin.liveStreams++

	var once sync.Once
	return pin.binary, func() { once.Do(pin.release) }
}

func (pin *ffmpegPin) release() {
	pin.mu.Lock()
	defer pin.mu.Unlock()

	pin.liveStreams--
	if pin.liveStreams == 0 {
		releaseFFmpeg(pin.binary)
		pin.binary = nil
	}
}

func (player *GuildPlayer) startStream(args []string, collectTail bool) (audioStream, error) {
	binary, done := player.ffmpeg.claim()
	stream, err := newAudioStream(binary.Path, args, collectTail, done)
	if err != nil {
		done()
	}
	return stream, err
}

func (player *GuildPlayer) startLiveStream(url string, bitrate int, normalization, collectTail bool) (audioStream, error) {
	binary, done := player.ffmpeg.claim()
	pipe, err := getLiveStreamPipe(url, false, bitrate, 0, binary)
	if err != nil {
		done()
		return nil, err
	}

	stream, err := newAudioStreamPipe(binary.Path, ffmpeg.PipeArgs(normalization), pipe, collectTail, done)
	if err != nil {
		done()
	}
	return stream, err
}
