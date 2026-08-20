package player

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

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

func noiseSamples(seconds float64, seed int64) []float32 {
	source := rand.New(rand.NewSource(seed))
	noise := make([]float32, int(seconds*analysis.SampleRate))
	for i := range noise {
		noise[i] = float32(source.NormFloat64() * 0.2)
	}
	return noise
}

func TestCamelotWheelMapping(t *testing.T) {
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

	for _, testCase := range cases {
		got := analysis.CamelotCode(testCase.tonic, testCase.minor)
		if got != testCase.want {
			t.Errorf("%s = %s, want %s", analysis.KeyName(testCase.tonic, testCase.minor), got, testCase.want)
		}
	}
}

func TestCamelotDistanceSemantics(t *testing.T) {
	confident := func(tonic int, minor bool) *analysis.TrackAnalysis {
		return &analysis.TrackAnalysis{Tonic: tonic, Minor: minor, KeyConfidence: 0.5}
	}
	root := confident(0, false)

	cases := []struct {
		name  string
		other *analysis.TrackAnalysis
		want  int
	}{
		{"same key", confident(0, false), 0},
		{"relative minor", confident(9, true), 1},
		{"neighbour", confident(7, false), 1},
		{"tritone", confident(6, false), 6},
		{"unknown key", &analysis.TrackAnalysis{Tonic: 0, KeyConfidence: 0}, -1},
	}

	for _, testCase := range cases {
		if got := analysis.CamelotDistance(root, testCase.other); got != testCase.want {
			t.Errorf("%s = %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestCMajorProgressionDetectsCMajorOrItsRelative(t *testing.T) {
	samples := synthesizeChordProgression([][]int{{0, 4, 7}, {5, 9, 12}, {7, 11, 14}, {0, 4, 7}}, 20, analysis.SampleRate)
	tonic, minor, confidence := analysis.AnalyzeKey(samples, analysis.SampleRate)

	if confidence <= 0 {
		t.Fatalf("confidence = %.4f, want > 0", confidence)
	}
	if !((tonic == 0 && !minor) || (tonic == 9 && minor)) {
		t.Errorf("got %s (%s), want C major or A minor", analysis.KeyName(tonic, minor), analysis.CamelotCode(tonic, minor))
	}
}

func TestAMinorProgressionDetectsAMinorOrItsRelative(t *testing.T) {
	samples := synthesizeChordProgression([][]int{{9, 12, 16}, {2, 5, 9}, {4, 8, 11}, {9, 12, 16}}, 20, analysis.SampleRate)
	tonic, minor, confidence := analysis.AnalyzeKey(samples, analysis.SampleRate)

	if confidence <= 0 {
		t.Fatalf("confidence = %.4f, want > 0", confidence)
	}
	if !((tonic == 9 && minor) || (tonic == 0 && !minor)) {
		t.Errorf("got %s (%s), want A minor or C major", analysis.KeyName(tonic, minor), analysis.CamelotCode(tonic, minor))
	}
}

func TestTransposedProgressionTracksTheTransposition(t *testing.T) {
	samples := synthesizeChordProgression([][]int{{3, 7, 10}, {8, 12, 15}, {10, 14, 17}, {3, 7, 10}}, 20, analysis.SampleRate)
	tonic, minor, _ := analysis.AnalyzeKey(samples, analysis.SampleRate)

	if !((tonic == 3 && !minor) || (tonic == 0 && minor)) {
		t.Errorf("got %s, want D# major or C minor", analysis.KeyName(tonic, minor))
	}
}

func TestSilenceYieldsNoKeyConfidence(t *testing.T) {
	silence := make([]float32, int(20*analysis.SampleRate))

	if _, _, confidence := analysis.AnalyzeKey(silence, analysis.SampleRate); confidence != 0 {
		t.Errorf("confidence = %.6f, want 0", confidence)
	}
}

func TestTooShortInputYieldsNoKey(t *testing.T) {
	if _, _, confidence := analysis.AnalyzeKey(make([]float32, 100), analysis.SampleRate); confidence != 0 {
		t.Errorf("confidence = %.6f, want 0", confidence)
	}
}

func TestWhiteNoiseStaysBelowTheConfidenceFloor(t *testing.T) {
	noise := noiseSamples(20, 7)
	_, _, confidence := analysis.AnalyzeKey(noise, analysis.SampleRate)

	if confidence >= analysis.KeyConfidenceFloor {
		music := synthesizeChordProgression([][]int{{0, 4, 7}, {5, 9, 12}, {7, 11, 14}, {0, 4, 7}}, 20, analysis.SampleRate)
		noiseChroma, _ := analysis.Chromagram(noise, analysis.SampleRate)
		musicChroma, _ := analysis.Chromagram(music, analysis.SampleRate)
		t.Errorf("confidence = %.4f, want < floor %.4f (chroma contrast %.3f vs music %.3f)",
			confidence, analysis.KeyConfidenceFloor, analysis.ChromaContrast(noiseChroma), analysis.ChromaContrast(musicChroma))
	}
}

func TestNoisyGlideStaysBelowTheMusicConfidenceLevel(t *testing.T) {
	music := synthesizeChordProgression([][]int{{9, 12, 16}, {2, 5, 9}, {4, 8, 11}, {9, 12, 16}}, 20, analysis.SampleRate)
	_, _, musicConfidence := analysis.AnalyzeKey(music, analysis.SampleRate)

	source := rand.New(rand.NewSource(7))
	glide := make([]float32, int(20*analysis.SampleRate))
	phase := 0.0
	for i := range glide {
		if i%int(analysis.SampleRate/4) == 0 {
			phase = 0
		}
		frequency := 110 + 60*math.Sin(float64(i)/analysis.SampleRate*3)
		phase += 2 * math.Pi * frequency / analysis.SampleRate
		glide[i] = float32(0.4*math.Sin(phase) + 0.3*source.NormFloat64())
	}

	_, _, glideConfidence := analysis.AnalyzeKey(glide, analysis.SampleRate)
	if glideConfidence >= musicConfidence {
		t.Errorf("glide confidence %.4f, want below tonal music %.4f", glideConfidence, musicConfidence)
	}
}

func TestAClearlyTonalSignalClearsTheContrastGate(t *testing.T) {
	tonal := make([]float32, int(20*analysis.SampleRate))
	phases := []float64{0, 0, 0}
	frequencies := []float64{261.63, 329.63, 392.00}
	for i := range tonal {
		var sample float64
		for f, frequency := range frequencies {
			phases[f] += 2 * math.Pi * frequency / analysis.SampleRate
			sample += math.Sin(phases[f])
		}
		tonal[i] = float32(sample / float64(len(frequencies)) * 0.5)
	}

	_, _, confidence := analysis.AnalyzeKey(tonal, analysis.SampleRate)
	if confidence <= 0 {
		t.Errorf("confidence = %.4f, want > 0 (floor %.4f)", confidence, analysis.KeyConfidenceFloor)
	}
}

func TestBroadbandNoiseIsRejectedByTheContrastGate(t *testing.T) {
	_, _, confidence := analysis.AnalyzeKey(noiseSamples(20, 7), analysis.SampleRate)

	if confidence != 0 {
		t.Errorf("confidence = %.4f, want 0", confidence)
	}
}

func tempoLagBounds() (minLag, maxLag int, frameRate float64) {
	frameRate = analysis.SampleRate / analysis.BeatHop
	minLag, maxLag = analysis.TempoLagRange(frameRate)
	return minLag, maxLag, frameRate
}

func TestFastestSearchableLagIsInsideTheAcceptedBand(t *testing.T) {
	minLag, _, frameRate := tempoLagBounds()

	period := float64(minLag) / frameRate
	if lowerBound := 60.0 / analysis.MaxBPM; period < lowerBound {
		t.Errorf("minLag=%d period=%.4fs, want >= %.5fs (%.0f BPM)", minLag, period, lowerBound, analysis.MaxBPM)
	}
}

func TestSlowestSearchableLagIsInsideTheAcceptedBand(t *testing.T) {
	_, maxLag, frameRate := tempoLagBounds()

	period := float64(maxLag) / frameRate
	if upperBound := 60.0 / analysis.MinBPM; period > upperBound {
		t.Errorf("maxLag=%d period=%.4fs, want <= %.5fs (%.0f BPM)", maxLag, period, upperBound, analysis.MinBPM)
	}
}

func TestTempoSearchWindowIsNotEmpty(t *testing.T) {
	minLag, maxLag, frameRate := tempoLagBounds()

	if minLag < 1 {
		t.Errorf("minLag = %d, want at least 1 (frameRate %.4f)", minLag, frameRate)
	}
	if minLag > maxLag {
		t.Errorf("minLag = %d exceeds maxLag = %d", minLag, maxLag)
	}
}

func TestATempoJustUnderTheCeilingIsRefinedNotClamped(t *testing.T) {
	minLag, _, frameRate := tempoLagBounds()
	clampBPM := (60 * frameRate) / float64(minLag)

	track, err := analysis.AnalyzeTrackSamples(synthesizeClickTrack(205, 30, analysis.SampleRate, 4, 0), analysis.SampleRate)
	if err != nil {
		t.Fatalf("205 BPM click track failed: %v", err)
	}

	if drift := math.Abs(track.BPM-205) / 205; drift >= 0.03 {
		t.Errorf("205 BPM detected as %.2f (%.1f%% off), clamp would be %.2f", track.BPM, drift*100, clampBPM)
	}
}

func TestANearCeilingEstimateIsNotTheRawLagQuantum(t *testing.T) {
	minLag, _, frameRate := tempoLagBounds()
	clampBPM := (60 * frameRate) / float64(minLag)

	track, err := analysis.AnalyzeTrackSamples(synthesizeClickTrack(205, 30, analysis.SampleRate, 4, 0), analysis.SampleRate)
	if err != nil {
		t.Fatalf("205 BPM click track failed: %v", err)
	}

	if math.Abs(track.BPM-clampBPM) <= 0.5 {
		t.Errorf("detected %.4f sits on the lag quantum %.4f", track.BPM, clampBPM)
	}
}

func TestATempoBeyondTheCeilingResolvesToAnOctaveNeverTheClamp(t *testing.T) {
	minLag, _, frameRate := tempoLagBounds()
	clampBPM := (60 * frameRate) / float64(minLag)

	for _, trueBPM := range []float64{215, 225, 250} {
		track, err := analysis.AnalyzeTrackSamples(synthesizeClickTrack(trueBPM, 30, analysis.SampleRate, 4, 0), analysis.SampleRate)
		if err != nil {
			continue
		}

		ratio := trueBPM / track.BPM
		if math.Abs(ratio-2) >= 0.05 {
			t.Errorf("%.0f BPM detected as %.2f (ratio %.3f), want the half-tempo octave", trueBPM, track.BPM, ratio)
		}
		if math.Abs(track.BPM-clampBPM) <= 0.5 {
			t.Errorf("%.0f BPM detected as %.2f, which is the clamp %.2f", trueBPM, track.BPM, clampBPM)
		}
	}
}

func TestTempoDeltaFoldsOctaveErrors(t *testing.T) {
	cases := []struct {
		name string
		bpmA float64
		bpmB float64
		want float64
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

	for _, testCase := range cases {
		if got := analysis.TempoDelta(testCase.bpmA, testCase.bpmB); math.Abs(got-testCase.want) > 0.0001 {
			t.Errorf("%s: got %.4f, want %.4f", testCase.name, got, testCase.want)
		}
	}
}

func TestTempoDeltaRejectsNonPositiveInput(t *testing.T) {
	cases := []struct {
		name string
		bpmA float64
		bpmB float64
	}{
		{"zero first", 0, 128},
		{"zero second", 128, 0},
		{"negative first", -5, 128},
	}

	for _, testCase := range cases {
		if got := analysis.TempoDelta(testCase.bpmA, testCase.bpmB); got != 0 {
			t.Errorf("%s: got %.4f, want 0", testCase.name, got)
		}
	}
}

func TestFoldingOnlyAppliesToGenuineOctaveRelationships(t *testing.T) {
	_, thirdsFactor := analysis.TempoDeltaFactor(128, 192)
	_, halfFactor := analysis.TempoDeltaFactor(187.5, 93.75)

	if thirdsFactor != 1.0 {
		t.Errorf("3:2 factor = %.1fx, want 1.0x (tolerance %.2f)", thirdsFactor, analysis.TempoFoldTolerance)
	}
	if halfFactor != 2.0 {
		t.Errorf("half-time factor = %.1fx, want 2.0x (tolerance %.2f)", halfFactor, analysis.TempoFoldTolerance)
	}
}

func TestSignedTempoDeltaKeepsDirectionAndFolds(t *testing.T) {
	if up := analysis.SignedTempoDelta(128, 160); up <= 0 {
		t.Errorf("128 to 160 = %.4f, want positive", up)
	}
	if down := analysis.SignedTempoDelta(160, 128); down >= 0 {
		t.Errorf("160 to 128 = %.4f, want negative", down)
	}
	if half := analysis.SignedTempoDelta(187.5, 93.75); math.Abs(half) >= 0.0001 {
		t.Errorf("half time = %.4f, want folded to 0", half)
	}
}

func TestBPMDetectionOnSyntheticClickTracks(t *testing.T) {
	for _, bpm := range []float64{70, 72, 90, 120, 140, 174, 185, 200, 205} {
		track, err := analysis.AnalyzeTrackSamples(synthesizeClickTrack(bpm, 30, analysis.SampleRate, 0, 0), analysis.SampleRate)
		if err != nil {
			t.Errorf("%.0f BPM click track failed: %v", bpm, err)
			continue
		}

		want := bpm
		if bpm > analysis.MaxBPM {
			want = bpm / 2
		}
		if drift := math.Abs(track.BPM-want) / want; drift > 0.03 {
			t.Errorf("%.0f BPM click track detected as %.1f, want %.1f within 3%%", bpm, track.BPM, want)
		}
	}
}

func TestShortTrackIsRejected(t *testing.T) {
	if _, err := analysis.AnalyzeTrackSamples(make([]float32, int(4*analysis.SampleRate)), analysis.SampleRate); err == nil {
		t.Error("a 4 second track was accepted")
	}
}

func TestSilentTrackIsRejected(t *testing.T) {
	if _, err := analysis.AnalyzeTrackSamples(make([]float32, int(30*analysis.SampleRate)), analysis.SampleRate); err == nil {
		t.Error("a silent track was accepted")
	}
}

func TestInvalidSampleRateIsRejected(t *testing.T) {
	if _, err := analysis.AnalyzeTrackSamples(make([]float32, 1000), 0); err == nil {
		t.Error("a zero sample rate was accepted")
	}
}

func analyzeAccentedClickTrack(t *testing.T) *analysis.TrackAnalysis {
	t.Helper()

	track, err := analysis.AnalyzeTrackSamples(synthesizeClickTrack(120, 40, analysis.SampleRate, 4, 0), analysis.SampleRate)
	if err != nil {
		t.Fatalf("accented click track failed: %v", err)
	}
	return track
}

func TestDownbeatPhaseIsInRange(t *testing.T) {
	track := analyzeAccentedClickTrack(t)

	if track.DownbeatPhase < 0 || track.DownbeatPhase >= 4 {
		t.Errorf("phase = %d, want within [0, 4) for a %d beat bar", track.DownbeatPhase, analysis.BarBeats)
	}
}

func TestTransitionSnappingLandsOnTheGrid(t *testing.T) {
	track := analyzeAccentedClickTrack(t)

	grid := snapTransitionToGrid(1000, 0, track)
	bar := snapTransitionToBar(1000, 0, track)
	periodFrames := track.PeriodSec * dsp.FramesPerSecond
	firstBeatFrame := track.FirstBeat * dsp.FramesPerSecond

	gridBeat := (float64(grid) - firstBeatFrame) / periodFrames
	if math.Abs(gridBeat-math.Round(gridBeat)) >= 0.02 {
		t.Errorf("beat snap %d lands on beat %.3f, want a whole beat", grid, gridBeat)
	}

	barBeat := (float64(bar) - firstBeatFrame) / periodFrames
	if math.Abs(barBeat-math.Round(barBeat)) >= 0.02 {
		t.Errorf("bar snap %d lands on beat %.3f, want a whole beat", bar, barBeat)
	}

	barPhase := math.Mod(math.Round(barBeat)-float64(track.DownbeatPhase), analysis.BarBeats)
	if barPhase < 0 {
		barPhase += analysis.BarBeats
	}
	if barPhase != 0 {
		t.Errorf("bar snap %d sits at bar phase %.0f, want 0", bar, barPhase)
	}
}

func TestBarSnapIsWithinOneBarOfBeatSnap(t *testing.T) {
	track := analyzeAccentedClickTrack(t)

	grid := snapTransitionToGrid(1000, 0, track)
	bar := snapTransitionToBar(1000, 0, track)
	barFrames := track.PeriodSec * dsp.FramesPerSecond * analysis.BarBeats

	if distance := math.Abs(float64(bar - grid)); distance > barFrames {
		t.Errorf("bar snap %d is %.1f frames from beat snap %d, want at most one bar (%.1f frames)", bar, distance, grid, barFrames)
	}
}

func TestAnalysisCarriesKeyData(t *testing.T) {
	track := analyzeAccentedClickTrack(t)

	if track.BPM <= 0 {
		t.Errorf("BPM = %.1f, want > 0", track.BPM)
	}
	if code := analysis.CamelotCode(track.Tonic, track.Minor); code == "" {
		t.Error("analysis carries no Camelot code")
	}
}

func measureProcessorFrameCost(recipe transition.Recipe, bothSides bool) time.Duration {
	processor := transition.NewProcessor(recipe, 500, 0.5)
	aTone := &toneGenerator{frequency: 220, amplitude: 8000}
	bTone := &toneGenerator{frequency: 660, amplitude: 8000}
	aFrame := make([]int16, frameSize*channels)
	bFrame := make([]int16, frameSize*channels)

	frames := 500
	start := time.Now()
	for i := 0; i < frames; i++ {
		aTone.fill(aFrame)
		progress := float64(i) / float64(frames)

		if !bothSides {
			processor.ProcessA(aFrame, progress)
			continue
		}

		bTone.fill(bFrame)
		aBuf := processor.ProcessA(aFrame, progress)
		bBuf := processor.ProcessB(bFrame, progress)
		processor.ApplyGains(aBuf, bBuf, progress, 1.0)
	}

	return time.Since(start) / time.Duration(frames)
}

func TestHeaviestRecipeFitsTheRealtimeBudget(t *testing.T) {
	perFrame := measureProcessorFrameCost(transition.Recipe{
		Volume: transition.VolumeCutInFadeOut,
		EQ:     transition.EQThreeBandFade,
		Filter: transition.FilterLowPassInHighPassOut,
		Effect: transition.EffectEchoHalfCutEnd,
		Loop:   transition.LoopFourBeats,
	}, true)

	if perFrame >= 4*time.Millisecond {
		t.Errorf("%.3fms per 20ms frame (%.1f%% of realtime), want under 4ms",
			float64(perFrame.Microseconds())/1000, float64(perFrame.Microseconds())/200)
	}
}

func TestReverbFitsTheRealtimeBudget(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Effect = transition.EffectReverbOutCenter

	if perFrame := measureProcessorFrameCost(recipe, false); perFrame >= 4*time.Millisecond {
		t.Errorf("%.3fms per 20ms frame, want under 4ms", float64(perFrame.Microseconds())/1000)
	}
}
