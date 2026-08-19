package player

import "testing"

func TestAdjustEndStateForOffsetLeavesUnshiftedStreams(t *testing.T) {
	original := &streamEndState{totalFrames: 100, tailStartFrame: 80, silentTailFrames: 5}

	for _, offset := range []int{0, -1} {
		if got := adjustEndStateForOffset(original, offset); got != original {
			t.Errorf("offset %d returned a copy, want the original untouched", offset)
		}
	}
}

func TestAdjustEndStateForOffsetShiftsFrameCounts(t *testing.T) {
	original := &streamEndState{totalFrames: 100, tailStartFrame: 80, silentTailFrames: 5}

	got := adjustEndStateForOffset(original, 30)

	if got == original {
		t.Fatal("the original end state was mutated in place")
	}
	if got.totalFrames != 70 {
		t.Errorf("totalFrames = %d, want 70", got.totalFrames)
	}
	if got.tailStartFrame != 50 {
		t.Errorf("tailStartFrame = %d, want 50", got.tailStartFrame)
	}
	if got.silentTailFrames != 5 {
		t.Errorf("silentTailFrames = %d, want it carried through unchanged", got.silentTailFrames)
	}
	if original.totalFrames != 100 || original.tailStartFrame != 80 {
		t.Error("the original end state was modified")
	}
}
