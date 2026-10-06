package player_test

import (
	"testing"

	"noraegaori/internal/audio/transition"
	"noraegaori/internal/player"
)

func countingPCM(start int) []int16 {
	frame := make([]int16, player.HookFrameSize*player.HookChannels)
	for pair := 0; pair < player.HookFrameSize; pair++ {
		value := int16((start + pair) % 30000)
		frame[pair*player.HookChannels] = value
		frame[pair*player.HookChannels+1] = value
	}
	return frame
}

func incomingFrames(t *testing.T, loop *transition.BeatLoop, frames int) [][]int16 {
	t.Helper()
	stream := newFakeStream(frames)
	for index := 0; index < frames; index++ {
		stream.pcm <- countingPCM(index * player.HookFrameSize)
	}
	cs := player.HookNewCrossfadeState()
	*cs.HookBStream() = stream
	*cs.HookIncomingLoop() = loop

	outputs := make([][]int16, frames)
	for index := range outputs {
		outputs[index] = append([]int16(nil), cs.HookNextBFrame()...)
	}
	return outputs
}

func TestIncomingSlipRollLoopsTheFirstQuarterBeatThenReleases(t *testing.T) {
	outputs := incomingFrames(t, transition.PrepareIncomingLoop(transition.FXSlipRoll, 0.1, 40), 40)

	quarterBeat := 1200
	if got, want := int(outputs[5][500*player.HookChannels]), (5*player.HookFrameSize+500)%quarterBeat; got != want {
		t.Errorf("rolled sample = %d, want %d from the first quarter beat of the incoming song", got, want)
	}
	if got, want := int(outputs[20][3*player.HookChannels]), 20*player.HookFrameSize+3; got != want {
		t.Errorf("released sample = %d, want the live %d at the incoming song's real position", got, want)
	}
}

func TestIncomingFramesPassThroughWithoutARoll(t *testing.T) {
	outputs := incomingFrames(t, nil, 3)
	for index, frame := range outputs {
		if got := int(frame[7*player.HookChannels]); got != index*player.HookFrameSize+7 {
			t.Errorf("frame %d sample 7 = %d, want the untouched incoming audio", index, got)
		}
	}
}
