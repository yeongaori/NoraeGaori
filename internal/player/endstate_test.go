package player

import "noraegaori/internal/audio/ffmpeg"

import "testing"

func TestAdjustEndStateForOffsetLeavesUnshiftedStreams(t *testing.T) {
	original := &ffmpeg.EndState{TotalFrames: 100, TailStartFrame: 80, SilentTailFrames: 5}

	for _, offset := range []int{0, -1} {
		if got := adjustEndStateForOffset(original, offset); got != original {
			t.Errorf("offset %d returned a copy, want the original untouched", offset)
		}
	}
}

func TestAdjustEndStateForOffsetShiftsFrameCounts(t *testing.T) {
	original := &ffmpeg.EndState{TotalFrames: 100, TailStartFrame: 80, SilentTailFrames: 5}

	got := adjustEndStateForOffset(original, 30)

	if got == original {
		t.Fatal("the original end state was mutated in place")
	}
	if got.TotalFrames != 70 {
		t.Errorf("totalFrames = %d, want 70", got.TotalFrames)
	}
	if got.TailStartFrame != 50 {
		t.Errorf("tailStartFrame = %d, want 50", got.TailStartFrame)
	}
	if got.SilentTailFrames != 5 {
		t.Errorf("silentTailFrames = %d, want it carried through unchanged", got.SilentTailFrames)
	}
	if original.TotalFrames != 100 || original.TailStartFrame != 80 {
		t.Error("the original end state was modified")
	}
}
