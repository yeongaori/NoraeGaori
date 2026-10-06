package transition_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

func playedThrough(fx string, amplitude float64) *transition.Processor {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXOut, fx)), eightBarWindow(100))
	tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: amplitude}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for i := 0; i < 100; i++ {
		tone.Fill(frame)
		processor.Mix(frame, silent, float64(i)/100, 1)
	}
	return processor
}

func framePeak(frame []int16) float64 {
	peak := 0.0
	for _, sample := range frame {
		peak = math.Max(peak, math.Abs(float64(sample)))
	}
	return peak
}

func TestOnlyRingingEffectsLeaveATail(t *testing.T) {
	ringing := map[string]bool{
		"reverb_out_center":          true,
		"reverb_out_end":             true,
		"echo_half_out_end":          true,
		"echo_three_quarter_out_end": true,
		"echo_beat_out_end":          true,
		"delay_one_bar":              true,
		"delay_one_bar_at_end":       true,
	}
	for _, effect := range legacyNames(transition.CategoryFXOut) {
		tail := playedThrough(effect, 9000).MakeTail()
		if (tail != nil) != ringing[effect] {
			t.Errorf("%s left a tail: %t, want %t", effect, tail != nil, ringing[effect])
		}
	}
}

func TestIncomingEffectsNeverLeaveATail(t *testing.T) {
	for _, effect := range legacyNames(transition.CategoryFXIn) {
		processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXIn, effect)), eightBarWindow(100))
		if processor.MakeTail() != nil {
			t.Errorf("incoming %s left a tail, want the incoming song to continue dry", effect)
		}
	}
}

func TestRingingTailsDecayAndFinish(t *testing.T) {
	for _, effect := range []string{"reverb_out_end", "echo_half_out_end", "delay_one_bar"} {
		tail := playedThrough(effect, 9000).MakeTail()
		frame := make([]int16, dsp.FrameSize*dsp.Channels)
		tail.Apply(frame)
		first := framePeak(frame)

		frames := 1
		var last float64
		for {
			for i := range frame {
				frame[i] = 0
			}
			more := tail.Apply(frame)
			last = framePeak(frame)
			frames++
			if !more {
				break
			}
			if frames > 700 {
				t.Fatalf("%s tail never finished", effect)
			}
		}
		if first < 300 {
			t.Errorf("%s tail starts at %.0f, want the effect ringing into the next song", effect, first)
		}
		if last > 2 {
			t.Errorf("%s tail ends at %.0f, want silence", effect, last)
		}
	}
}

func TestTheSendTakesTheFadedOutgoingSong(t *testing.T) {
	ringFirstFrame := func(volume string) float64 {
		processor := transition.NewProcessor(combinedRecipe(volumeStyle(volume), effectStyle("echo_half_out_end")), eightBarWindow(200))
		tone := &audiotest.ToneGenerator{Frequency: 1000, Amplitude: 9000}
		silent := make([]int16, dsp.FrameSize*dsp.Channels)
		frame := make([]int16, dsp.FrameSize*dsp.Channels)
		for i := 0; i < 200; i++ {
			tone.Fill(frame)
			processor.Mix(frame, silent, float64(i)/200, 1)
		}
		tail := make([]int16, dsp.FrameSize*dsp.Channels)
		processor.MakeTail().Apply(tail)
		return framePeak(tail)
	}

	held := ringFirstFrame("overlap")
	faded := ringFirstFrame("crossfade")
	if faded > held*0.15 {
		t.Errorf("echo after a faded-out song peaks at %.0f against %.0f after a held one, want the fader in front of the send", faded, held)
	}
}

func TestReverbRingsLongerThanAHalfBeatEcho(t *testing.T) {
	length := func(effect string) int {
		tail := playedThrough(effect, 9000).MakeTail()
		frame := make([]int16, dsp.FrameSize*dsp.Channels)
		frames := 1
		for tail.Apply(frame) {
			for i := range frame {
				frame[i] = 0
			}
			frames++
		}
		return frames
	}

	reverb, echo := length("reverb_out_end"), length("echo_half_out_end")
	if reverb <= echo {
		t.Errorf("reverb tail lasted %d frames and the echo %d, want the 8.5s reverb to outlast the echo", reverb, echo)
	}
}

func TestTailIsLimitedOverALoudNextSong(t *testing.T) {
	tail := playedThrough("echo_half_out_end", 30000).MakeTail()
	song := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 32000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	run, longest := 0, 0
	for frames := 0; frames < 50; frames++ {
		song.Fill(frame)
		tail.Apply(frame)
		for i := 0; i < len(frame); i += dsp.Channels {
			if frame[i] == 32767 || frame[i] <= -32767 {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
	}
	if longest > 2 {
		t.Errorf("the next song plus the echo held the rail for %d samples, want a limited peak", longest)
	}
}

func TestTailHandsTheNextSongBackWithoutALevelStep(t *testing.T) {
	tail := playedThrough("echo_half_out_end", 30000).MakeTail()
	song := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 32000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	input := make([]int16, len(frame))

	lastDeviation := 0.0
	for frames := 0; ; frames++ {
		if frames > 1000 {
			t.Fatal("the tail never let go of the next song")
		}
		song.Fill(frame)
		copy(input, frame)
		more := tail.Apply(frame)
		lastDeviation = 0
		for i := range frame {
			lastDeviation = math.Max(lastDeviation, math.Abs(float64(frame[i])-float64(input[i])))
		}
		if !more {
			break
		}
	}
	if lastDeviation > 32 {
		t.Errorf("the last frame the tail touched differs from the song by %.0f, want at most 32 (0.1%%) so there is no step when it ends", lastDeviation)
	}
}

func TestFinishedTailLeavesFramesAlone(t *testing.T) {
	tail := playedThrough("echo_half_out_end", 5000).MakeTail()
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for tail.Apply(frame) {
		for i := range frame {
			frame[i] = 0
		}
	}

	song := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 20000}
	song.Fill(frame)
	want := append([]int16(nil), frame...)
	if tail.Apply(frame) {
		t.Error("a finished tail reported more audio to come")
	}
	for i := range frame {
		if frame[i] != want[i] {
			t.Fatalf("sample %d = %d after the tail finished, want the untouched %d", i, frame[i], want[i])
		}
	}
}

func TestNilTailApplyIsSafe(t *testing.T) {
	var tail *transition.Tail

	if tail.Apply(make([]int16, dsp.FrameSize*dsp.Channels)) {
		t.Error("a nil tail reported more audio to come")
	}
}
