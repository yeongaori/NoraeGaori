package player

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// openSharedLib resolves a shared library by name via the OS dynamic loader.
// Implemented per-platform in opus_libopus_unix.go / opus_libopus_windows.go.

// libopus C constants used by the bot.
const (
	opusApplicationAudio = 2049 // OPUS_APPLICATION_AUDIO
	opusSetBitrateReq    = 4002 // OPUS_SET_BITRATE_REQUEST
)

// libopus function pointers, populated once on first successful dlopen.
var (
	libopusOnce      sync.Once
	libopusLibName   string
	libopusLoadErr   error

	cOpusEncoderCreate  func(fs, channels, application int32, errPtr *int32) uintptr
	cOpusEncoderDestroy func(st uintptr)
	cOpusEncoderCtl     func(st uintptr, request, value int32) int32
	cOpusEncode         func(st uintptr, pcm *int16, frameSize int32, data *uint8, maxBytes int32) int32
	cOpusStrerror       func(err int32) *byte
)

// libopusCandidates returns the per-platform shared-library names to try, in
// order. The first one that the OS dynamic loader can resolve wins.
func libopusCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"libopus-0.dll", "opus.dll"}
	case "darwin":
		return []string{"libopus.0.dylib", "libopus.dylib"}
	default:
		return []string{"libopus.so.0", "libopus.so"}
	}
}

// tryLoadLibopus dlopens libopus and registers the encoder symbols. Idempotent
// — only the first call performs the load. Returns the resolved library name
// on success, or an error describing why the probe failed.
func tryLoadLibopus() (libName string, err error) {
	libopusOnce.Do(func() {
		var handle uintptr
		var lastErr error
		var resolved string
		for _, name := range libopusCandidates() {
			h, e := openSharedLib(name)
			if e == nil {
				handle = h
				resolved = name
				break
			}
			lastErr = e
		}
		if handle == 0 {
			if lastErr == nil {
				lastErr = errors.New("no libopus candidate library found")
			}
			libopusLoadErr = lastErr
			return
		}

		// RegisterLibFunc panics on missing symbols; treat that as a load failure.
		defer func() {
			if r := recover(); r != nil {
				libopusLoadErr = fmt.Errorf("libopus symbol registration failed: %v", r)
				libopusLibName = ""
			}
		}()
		purego.RegisterLibFunc(&cOpusEncoderCreate, handle, "opus_encoder_create")
		purego.RegisterLibFunc(&cOpusEncoderDestroy, handle, "opus_encoder_destroy")
		purego.RegisterLibFunc(&cOpusEncoderCtl, handle, "opus_encoder_ctl")
		purego.RegisterLibFunc(&cOpusEncode, handle, "opus_encode")
		purego.RegisterLibFunc(&cOpusStrerror, handle, "opus_strerror")
		libopusLibName = resolved
	})
	return libopusLibName, libopusLoadErr
}

// libopusEncoder is the native opus encoder backed by libopus loaded via purego.
type libopusEncoder struct {
	st       uintptr
	channels int
}

func newLibopusEncoder(sampleRate, channels int) (*libopusEncoder, error) {
	if libopusLibName == "" {
		return nil, errors.New("libopus is not loaded")
	}
	var errCode int32
	st := cOpusEncoderCreate(int32(sampleRate), int32(channels), opusApplicationAudio, &errCode)
	if st == 0 || errCode != 0 {
		return nil, fmt.Errorf("opus_encoder_create failed: %s", opusStrError(errCode))
	}
	enc := &libopusEncoder{st: st, channels: channels}
	runtime.SetFinalizer(enc, func(e *libopusEncoder) {
		if e.st != 0 {
			cOpusEncoderDestroy(e.st)
			e.st = 0
		}
	})
	return enc, nil
}

func (e *libopusEncoder) SetBitrate(bitrate int) error {
	rc := cOpusEncoderCtl(e.st, opusSetBitrateReq, int32(bitrate))
	if rc < 0 {
		return fmt.Errorf("opus_encoder_ctl SET_BITRATE failed: %s", opusStrError(rc))
	}
	return nil
}

func (e *libopusEncoder) Encode(pcm []int16, output []byte) (int, error) {
	if len(pcm) == 0 {
		return 0, errors.New("opus_encode: empty PCM input")
	}
	if len(output) == 0 {
		return 0, errors.New("opus_encode: empty output buffer")
	}
	frameSize := int32(len(pcm) / e.channels)
	n := cOpusEncode(
		e.st,
		(*int16)(unsafe.Pointer(&pcm[0])),
		frameSize,
		(*uint8)(unsafe.Pointer(&output[0])),
		int32(len(output)),
	)
	if n < 0 {
		return 0, fmt.Errorf("opus_encode failed: %s", opusStrError(n))
	}
	return int(n), nil
}

// opusStrError converts a libopus error code to its human-readable string by
// calling opus_strerror. Returns a numeric fallback if the symbol isn't loaded.
func opusStrError(code int32) string {
	if cOpusStrerror == nil {
		return fmt.Sprintf("opus error %d", code)
	}
	p := cOpusStrerror(code)
	if p == nil {
		return fmt.Sprintf("opus error %d", code)
	}
	var n int
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
