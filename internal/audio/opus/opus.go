package opus

import (
	"sync"

	"noraegaori/internal/logger"
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

func initBackend() {
	backendOnce.Do(func() {
		libName, err := tryLoadLibopus()
		if err != nil {
			activeBackend = backendWASM
			logger.Warnf("libopus not available at runtime (%v); using WASM opus encoder", err)
			return
		}
		activeBackend = backendNative
		logger.Debugf("Loaded libopus via dlopen (%s); using native opus encoder", libName)
	})
}

type Encoder struct {
	native *libopusEncoder
	wasm   *wasmEncoder
}

func NewEncoder(sampleRate, channels int) (*Encoder, error) {
	initBackend()
	if activeBackend == backendNative {
		e, err := newLibopusEncoder(sampleRate, channels)
		if err != nil {
			return nil, err
		}
		return &Encoder{native: e}, nil
	}
	e, err := newWASMEncoder(sampleRate, channels)
	if err != nil {
		return nil, err
	}
	return &Encoder{wasm: e}, nil
}

func (e *Encoder) SetBitrate(bitrate int) error {
	if e.native != nil {
		return e.native.SetBitrate(bitrate)
	}
	return e.wasm.SetBitrate(bitrate)
}

func (e *Encoder) Encode(pcm []int16, output []byte) (int, error) {
	if e.native != nil {
		return e.native.Encode(pcm, output)
	}
	return e.wasm.Encode(pcm, output)
}

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
