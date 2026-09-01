package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/rand"
	"testing"
)

type chunkedReader struct {
	data    []byte
	maxRead int
	rng     *rand.Rand
	failure error
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.failure != nil {
			return 0, r.failure
		}
		return 0, io.EOF
	}

	n := 1 + r.rng.Intn(r.maxRead)
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.data) {
		n = len(r.data)
	}
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

func encodeFloat32LE(values []float32, trailingBytes int) []byte {
	var raw bytes.Buffer
	for _, value := range values {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], math.Float32bits(value))
		raw.Write(word[:])
	}
	raw.Write(make([]byte, trailingBytes))
	return raw.Bytes()
}

func TestReadFloat32SamplesDecodesAcrossChunkBoundaries(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	for _, count := range []int{0, 1, 2, 100, 4097} {
		for _, trailing := range []int{0, 1, 2, 3} {
			want := make([]float32, count)
			for i := range want {
				want[i] = rng.Float32()*2 - 1
			}

			raw := encodeFloat32LE(want, trailing)
			reader := &chunkedReader{data: raw, maxRead: 13, rng: rand.New(rand.NewSource(9))}

			got, err := readFloat32Samples(reader, int64(len(raw)))
			if err != nil {
				t.Fatalf("%d samples, %d trailing bytes: %v", count, trailing, err)
			}
			if len(got) != len(want) {
				t.Fatalf("%d samples, %d trailing bytes: len = %d, want %d",
					count, trailing, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%d samples, %d trailing bytes: sample[%d] = %v, want %v",
						count, trailing, i, got[i], want[i])
				}
			}
		}
	}
}

func TestReadFloat32SamplesStopsAtMaxBytes(t *testing.T) {
	want := make([]float32, 64)
	for i := range want {
		want[i] = float32(i)
	}
	raw := encodeFloat32LE(want, 0)

	reader := &chunkedReader{data: raw, maxRead: 7, rng: rand.New(rand.NewSource(3))}
	got, err := readFloat32Samples(reader, 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
	for i, sample := range got {
		if sample != want[i] {
			t.Fatalf("sample[%d] = %v, want %v", i, sample, want[i])
		}
	}
}

func TestReadFloat32SamplesReportsReadFailure(t *testing.T) {
	failure := errors.New("pipe broke")
	raw := encodeFloat32LE([]float32{1, 2, 3}, 0)

	reader := &chunkedReader{data: raw, maxRead: 4, rng: rand.New(rand.NewSource(5)), failure: failure}

	samples, err := readFloat32Samples(reader, int64(len(raw))+16)
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
	}
	if samples != nil {
		t.Errorf("samples = %v, want nil on failure", samples)
	}
}
