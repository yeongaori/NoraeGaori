package player

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

func checkCamelot(c *checkCollector) {
	cases := []struct {
		tonic int
		minor bool
		want  string
	}{
		{0, false, "8B"},
		{9, true, "8A"},
		{4, true, "9A"},
		{7, false, "9B"},
		{6, false, "2B"},
		{11, false, "1B"},
		{1, true, "12A"},
		{8, true, "1A"},
	}

	ok := true
	detail := ""
	for _, testCase := range cases {
		got := analysis.CamelotCode(testCase.tonic, testCase.minor)
		if got != testCase.want {
			ok = false
			detail = fmt.Sprintf("%s expected %s got %s", analysis.KeyName(testCase.tonic, testCase.minor), testCase.want, got)
			break
		}
	}
	if ok {
		detail = "reference Camelot codes match for major, minor and wrap-around keys"
	}
	c.add("camelot wheel mapping", ok, "%s", detail)

	confident := func(tonic int, minor bool) *analysis.TrackAnalysis {
		return &analysis.TrackAnalysis{Tonic: tonic, Minor: minor, KeyConfidence: 0.5}
	}

	same := analysis.CamelotDistance(confident(0, false), confident(0, false))
	relative := analysis.CamelotDistance(confident(0, false), confident(9, true))
	neighbour := analysis.CamelotDistance(confident(0, false), confident(7, false))
	tritone := analysis.CamelotDistance(confident(0, false), confident(6, false))
	unknown := analysis.CamelotDistance(confident(0, false), &analysis.TrackAnalysis{Tonic: 0, KeyConfidence: 0})

	c.add("camelot distance semantics",
		same == 0 && relative == 1 && neighbour == 1 && tritone == 6 && unknown == -1,
		"same=%d relative=%d neighbour=%d tritone=%d unknown=%d", same, relative, neighbour, tritone, unknown)
}

func synthesizeChordProgression(chords [][]int, seconds, sampleRate float64) []float32 {
	total := int(seconds * sampleRate)
	samples := make([]float32, total)
	chordLength := total / len(chords)
	if chordLength == 0 {
		return samples
	}

	for chordIndex, chord := range chords {
		start := chordIndex * chordLength
		end := start + chordLength
		if end > total {
			end = total
		}
		for _, note := range chord {
			frequency := 130.81 * math.Pow(2, float64(note)/12)
			for harmonic := 1; harmonic <= 4; harmonic++ {
				amplitude := 0.25 / float64(harmonic)
				partial := frequency * float64(harmonic)
				if partial >= sampleRate/2 {
					continue
				}
				step := 2 * math.Pi * partial / sampleRate
				phase := 0.0
				for i := start; i < end; i++ {
					samples[i] += float32(amplitude * math.Sin(phase))
					phase += step
				}
			}
		}
	}
	return samples
}

func checkKeyDetection(c *checkCollector) {
	majorProgression := [][]int{{0, 4, 7}, {5, 9, 12}, {7, 11, 14}, {0, 4, 7}}
	majorSamples := synthesizeChordProgression(majorProgression, 20, analysis.SampleRate)
	tonic, minor, confidence := analysis.AnalyzeKey(majorSamples, analysis.SampleRate)
	majorOK := confidence > 0 && ((tonic == 0 && !minor) || (tonic == 9 && minor))
	c.add("C major progression detects C major or its relative", majorOK,
		"got %s (%s), confidence %.4f", analysis.KeyName(tonic, minor), analysis.CamelotCode(tonic, minor), confidence)

	minorProgression := [][]int{{9, 12, 16}, {2, 5, 9}, {4, 8, 11}, {9, 12, 16}}
	minorSamples := synthesizeChordProgression(minorProgression, 20, analysis.SampleRate)
	tonic, minor, confidence = analysis.AnalyzeKey(minorSamples, analysis.SampleRate)
	minorOK := confidence > 0 && ((tonic == 9 && minor) || (tonic == 0 && !minor))
	c.add("A minor progression detects A minor or its relative", minorOK,
		"got %s (%s), confidence %.4f", analysis.KeyName(tonic, minor), analysis.CamelotCode(tonic, minor), confidence)

	transposed := [][]int{{3, 7, 10}, {8, 12, 15}, {10, 14, 17}, {3, 7, 10}}
	transposedSamples := synthesizeChordProgression(transposed, 20, analysis.SampleRate)
	tonic, minor, _ = analysis.AnalyzeKey(transposedSamples, analysis.SampleRate)
	transposedOK := (tonic == 3 && !minor) || (tonic == 0 && minor)
	c.add("transposed progression tracks the transposition", transposedOK,
		"got %s, expected D# major or C minor", analysis.KeyName(tonic, minor))

	silence := make([]float32, int(20*analysis.SampleRate))
	_, _, silentConfidence := analysis.AnalyzeKey(silence, analysis.SampleRate)
	c.add("silence yields no key confidence", silentConfidence == 0, "confidence %.6f", silentConfidence)

	short := make([]float32, 100)
	_, _, shortConfidence := analysis.AnalyzeKey(short, analysis.SampleRate)
	c.add("too short input yields no key", shortConfidence == 0, "confidence %.6f", shortConfidence)

	source := rand.New(rand.NewSource(7))
	noise := make([]float32, int(20*analysis.SampleRate))
	for i := range noise {
		noise[i] = float32(source.NormFloat64() * 0.2)
	}
	_, _, noiseConfidence := analysis.AnalyzeKey(noise, analysis.SampleRate)
	noiseChroma, _ := analysis.Chromagram(noise, analysis.SampleRate)
	musicChroma, _ := analysis.Chromagram(majorSamples, analysis.SampleRate)
	c.add("white noise stays below the confidence floor", noiseConfidence < analysis.KeyConfidenceFloor,
		"noise confidence %.4f (floor %.4f), noise chroma contrast %.3f vs music %.3f",
		noiseConfidence, analysis.KeyConfidenceFloor, analysis.ChromaContrast(noiseChroma), analysis.ChromaContrast(musicChroma))

	speechLike := make([]float32, int(20*analysis.SampleRate))
	phase := 0.0
	for i := range speechLike {
		if i%int(analysis.SampleRate/4) == 0 {
			phase = 0
		}
		frequency := 110 + 60*math.Sin(float64(i)/analysis.SampleRate*3)
		phase += 2 * math.Pi * frequency / analysis.SampleRate
		speechLike[i] = float32(0.4*math.Sin(phase) + 0.3*source.NormFloat64())
	}
	_, _, speechConfidence := analysis.AnalyzeKey(speechLike, analysis.SampleRate)
	c.add("noisy glide stays below the music confidence level", speechConfidence < confidence,
		"glide confidence %.4f vs tonal music %.4f", speechConfidence, confidence)
}

func synthesizeClickTrack(bpm, seconds, sampleRate float64, accentEvery, accentPhase int) []float32 {
	total := int(seconds * sampleRate)
	samples := make([]float32, total)
	interval := 60 / bpm * sampleRate
	burst := int(0.02 * sampleRate)
	source := rand.New(rand.NewSource(11))

	beat := 0
	for position := 0.0; position < float64(total); position += interval {
		amplitude := 0.5
		if accentEvery > 0 && beat%accentEvery == accentPhase {
			amplitude = 1.0
		}
		start := int(position)
		for i := 0; i < burst && start+i < total; i++ {
			decay := 1 - float64(i)/float64(burst)
			samples[start+i] += float32(amplitude * decay * source.NormFloat64() * 0.5)
		}
		beat++
	}
	return samples
}

func checkTempoSearchBounds(c *checkCollector) {
	frameRate := analysis.SampleRate / analysis.BeatHop
	minLag, maxLag := analysis.TempoLagRange(frameRate)

	minPeriod := float64(minLag) / frameRate
	maxPeriod := float64(maxLag) / frameRate
	lowerBound := 60.0 / analysis.MaxBPM
	upperBound := 60.0 / analysis.MinBPM

	c.add("fastest searchable lag is inside the accepted band",
		minPeriod >= lowerBound,
		"minLag=%d period=%.4fs must be >= %.5fs (%.0f BPM)", minLag, minPeriod, lowerBound, analysis.MaxBPM)

	c.add("slowest searchable lag is inside the accepted band",
		maxPeriod <= upperBound,
		"maxLag=%d period=%.4fs must be <= %.5fs (%.0f BPM)", maxLag, maxPeriod, upperBound, analysis.MinBPM)

	c.add("tempo search window is not empty", minLag >= 1 && minLag <= maxLag,
		"minLag=%d maxLag=%d at frameRate=%.4f", minLag, maxLag, frameRate)

	nearCeiling := synthesizeClickTrack(205, 30, analysis.SampleRate, 4, 0)
	nearAnalysis, nearErr := analysis.AnalyzeTrackSamples(nearCeiling, analysis.SampleRate)
	nearBPM := 0.0
	if nearErr == nil {
		nearBPM = nearAnalysis.BPM
	}
	c.add("a tempo just under the ceiling is refined, not clamped",
		nearErr == nil && math.Abs(nearBPM-205)/205 < 0.03,
		"205 BPM detected as %.2f (clamp would be %.2f), err=%v",
		nearBPM, (60*frameRate)/float64(minLag), nearErr)

	clampBPM := (60 * frameRate) / float64(minLag)
	c.add("a near-ceiling estimate is not the raw lag quantum",
		nearErr == nil && math.Abs(nearBPM-clampBPM) > 0.5,
		"detected %.4f vs lag-quantum %.4f", nearBPM, clampBPM)

	beyondFailures := 0
	beyondDetail := ""
	for _, trueBPM := range []float64{215, 225, 250} {
		beyond := synthesizeClickTrack(trueBPM, 30, analysis.SampleRate, 4, 0)
		track, err := analysis.AnalyzeTrackSamples(beyond, analysis.SampleRate)
		if err != nil {
			continue
		}
		ratio := trueBPM / track.BPM
		octaveResolved := math.Abs(ratio-2) < 0.05
		notClamped := math.Abs(track.BPM-clampBPM) > 0.5
		if !octaveResolved || !notClamped {
			beyondFailures++
			beyondDetail = fmt.Sprintf("%.0f BPM detected as %.2f (ratio %.3f)", trueBPM, track.BPM, ratio)
		}
	}
	if beyondFailures == 0 {
		beyondDetail = "215, 225 and 250 BPM resolve to their half-tempo grid, never the clamp"
	}
	c.add("a tempo beyond the ceiling resolves to an octave, never the clamp",
		beyondFailures == 0, "%s", beyondDetail)

	checkTempoFolding(c)
}

func checkTempoFolding(c *checkCollector) {
	cases := []struct {
		name     string
		bpmA     float64
		bpmB     float64
		expected float64
	}{
		{"identical tempos", 128, 128, 0},
		{"half time", 187.5, 93.75, 0},
		{"double time", 93.75, 187.5, 0},
		{"genuine wide gap", 128, 160, 0.25},
		{"near miss stays near", 128, 130, 2.0 / 128.0},
		{"three to two is not folded", 128, 192, 0.5},
		{"two to three is not folded", 192, 128, 1.0 / 3.0},
		{"loose double still folds", 200, 108, 0.08},
	}

	failures := 0
	detail := ""
	for _, testCase := range cases {
		got := analysis.TempoDelta(testCase.bpmA, testCase.bpmB)
		if math.Abs(got-testCase.expected) > 0.0001 {
			failures++
			detail = fmt.Sprintf("%s: got %.4f want %.4f", testCase.name, got, testCase.expected)
		}
	}
	if failures == 0 {
		detail = fmt.Sprintf("%d folding cases exact", len(cases))
	}
	c.add("tempo delta folds octave errors", failures == 0, "%s", detail)

	c.add("tempo delta rejects non-positive input",
		analysis.TempoDelta(0, 128) == 0 && analysis.TempoDelta(128, 0) == 0 && analysis.TempoDelta(-5, 128) == 0,
		"zero and negative input return 0")

	_, thirdsFactor := analysis.TempoDeltaFactor(128, 192)
	_, halfFactor := analysis.TempoDeltaFactor(187.5, 93.75)
	c.add("folding only applies to genuine octave relationships",
		thirdsFactor == 1.0 && halfFactor == 2.0,
		"3:2 factor=%.1fx half-time factor=%.1fx tolerance=%.2f", thirdsFactor, halfFactor, analysis.TempoFoldTolerance)

	c.add("signed tempo delta keeps direction and folds",
		analysis.SignedTempoDelta(128, 160) > 0 && analysis.SignedTempoDelta(160, 128) < 0 &&
			math.Abs(analysis.SignedTempoDelta(187.5, 93.75)) < 0.0001,
		"up=%.4f down=%.4f half=%.4f",
		analysis.SignedTempoDelta(128, 160), analysis.SignedTempoDelta(160, 128), analysis.SignedTempoDelta(187.5, 93.75))
}

func checkKeyContrastGate(c *checkCollector) {
	toneRate := analysis.SampleRate
	tonal := make([]float32, int(20*toneRate))
	phases := []float64{0, 0, 0}
	frequencies := []float64{261.63, 329.63, 392.00}
	for i := range tonal {
		var sample float64
		for f, freq := range frequencies {
			phases[f] += 2 * math.Pi * freq / toneRate
			sample += math.Sin(phases[f])
		}
		tonal[i] = float32(sample / float64(len(frequencies)) * 0.5)
	}
	_, _, tonalConfidence := analysis.AnalyzeKey(tonal, toneRate)
	c.add("a clearly tonal signal clears the contrast gate", tonalConfidence > 0,
		"confidence=%.4f floor=%.4f", tonalConfidence, analysis.KeyConfidenceFloor)

	noise := make([]float32, int(20*toneRate))
	generator := rand.New(rand.NewSource(7))
	for i := range noise {
		noise[i] = float32(generator.NormFloat64() * 0.2)
	}
	_, _, noiseConfidence := analysis.AnalyzeKey(noise, toneRate)
	c.add("broadband noise is rejected by the contrast gate", noiseConfidence == 0,
		"confidence=%.4f", noiseConfidence)
}

func checkBeatAnalysis(c *checkCollector) {
	tempos := []float64{70, 72, 90, 120, 140, 174, 185, 200, 205}
	failures := 0
	detail := ""
	for _, bpm := range tempos {
		samples := synthesizeClickTrack(bpm, 30, analysis.SampleRate, 0, 0)
		track, err := analysis.AnalyzeTrackSamples(samples, analysis.SampleRate)
		if err != nil {
			failures++
			detail = fmt.Sprintf("%.0f BPM click track failed: %v", bpm, err)
			continue
		}
		expected := bpm
		if bpm > analysis.MaxBPM {
			expected = bpm / 2
		}
		if math.Abs(track.BPM-expected)/expected > 0.03 {
			failures++
			detail = fmt.Sprintf("%.0f BPM click track detected as %.1f", bpm, track.BPM)
		}
	}
	if failures == 0 {
		detail = fmt.Sprintf("%d click tracks from %.0f to %.0f BPM detected within 3%%",
			len(tempos), tempos[0], tempos[len(tempos)-1])
	}
	c.add("bpm detection on synthetic click tracks", failures == 0, "%s", detail)

	short := make([]float32, int(4*analysis.SampleRate))
	_, err := analysis.AnalyzeTrackSamples(short, analysis.SampleRate)
	c.add("short track is rejected", err != nil, "error: %v", err)

	silence := make([]float32, int(30*analysis.SampleRate))
	_, err = analysis.AnalyzeTrackSamples(silence, analysis.SampleRate)
	c.add("silent track is rejected", err != nil, "error: %v", err)

	_, err = analysis.AnalyzeTrackSamples(make([]float32, 1000), 0)
	c.add("invalid sample rate is rejected", err != nil, "error: %v", err)

	accented := synthesizeClickTrack(120, 40, analysis.SampleRate, 4, 0)
	track, err := analysis.AnalyzeTrackSamples(accented, analysis.SampleRate)
	phaseOK := err == nil && track.DownbeatPhase >= 0 && track.DownbeatPhase < 4
	phase := -1
	if err == nil {
		phase = track.DownbeatPhase
	}
	c.add("downbeat phase is in range", phaseOK, "phase %d (bar length %d beats)", phase, analysis.BarBeats)

	if err == nil {
		grid := snapTransitionToGrid(1000, 0, track)
		bar := snapTransitionToBar(1000, 0, track)
		periodFrames := track.PeriodSec * dsp.FramesPerSecond
		firstBeatFrame := track.FirstBeat * dsp.FramesPerSecond

		gridBeat := (float64(grid) - firstBeatFrame) / periodFrames
		barBeat := (float64(bar) - firstBeatFrame) / periodFrames
		gridOK := math.Abs(gridBeat-math.Round(gridBeat)) < 0.02
		barPhase := math.Mod(math.Round(barBeat)-float64(track.DownbeatPhase), analysis.BarBeats)
		if barPhase < 0 {
			barPhase += analysis.BarBeats
		}
		barOK := math.Abs(barBeat-math.Round(barBeat)) < 0.02 && barPhase == 0
		c.add("transition snapping lands on the grid", gridOK && barOK,
			"beat snap %d (beat %.3f), bar snap %d (beat %.3f, phase %.0f)", grid, gridBeat, bar, barBeat, barPhase)

		c.add("bar snap is within one bar of beat snap",
			math.Abs(float64(bar-grid)) <= periodFrames*analysis.BarBeats,
			"beat snap %d, bar snap %d, bar length %.1f frames", grid, bar, periodFrames*analysis.BarBeats)
	}

	c.add("analysis carries key data", err == nil && track != nil,
		"click track analysis produced BPM %.1f and key %s", analysisBPM(track), analysisKey(track))
}

func analysisBPM(track *analysis.TrackAnalysis) float64 {
	if track == nil {
		return 0
	}
	return track.BPM
}

func analysisKey(track *analysis.TrackAnalysis) string {
	if track == nil {
		return "none"
	}
	return analysis.CamelotCode(track.Tonic, track.Minor)
}

func checkRealtimeBudget(c *checkCollector) {
	recipe := transition.Recipe{
		Volume: transition.VolumeCutInFadeOut,
		EQ:     transition.EQThreeBandFade,
		Filter: transition.FilterLowPassInHighPassOut,
		Effect: transition.EffectEchoHalfCutEnd,
		Loop:   transition.LoopFourBeats,
	}
	processor := transition.NewProcessor(recipe, 500, 0.5)

	aTone := &toneGenerator{frequency: 220, amplitude: 8000}
	bTone := &toneGenerator{frequency: 660, amplitude: 8000}
	aFrame := make([]int16, frameSize*channels)
	bFrame := make([]int16, frameSize*channels)

	frames := 500
	start := time.Now()
	for i := 0; i < frames; i++ {
		aTone.fill(aFrame)
		bTone.fill(bFrame)
		progress := float64(i) / float64(frames)
		aBuf := processor.ProcessA(aFrame, progress)
		bBuf := processor.ProcessB(bFrame, progress)
		processor.ApplyGains(aBuf, bBuf, progress, 1.0)
	}
	elapsed := time.Since(start)
	perFrame := elapsed / time.Duration(frames)

	c.add("heaviest recipe fits the realtime budget", perFrame < 4*time.Millisecond,
		"%.3fms per 20ms frame (%.1f%% of realtime)", float64(perFrame.Microseconds())/1000,
		float64(perFrame.Microseconds())/200)

	reverbRecipe := transition.DefaultRecipe()
	reverbRecipe.Effect = transition.EffectReverbOutCenter
	reverbProcessor := transition.NewProcessor(reverbRecipe, 500, 0.5)
	start = time.Now()
	for i := 0; i < frames; i++ {
		aTone.fill(aFrame)
		progress := float64(i) / float64(frames)
		reverbProcessor.ProcessA(aFrame, progress)
	}
	reverbPerFrame := time.Since(start) / time.Duration(frames)
	c.add("reverb fits the realtime budget", reverbPerFrame < 4*time.Millisecond,
		"%.3fms per 20ms frame", float64(reverbPerFrame.Microseconds())/1000)
}
