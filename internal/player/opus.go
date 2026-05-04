package player

import (
	"sync"

	"noraegaori/pkg/logger"
)

type opusBackend int

const (
	backendUninitialized opusBackend = iota
	backendNative
	backendWASM
)

var (
	backendOnce   sync.Once
	activeBackend opusBackend
)

// initBackend probes libopus once and remembers which encoder backend to use
// for the rest of the process. Native (libopus via purego) is preferred; on
// any failure the WASM encoder is used and a warning is logged.
func initBackend() {
	backendOnce.Do(func() {
		libName, err := tryLoadLibopus()
		if err != nil {
			activeBackend = backendWASM
			logger.Warnf("libopus not available at runtime (%v); using WASM opus encoder", err)
			return
		}
		activeBackend = backendNative
		logger.Infof("Loaded libopus via dlopen (%s); using native opus encoder", libName)
	})
}

// OpusEncoder is the public encoder type used by the audio pipeline. Internally
// it dispatches to either the libopus or WASM backend, chosen at startup.
type OpusEncoder struct {
	native *libopusEncoder
	wasm   *wasmEncoder
}

// NewOpusEncoder constructs an encoder using whichever backend was selected
// during the first call (libopus if available at runtime, WASM otherwise).
func NewOpusEncoder(sampleRate, channels int) (*OpusEncoder, error) {
	initBackend()
	if activeBackend == backendNative {
		e, err := newLibopusEncoder(sampleRate, channels)
		if err != nil {
			return nil, err
		}
		return &OpusEncoder{native: e}, nil
	}
	e, err := newWASMEncoder(sampleRate, channels)
	if err != nil {
		return nil, err
	}
	return &OpusEncoder{wasm: e}, nil
}

func (e *OpusEncoder) SetBitrate(bitrate int) error {
	if e.native != nil {
		return e.native.SetBitrate(bitrate)
	}
	return e.wasm.SetBitrate(bitrate)
}

func (e *OpusEncoder) Encode(pcm []int16, output []byte) (int, error) {
	if e.native != nil {
		return e.native.Encode(pcm, output)
	}
	return e.wasm.Encode(pcm, output)
}

// GetEncoderType returns a human-readable label for the active backend.
func GetEncoderType() string {
	initBackend()
	switch activeBackend {
	case backendNative:
		return "native (libopus, dlopen)"
	case backendWASM:
		return "WASM (fallback)"
	default:
		return "uninitialized"
	}
}
