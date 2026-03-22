//go:build opus_native
// +build opus_native

package player

import (
	"gopkg.in/hraban/opus.v2"
)

// OpusEncoder wraps the native opus encoder
type OpusEncoder struct {
	enc *opus.Encoder
}

// NewOpusEncoder creates a new opus encoder using native libopus
func NewOpusEncoder(sampleRate, channels int) (*OpusEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, err
	}
	return &OpusEncoder{enc: enc}, nil
}

// SetBitrate sets the bitrate for the encoder
func (e *OpusEncoder) SetBitrate(bitrate int) error {
	return e.enc.SetBitrate(bitrate)
}

// Encode encodes PCM samples to Opus
func (e *OpusEncoder) Encode(pcm []int16, output []byte) (int, error) {
	return e.enc.Encode(pcm, output)
}

// GetEncoderType returns the type of encoder being used
func GetEncoderType() string {
	return "native (libopus)"
}
