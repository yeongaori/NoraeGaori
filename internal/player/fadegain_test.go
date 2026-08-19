package player

import (
	"math"
	"testing"
)

func TestFadeInGainIsInactiveWithoutAWindow(t *testing.T) {
	for _, frames := range []int{0, -1} {
		gain, active := fadeInGainAt(10, 0, frames)
		if active {
			t.Errorf("frames=%d reported an active fade-in", frames)
		}
		if gain != 1.0 {
			t.Errorf("frames=%d returned gain %g, want unity", frames, gain)
		}
	}
}

func TestFadeInGainRunsFromSilenceToUnity(t *testing.T) {
	const start, frames = 100, 50

	if gain, active := fadeInGainAt(start, start, frames); !active || gain != 0 {
		t.Errorf("at the first frame got (%g, %v), want (0, true)", gain, active)
	}

	mid, active := fadeInGainAt(start+frames/2, start, frames)
	if !active {
		t.Fatal("the midpoint reported an inactive fade-in")
	}
	if mid <= 0 || mid >= 1 {
		t.Errorf("midpoint gain %g, want strictly between 0 and 1", mid)
	}

	if _, active := fadeInGainAt(start+frames, start, frames); active {
		t.Error("the frame past the window still reported an active fade-in")
	}
}

func TestFadeInGainIsMonotonic(t *testing.T) {
	const start, frames = 0, 64

	previous := -1.0
	for frame := start; frame < start+frames; frame++ {
		gain, active := fadeInGainAt(frame, start, frames)
		if !active {
			t.Fatalf("frame %d reported an inactive fade-in inside the window", frame)
		}
		if gain < previous {
			t.Errorf("frame %d gain %g dropped below the previous %g", frame, gain, previous)
		}
		if gain < 0 || gain > 1 {
			t.Errorf("frame %d gain %g is outside [0,1]", frame, gain)
		}
		previous = gain
	}
}

func TestFadeOutGainIsInactiveBeforeItsWindow(t *testing.T) {
	gain, active := fadeOutGainAt(10, 100, 50)
	if active {
		t.Error("a frame before the window reported an active fade-out")
	}
	if gain != 1.0 {
		t.Errorf("got gain %g before the window, want unity", gain)
	}

	if _, active := fadeOutGainAt(200, 100, 0); active {
		t.Error("a zero-length window reported an active fade-out")
	}
}

func TestFadeOutGainRunsFromUnityToSilence(t *testing.T) {
	const start, frames = 100, 50

	if gain, active := fadeOutGainAt(start, start, frames); !active || gain != 1 {
		t.Errorf("at the first frame got (%g, %v), want (1, true)", gain, active)
	}

	end, active := fadeOutGainAt(start+frames, start, frames)
	if !active {
		t.Fatal("the final frame reported an inactive fade-out")
	}
	if end != 0 {
		t.Errorf("final gain %g, want 0", end)
	}
}

func TestFadeOutGainIsMonotonic(t *testing.T) {
	const start, frames = 0, 64

	previous := math.Inf(1)
	for frame := start; frame <= start+frames; frame++ {
		gain, active := fadeOutGainAt(frame, start, frames)
		if !active {
			t.Fatalf("frame %d reported an inactive fade-out inside the window", frame)
		}
		if gain > previous {
			t.Errorf("frame %d gain %g rose above the previous %g", frame, gain, previous)
		}
		if gain < 0 || gain > 1 {
			t.Errorf("frame %d gain %g is outside [0,1]", frame, gain)
		}
		previous = gain
	}
}

func TestFadeGainsClampOutsideTheirWindows(t *testing.T) {
	if gain, active := fadeInGainAt(-10, 0, 50); !active || gain != 0 {
		t.Errorf("a frame before the fade-in start gave (%g, %v), want (0, true)", gain, active)
	}

	if gain, active := fadeOutGainAt(1000, 100, 50); !active || gain != 0 {
		t.Errorf("a frame far past the fade-out end gave (%g, %v), want (0, true)", gain, active)
	}
}

func TestFadeInAndFadeOutAreComplementary(t *testing.T) {
	const frames = 32

	for frame := 0; frame < frames; frame++ {
		in, _ := fadeInGainAt(frame, 0, frames)
		out, _ := fadeOutGainAt(frame, 0, frames)

		if math.Abs((in*in)+(out*out)-1) > 1e-9 {
			t.Errorf("frame %d: fade-in %g and fade-out %g are not equal-power complements", frame, in, out)
		}
	}
}
