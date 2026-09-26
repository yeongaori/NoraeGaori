package transition

import "noraegaori/internal/audio/dsp"

const loopSeamSamples = 240

type BeatLoop struct {
	samples  []int16
	length   int
	seam     int
	captured int
	cursor   int
	out      []int16
}

func CaptureBeatLoop(lengthSamples int) *BeatLoop {
	seam := min(loopSeamSamples, lengthSamples/2)
	return &BeatLoop{
		samples: make([]int16, (lengthSamples+seam)*dsp.Channels),
		length:  lengthSamples,
		seam:    seam,
		out:     make([]int16, dsp.FrameSize*dsp.Channels),
	}
}

func (l *BeatLoop) IsReady() bool {
	return l.captured >= l.length+l.seam
}

func (l *BeatLoop) Next(live []int16) []int16 {
	if live == nil && !l.IsReady() {
		return nil
	}

	for pair := 0; pair < dsp.FrameSize; pair++ {
		l.capture(live, pair)
		l.emit(pair)
		l.cursor++
	}
	return l.out
}

func (l *BeatLoop) capture(live []int16, pair int) {
	if l.IsReady() || live == nil {
		return
	}
	for channel := 0; channel < dsp.Channels; channel++ {
		var sample int16
		if index := pair*dsp.Channels + channel; index < len(live) {
			sample = live[index]
		}
		l.samples[l.captured*dsp.Channels+channel] = sample
	}
	l.captured++
}

func (l *BeatLoop) emit(pair int) {
	position := l.cursor
	if position >= l.length {
		position = (l.cursor - l.length) % l.length
	}

	for channel := 0; channel < dsp.Channels; channel++ {
		sample := float64(l.samples[position*dsp.Channels+channel])
		if l.cursor >= l.length && position < l.seam {
			blend := (float64(position) + 0.5) / float64(l.seam)
			continued := float64(l.samples[(l.length+position)*dsp.Channels+channel])
			sample = sample*dsp.QSinIn(blend) + continued*dsp.QSinOut(blend)
		}
		l.out[pair*dsp.Channels+channel] = dsp.ClampToInt16(sample)
	}
}
