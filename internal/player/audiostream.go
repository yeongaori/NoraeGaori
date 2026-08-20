package player

import (
	"io"

	"noraegaori/internal/audio/ffmpeg"
)

type audioStream interface {
	PCM() <-chan []int16
	Errs() <-chan error
	EndState() *ffmpeg.EndState
	Buffered() int
	Diagnostics() string
	Stop()
}

type streamRef struct {
	stream audioStream
}

var (
	newAudioStream     = func(args []string, collectTail bool) (audioStream, error) { return ffmpeg.Start(args, collectTail) }
	newAudioStreamPipe = func(args []string, stdin io.ReadCloser, collectTail bool) (audioStream, error) {
		return ffmpeg.StartPipe(args, stdin, collectTail)
	}
)
