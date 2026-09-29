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
	newAudioStream = func(binary string, args []string, collectTail bool, onExit func()) (audioStream, error) {
		return ffmpeg.Start(binary, args, collectTail, onExit)
	}
	newAudioStreamPipe = func(binary string, args []string, stdin io.ReadCloser, collectTail bool, onExit func()) (audioStream, error) {
		return ffmpeg.StartPipe(binary, args, stdin, collectTail, onExit)
	}
)
