package transition_test

import (
	"math"
	"testing"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

func countingFrame(start int) []int16 {
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for pair := 0; pair < dsp.FrameSize; pair++ {
		frame[pair*dsp.Channels] = int16((start + pair) % 30000)
		frame[pair*dsp.Channels+1] = int16((start + pair) % 30000)
	}
	return frame
}

func sineFrame(start int, frequency, amplitude float64) []int16 {
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for pair := 0; pair < dsp.FrameSize; pair++ {
		value := int16(amplitude * math.Sin(2*math.Pi*frequency*float64(start+pair)/dsp.SampleRate))
		frame[pair*dsp.Channels] = value
		frame[pair*dsp.Channels+1] = value
	}
	return frame
}

func runBeatLoop(loop *transition.BeatLoop, frames int, source func(int) []int16) []int16 {
	output := []int16{}
	for i := 0; i < frames; i++ {
		frame := loop.Next(source(i * dsp.FrameSize))
		for pair := 0; pair < dsp.FrameSize; pair++ {
			output = append(output, frame[pair*dsp.Channels])
		}
	}
	return output
}

func TestBeatLoopPassesLiveAudioUntilTheLoopLength(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	output := runBeatLoop(loop, 2, countingFrame)

	for position := 0; position < 1000; position++ {
		if output[position] != int16(position) {
			t.Fatalf("sample %d = %d, want the live %d", position, output[position], position)
		}
	}
}

func TestBeatLoopRepeatsOnTheExactSampleLength(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	output := runBeatLoop(loop, 5, countingFrame)

	for position := 1240; position < 2000; position++ {
		if want := int16(position - 1000); output[position] != want {
			t.Fatalf("sample %d = %d, want %d from one loop length earlier", position, output[position], want)
		}
	}
	for position := 3240; position < 4000; position++ {
		if want := int16(position - 3000); output[position] != want {
			t.Fatalf("sample %d = %d, want %d three loop lengths on", position, output[position], want)
		}
	}
}

func TestBeatLoopSeamIsCrossfaded(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	output := runBeatLoop(loop, 6, func(start int) []int16 { return sineFrame(start, 1030, 12000) })

	sineStep := 12000 * 2 * math.Pi * 1030 / dsp.SampleRate
	worst := 0.0
	for position := 1; position < len(output); position++ {
		worst = math.Max(worst, math.Abs(float64(output[position])-float64(output[position-1])))
	}
	if worst > sineStep*1.5 {
		t.Errorf("largest step %.0f, want at most %.0f (1.5x the sine's own step) across loop seams", worst, sineStep*1.5)
	}
}

func TestBeatLoopSeamBlendsTheContinuationIntoTheLoopStart(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	output := runBeatLoop(loop, 2, countingFrame)

	if got := output[1120]; got != 874 {
		t.Errorf("mid-seam sample = %d, want 874 (120*sin + 1120*cos at blend 120.5/240)", got)
	}
}

func TestBeatLoopReturnsNilWithoutLiveAudioBeforeItIsReady(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)

	if loop.IsReady() {
		t.Fatal("a fresh loop reported ready")
	}
	if out := loop.Next(nil); out != nil {
		t.Errorf("got %d samples, want nil before the loop is captured", len(out))
	}
}

func TestBeatLoopBecomesReadyOnceTheSeamIsCaptured(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	runBeatLoop(loop, 1, countingFrame)
	if loop.IsReady() {
		t.Fatal("ready after 960 samples, want the loop plus its 240-sample seam first")
	}

	runBeatLoop(loop, 1, countingFrame)
	if !loop.IsReady() {
		t.Fatal("not ready after 1920 samples, want ready past 1240")
	}
	if out := loop.Next(nil); out == nil {
		t.Error("a ready loop returned nil for a drained source")
	}
}

func TestBeatLoopCopiesTheSourceFrame(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	source := countingFrame(0)
	loop.Next(source)
	for i := range source {
		source[i] = 999
	}

	output := runBeatLoop(loop, 2, func(start int) []int16 { return countingFrame(start + dsp.FrameSize) })
	if output[len(output)-1] == 999 {
		t.Error("changing the source frame leaked into the loop")
	}
	if last := output[len(output)-1]; last != 879 {
		t.Errorf("sample 2879 = %d, want 879 (loop position (2879-1000) mod 1000 from the first frame)", last)
	}
}
