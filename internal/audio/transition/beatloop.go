package transition

import (
	"math"

	"noraegaori/internal/audio/dsp"
)

const loopSeamSamples = 240

type rollStage struct {
	at      float64
	divisor int
}

type rollSchedule struct {
	beats   int
	stages  []rollStage
	release float64
}

var wholeOverlap = []rollStage{{0, 1}}

var loopSchedules = map[LoopStyle]rollSchedule{
	LoopOneBeat:       {beats: 1, stages: wholeOverlap},
	LoopTwoBeats:      {beats: 2, stages: wholeOverlap},
	LoopFourBeats:     {beats: 4, stages: wholeOverlap},
	LoopEightBeats:    {beats: 8, stages: wholeOverlap},
	LoopSixteenBeats:  {beats: 16, stages: wholeOverlap},
	LoopRoll:          {beats: 1, stages: []rollStage{{0.5, 1}, {0.75, 2}, {0.875, 4}}},
	LoopRollAtEnd:     {beats: 1, stages: []rollStage{{0.75, 1}, {0.875, 2}, {0.9375, 4}}},
	LoopSlipRoll:      {beats: 1, stages: []rollStage{{0.5, 1}, {0.625, 2}, {0.6875, 4}}, release: 0.75},
	LoopSlipRollAtEnd: {beats: 1, stages: []rollStage{{0.75, 1}, {0.8125, 2}, {0.84375, 4}}, release: 0.875},
}

var incomingSchedules = map[FXStyle]rollSchedule{
	FXRoll:     {beats: 1, stages: []rollStage{{0, 4}, {0.125, 2}, {0.25, 1}}, release: 0.5},
	FXSlipRoll: {beats: 1, stages: []rollStage{{0, 4}}, release: 0.25},
}

func (s rollSchedule) fits(periodSec float64, frames int) bool {
	if periodSec <= 0 {
		return false
	}
	spanSec := float64(frames) / dsp.FramesPerSecond
	for index, stage := range s.stages {
		end := 1.0
		if index+1 < len(s.stages) {
			end = s.stages[index+1].at
		} else if s.release > 0 {
			end = s.release
		}
		if float64(s.beats)/float64(stage.divisor)*periodSec > (end-stage.at)*spanSec {
			return false
		}
	}
	return true
}

func (s rollSchedule) capture(periodSec float64, frames int) *BeatLoop {
	if !s.fits(periodSec, frames) {
		return nil
	}
	beatSamples := int(math.Round(float64(s.beats) * periodSec * dsp.SampleRate))
	stages := make([]loopStage, 0, len(s.stages))
	for _, stage := range s.stages {
		stages = append(stages, loopStage{
			frame:  int(stage.at * float64(frames)),
			length: max(1, beatSamples/stage.divisor),
		})
	}
	loop := captureStages(stages)
	if s.release > 0 {
		loop.release = int(s.release * float64(frames))
	}
	return loop
}

func LoopBeatCount(style LoopStyle) int {
	return loopSchedules[style].beats
}

func PrepareBeatLoop(loop LoopStyle, periodSec float64, frames int) *BeatLoop {
	schedule, ok := loopSchedules[loop]
	if !ok {
		return nil
	}
	return schedule.capture(periodSec, frames)
}

func PrepareIncomingLoop(fx FXStyle, periodSec float64, frames int) *BeatLoop {
	schedule, ok := incomingSchedules[fx]
	if !ok {
		return nil
	}
	return schedule.capture(periodSec, frames)
}

type loopStage struct {
	frame  int
	length int
}

type BeatLoop struct {
	samples  []int16
	stages   []loopStage
	longest  int
	release  int
	frames   int
	captured int
	cursor   int
	out      []int16
}

func CaptureBeatLoop(lengthSamples int) *BeatLoop {
	return captureStages([]loopStage{{frame: 0, length: lengthSamples}})
}

func captureStages(stages []loopStage) *BeatLoop {
	longest := 0
	for _, stage := range stages {
		longest = max(longest, stage.length)
	}
	return &BeatLoop{
		samples: make([]int16, (longest+seamLength(longest))*dsp.Channels),
		stages:  stages,
		longest: longest,
		out:     make([]int16, dsp.FrameSize*dsp.Channels),
	}
}

func seamLength(length int) int {
	return min(loopSeamSamples, length/2)
}

func (l *BeatLoop) IsReady() bool {
	return l.captured >= l.longest+seamLength(l.longest)
}

func (l *BeatLoop) Next(live []int16) []int16 {
	frame := l.frames
	l.frames++
	if frame < l.stages[0].frame || (l.release > 0 && frame >= l.release) {
		return live
	}
	if live == nil && !l.IsReady() {
		return nil
	}

	length := l.lengthAt(frame)
	for pair := 0; pair < dsp.FrameSize; pair++ {
		l.capture(live, pair)
		l.emit(pair, length)
		l.cursor++
	}
	return l.out
}

func (l *BeatLoop) lengthAt(frame int) int {
	length := l.stages[0].length
	for _, stage := range l.stages {
		if frame >= stage.frame {
			length = stage.length
		}
	}
	return length
}

func (l *BeatLoop) capture(live []int16, pair int) {
	if l.IsReady() || live == nil {
		return
	}
	for channel := 0; channel < dsp.Channels; channel++ {
		var sample int16
		if index := pair*dsp.Channels + channel; index < len(live) {
			sample = live[index]
		}
		l.samples[l.captured*dsp.Channels+channel] = sample
	}
	l.captured++
}

func (l *BeatLoop) emit(pair, length int) {
	position := l.cursor
	if position >= length {
		position = l.cursor % length
	}
	seam := seamLength(length)
	repeating := l.cursor >= length

	for channel := 0; channel < dsp.Channels; channel++ {
		sample := float64(l.samples[position*dsp.Channels+channel])
		if repeating && position < seam {
			blend := (float64(position) + 0.5) / float64(seam)
			continued := float64(l.samples[(length+position)*dsp.Channels+channel])
			sample = sample*dsp.QSinIn(blend) + continued*dsp.QSinOut(blend)
		}
		l.out[pair*dsp.Channels+channel] = dsp.ClampToInt16(sample)
	}
}
