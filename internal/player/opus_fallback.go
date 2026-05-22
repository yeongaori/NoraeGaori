package player

import (
	"github.com/jj11hh/opus"
)

type wasmEncoder struct {
	enc *opus.Encoder
}

func newWASMEncoder(sampleRate, channels int) (*wasmEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, err
	}
	return &wasmEncoder{enc: enc}, nil
}

func (e *wasmEncoder) SetBitrate(bitrate int) error {
	return e.enc.SetBitrate(bitrate)
}

func (e *wasmEncoder) Encode(pcm []int16, output []byte) (int, error) {
	return e.enc.Encode(pcm, output)
}
