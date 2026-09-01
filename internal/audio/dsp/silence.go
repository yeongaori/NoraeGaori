package dsp

const (
	silenceSampleLevel = 327
	silencePeakLevel   = 0.01
)

func FrameSilent(buf []int16) bool {
	for _, s := range buf {
		if s > silenceSampleLevel || s < -silenceSampleLevel {
			return false
		}
	}
	return true
}

func ApplyGain(buf []int16, factor float64) {
	for i := 0; i < len(buf); i++ {
		sample := float64(buf[i]) * factor
		if sample > 32767 {
			buf[i] = 32767
		} else if sample < -32768 {
			buf[i] = -32768
		} else {
			buf[i] = int16(sample)
		}
	}
}

func LeadingSilentSamples(samples []float32) int {
	for i, s := range samples {
		if s > silencePeakLevel || s < -silencePeakLevel {
			return i
		}
	}
	return len(samples)
}

func TrailingSilentSamples(samples []float32) int {
	last := len(samples) - 1
	for ; last >= 0; last-- {
		if samples[last] > silencePeakLevel || samples[last] < -silencePeakLevel {
			break
		}
	}
	return len(samples) - 1 - last
}
