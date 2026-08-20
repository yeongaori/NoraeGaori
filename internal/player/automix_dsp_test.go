package player

import (
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

func checkBiquads(c *checkCollector) {
	lowPassLow := filterResponse(func(f *dsp.Biquad) { f.SetLowpass(500, 0.707) }, 100)
	lowPassHigh := filterResponse(func(f *dsp.Biquad) { f.SetLowpass(500, 0.707) }, 8000)
	c.add("biquad lowpass shape", lowPassLow > 0.85 && lowPassHigh < 0.05,
		"100Hz gain %.3f (want >0.85), 8000Hz gain %.4f (want <0.05)", lowPassLow, lowPassHigh)

	highPassLow := filterResponse(func(f *dsp.Biquad) { f.SetHighpass(2000, 0.707) }, 100)
	highPassHigh := filterResponse(func(f *dsp.Biquad) { f.SetHighpass(2000, 0.707) }, 12000)
	c.add("biquad highpass shape", highPassHigh > 0.85 && highPassLow < 0.05,
		"12000Hz gain %.3f (want >0.85), 100Hz gain %.4f (want <0.05)", highPassHigh, highPassLow)

	shelfLow := filterResponse(func(f *dsp.Biquad) { f.SetLowShelf(transition.EQLowFreq, transition.EQShelfQ, transition.EQKillDB) }, 60)
	shelfHigh := filterResponse(func(f *dsp.Biquad) { f.SetLowShelf(transition.EQLowFreq, transition.EQShelfQ, transition.EQKillDB) }, 6000)
	c.add("biquad low shelf bass kill", shelfLow < 0.05 && shelfHigh > 0.9,
		"60Hz gain %.4f (want <0.05), 6000Hz gain %.3f (want >0.9)", shelfLow, shelfHigh)

	highShelfHigh := filterResponse(func(f *dsp.Biquad) { f.SetHighShelf(transition.EQHighFreq, transition.EQShelfQ, transition.EQKillDB) }, 12000)
	highShelfLow := filterResponse(func(f *dsp.Biquad) { f.SetHighShelf(transition.EQHighFreq, transition.EQShelfQ, transition.EQKillDB) }, 200)
	c.add("biquad high shelf treble kill", highShelfHigh < 0.05 && highShelfLow > 0.9,
		"12000Hz gain %.4f (want <0.05), 200Hz gain %.3f (want >0.9)", highShelfHigh, highShelfLow)

	peakCut := filterResponse(func(f *dsp.Biquad) { f.SetPeaking(transition.EQMidFreq, transition.EQMidQ, transition.EQKillDB) }, transition.EQMidFreq)
	c.add("biquad peaking mid cut", peakCut < 0.1, "1000Hz gain %.4f (want <0.1)", peakCut)

	var bypass dsp.Biquad
	bypass.SetBypass()
	phase := 0.0
	original := sineFloatFrame(1000, 9000, &phase)
	copied := make([]float64, len(original))
	copy(copied, original)
	bypass.ProcessStereo(copied)
	identical := true
	for i := range original {
		if original[i] != copied[i] {
			identical = false
			break
		}
	}
	c.add("biquad bypass is transparent", identical, "sample-for-sample match: %v", identical)

	extremes := []struct {
		name  string
		setup func(*dsp.Biquad)
	}{
		{"lowpass 0.1Hz", func(f *dsp.Biquad) { f.SetLowpass(0.1, 0.707) }},
		{"lowpass 96000Hz", func(f *dsp.Biquad) { f.SetLowpass(96000, 0.707) }},
		{"highpass 0Hz", func(f *dsp.Biquad) { f.SetHighpass(0, 0) }},
		{"peaking negative Q", func(f *dsp.Biquad) { f.SetPeaking(1000, -5, -40) }},
		{"lowshelf huge gain", func(f *dsp.Biquad) { f.SetLowShelf(250, 0.707, 120) }},
	}
	stable := true
	details := ""
	for _, extreme := range extremes {
		var filter dsp.Biquad
		extreme.setup(&filter)
		localPhase := 0.0
		for frame := 0; frame < 50; frame++ {
			buf := sineFloatFrame(440, 10000, &localPhase)
			filter.ProcessStereo(buf)
			if !bufferFinite(buf) {
				stable = false
				details = extreme.name + " produced non-finite output"
				break
			}
			if bufferPeak(buf) > 1e9 {
				stable = false
				details = extreme.name + " diverged"
				break
			}
		}
		if !stable {
			break
		}
	}
	if stable {
		details = "all extreme coefficient cases stayed finite and bounded"
	}
	c.add("biquad extreme parameters stable", stable, "%s", details)
}

func checkDelayAndReverb(c *checkCollector) {
	delay := dsp.NewDelayLine()
	delay.SetDelaySeconds(0.1)
	delay.Feedback = 0.5
	delay.Wet = 1
	delay.Dry = 1

	impulse := make([]float64, frameSize*channels)
	impulse[0] = 10000
	impulse[1] = 10000
	delay.ProcessStereo(impulse)

	silent := make([]float64, frameSize*channels)
	var firstEchoFrame int
	var firstEchoPeak float64
	for frame := 1; frame <= 10; frame++ {
		dsp.SilenceFloat(silent)
		delay.ProcessStereo(silent)
		peak := bufferPeak(silent)
		if peak > 100 && firstEchoFrame == 0 {
			firstEchoFrame = frame
			firstEchoPeak = peak
		}
	}
	c.add("delay line echo timing", firstEchoFrame == 5,
		"first echo at frame %d (want 5 for 100ms), peak %.0f", firstEchoFrame, firstEchoPeak)

	feedbackDecay := true
	var peaks []float64
	for frame := 0; frame < 20; frame++ {
		dsp.SilenceFloat(silent)
		delay.ProcessStereo(silent)
		peak := bufferPeak(silent)
		if peak > 1 {
			peaks = append(peaks, peak)
		}
	}
	for i := 1; i < len(peaks); i++ {
		if peaks[i] > peaks[i-1]*1.05 {
			feedbackDecay = false
		}
	}
	c.add("delay line feedback decays", feedbackDecay && len(peaks) > 0,
		"%d decaying echo peaks observed", len(peaks))

	unit := dsp.NewReverb()
	wetImpulse := make([]float64, frameSize*channels)
	wetImpulse[0] = 20000
	wetImpulse[1] = 20000
	unit.ProcessStereo(wetImpulse, 0, 1)

	tailEnergy := 0.0
	maxTailPeak := 0.0
	finite := true
	for frame := 0; frame < 60; frame++ {
		dsp.SilenceFloat(silent)
		unit.ProcessStereo(silent, 0, 1)
		if !bufferFinite(silent) {
			finite = false
			break
		}
		tailEnergy += bufferRMS(silent)
		if bufferPeak(silent) > maxTailPeak {
			maxTailPeak = bufferPeak(silent)
		}
	}
	c.add("reverb produces bounded tail", finite && tailEnergy > 0 && maxTailPeak < 40000,
		"tail energy %.1f, peak %.0f, finite %v", tailEnergy, maxTailPeak, finite)

	late := 0.0
	for frame := 0; frame < 400; frame++ {
		dsp.SilenceFloat(silent)
		unit.ProcessStereo(silent, 0, 1)
		late = bufferRMS(silent)
	}
	c.add("reverb tail decays to near silence", late < 1, "rms after 460 frames: %.6f", late)
}

func checkConversions(c *checkCollector) {
	src := []float64{40000, -40000, 100.4, -100.4, 0}
	dst := make([]int16, len(src))
	dsp.FloatToFrame(src, dst)
	c.add("float to frame clamps", dst[0] == 32767 && dst[1] == -32768 && dst[2] == 100 && dst[4] == 0,
		"got %v", dst)

	frame := []int16{1000, -1000, 32767}
	out := make([]float64, 5)
	dsp.FrameToFloat(frame, out)
	c.add("frame to float zero pads", out[0] == 1000 && out[2] == 32767 && out[3] == 0 && out[4] == 0,
		"got %v", out)

	ramp := make([]float64, frameSize*channels)
	for i := range ramp {
		ramp[i] = 1000
	}
	dsp.ApplyGainRamp(ramp, 0, 1)
	c.add("gain ramp endpoints", ramp[0] == 0 && ramp[len(ramp)-1] > 900 && ramp[len(ramp)-1] <= 1000,
		"first %.1f, last %.1f", ramp[0], ramp[len(ramp)-1])

	flat := make([]float64, 8)
	for i := range flat {
		flat[i] = 500
	}
	dsp.ApplyGainRamp(flat, 0.5, 0.5)
	c.add("gain ramp constant factor", flat[0] == 250 && flat[7] == 250, "got %v", flat[:2])
}
