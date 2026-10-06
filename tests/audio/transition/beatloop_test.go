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

func TestBeatLoopKeepsLeftAndRightApart(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	var frame []int16
	for i := 0; i < 3; i++ {
		live := make([]int16, dsp.FrameSize*dsp.Channels)
		for pair := 0; pair < dsp.FrameSize; pair++ {
			position := i*dsp.FrameSize + pair
			live[pair*dsp.Channels] = int16(position)
			live[pair*dsp.Channels+1] = int16(-position)
		}
		frame = loop.Next(live)
	}

	pair := 700
	if left, right := frame[pair*dsp.Channels], frame[pair*dsp.Channels+1]; left != 620 || right != -620 {
		t.Errorf("sample 2620 = %d/%d, want 620/-620 (loop position (2620-1000) mod 1000, each channel repeating its own audio)", left, right)
	}
}

func TestBeatLoopSeamClampsLoudAudio(t *testing.T) {
	loop := transition.CaptureBeatLoop(1000)
	loud := func(int) []int16 {
		frame := make([]int16, dsp.FrameSize*dsp.Channels)
		for i := range frame {
			frame[i] = 30000
		}
		return frame
	}
	output := runBeatLoop(loop, 2, loud)

	for position, sample := range output {
		if sample < 0 {
			t.Fatalf("sample %d = %d, want a clamped positive value instead of a wrapped one", position, sample)
		}
	}
	if middle := output[1120]; middle != 32767 {
		t.Errorf("mid-seam sample = %d, want 32767 (two 30000 signals blended at equal power, clamped)", middle)
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

func TestRollPlaysLiveUntilTheMiddle(t *testing.T) {
	output := runBeatLoop(transition.PrepareBeatLoop(transition.LoopRoll, 0.1, 40), 20, countingFrame)

	for index, value := range output {
		if int(value) != index {
			t.Fatalf("sample %d = %d, want the live audio before the roll starts", index, value)
		}
	}
}

func TestRollHalvesTheLoopFromTheSameStart(t *testing.T) {
	output := runBeatLoop(transition.PrepareBeatLoop(transition.LoopRoll, 0.1, 40), 40, countingFrame)
	start := 20 * dsp.FrameSize

	cases := []struct {
		frame, pair, want int
	}{
		{22, 100, start + 2*dsp.FrameSize + 100},
		{26, 300, start + (6*dsp.FrameSize+300)%4800},
		{31, 300, start + (11*dsp.FrameSize+300)%2400},
		{36, 500, start + (16*dsp.FrameSize+500)%1200},
		{39, 900, start + (19*dsp.FrameSize+900)%1200},
	}
	for _, c := range cases {
		if got := int(output[c.frame*dsp.FrameSize+c.pair]); got != c.want {
			t.Errorf("frame %d pair %d = %d, want %d", c.frame, c.pair, got, c.want)
		}
	}
}

func counted(sample int) int {
	return sample % 30000
}

type loopProbe struct {
	frame, pair, want int
}

func checkLoopProbes(t *testing.T, output []int16, probes []loopProbe) {
	t.Helper()
	for _, probe := range probes {
		if got := int(output[probe.frame*dsp.FrameSize+probe.pair]); got != counted(probe.want) {
			t.Errorf("frame %d pair %d = %d, want %d", probe.frame, probe.pair, got, counted(probe.want))
		}
	}
}

func TestSlipRollReturnsToTheLivePosition(t *testing.T) {
	output := runBeatLoop(transition.PrepareBeatLoop(transition.LoopSlipRoll, 0.1, 80), 80, countingFrame)
	start := 40 * dsp.FrameSize

	checkLoopProbes(t, output, []loopProbe{
		{45, 300, start + (5*dsp.FrameSize+300)%4800},
		{52, 300, start + (12*dsp.FrameSize+300)%2400},
		{57, 900, start + (17*dsp.FrameSize+900)%1200},
		{60, 100, 60*dsp.FrameSize + 100},
		{79, 700, 79*dsp.FrameSize + 700},
	})
}

func TestRollAtEndStartsInTheLastQuarter(t *testing.T) {
	output := runBeatLoop(transition.PrepareBeatLoop(transition.LoopRollAtEnd, 0.1, 80), 80, countingFrame)
	for index := 0; index < 60*dsp.FrameSize; index++ {
		if int(output[index]) != counted(index) {
			t.Fatalf("sample %d = %d, want live audio before three quarters", index, output[index])
		}
	}
	checkLoopProbes(t, output, []loopProbe{{75, 500, 60*dsp.FrameSize + (15*dsp.FrameSize+500)%1200}})
}

func TestIncomingRollGrowsFromTheFirstBeatAndReleases(t *testing.T) {
	output := runBeatLoop(transition.PrepareIncomingLoop(transition.FXRoll, 0.1, 40), 40, countingFrame)

	checkLoopProbes(t, output, []loopProbe{
		{2, 100, (2*dsp.FrameSize + 100) % 1200},
		{7, 100, (7*dsp.FrameSize + 100) % 2400},
		{15, 600, (15*dsp.FrameSize + 600) % 4800},
		{20, 100, 20*dsp.FrameSize + 100},
		{39, 900, 39*dsp.FrameSize + 900},
	})
}

func TestIncomingSlipRollReleasesAtAQuarter(t *testing.T) {
	output := runBeatLoop(transition.PrepareIncomingLoop(transition.FXSlipRoll, 0.1, 40), 40, countingFrame)
	checkLoopProbes(t, output, []loopProbe{
		{5, 500, (5*dsp.FrameSize + 500) % 1200},
		{10, 3, 10*dsp.FrameSize + 3},
	})
}

func TestOnlyRollsBuildAnIncomingLoop(t *testing.T) {
	for _, fx := range []transition.FXStyle{transition.FXNone, transition.FXPhaser, transition.FXDelayOneBar} {
		if loop := transition.PrepareIncomingLoop(fx, 0.5, 400); loop != nil {
			t.Errorf("incoming effect %d built a loop", fx)
		}
	}
	if loop := transition.PrepareIncomingLoop(transition.FXRoll, 0.5, 10); loop != nil {
		t.Error("an incoming roll that does not fit still built a loop")
	}
}

func TestPrepareBeatLoopBuildsTheChosenLength(t *testing.T) {
	if loop := transition.PrepareBeatLoop(transition.LoopNone, 0.5, 400); loop != nil {
		t.Error("no loop style still built a loop")
	}
	loop := transition.PrepareBeatLoop(transition.LoopTwoBeats, 0.1, 400)
	output := runBeatLoop(loop, 12, countingFrame)
	if got := output[9600+300]; int(got) != 300 {
		t.Errorf("sample 9900 = %d, want 300 from a 9600-sample two-beat loop", got)
	}
}
