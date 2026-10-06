package transition

import (
	"math"

	"noraegaori/internal/audio/dsp"
)

const (
	tailQuietFrames = 75
	tailMaxFrames   = 600
	tailSilence     = 2.0
)

type Window struct {
	Frames    int
	PeriodSec float64
	Bars      int
}

type deckState struct {
	curves   deckCurves
	effect   *deckEffect
	isolator dsp.Isolator
	filter   dsp.Biquad
	buffer   []float64
	gain     float64
	primed   bool
}

type Processor struct {
	frameStep float64
	outgoing  deckState
	incoming  deckState
	jog       *Jogwheel
	mixBuffer []float64
	scratch   []float64
}

func prepareDeck(side *Side, isOutgoing bool, window *Window) deckState {
	return deckState{
		curves: buildDeckCurves(side, isOutgoing, window.Bars),
		effect: prepareEffect(side.FX, isOutgoing, window),
		buffer: make([]float64, dsp.FrameSize*dsp.Channels),
	}
}

func NewProcessor(recipe *Recipe, window *Window) *Processor {
	processor := &Processor{
		outgoing:  prepareDeck(&recipe.Out, true, window),
		incoming:  prepareDeck(&recipe.In, false, window),
		mixBuffer: make([]float64, dsp.FrameSize*dsp.Channels),
	}
	if processor.outgoing.effect != nil || processor.incoming.effect != nil {
		processor.scratch = make([]float64, dsp.FrameSize*dsp.Channels)
	}
	if window.Frames > 0 {
		processor.frameStep = 1 / float64(window.Frames)
		processor.jog = prepareJogwheel(recipe.Loop, window)
	}
	return processor
}

func (t *Processor) Gains(progress float64) (float64, float64) {
	return t.outgoing.curves.volume.At(progress), t.incoming.curves.volume.At(progress)
}

func (t *Processor) Mix(aFrame, bFrame []int16, progress, volume float64) []float64 {
	dsp.FrameToFloat(aFrame, t.outgoing.buffer)
	dsp.FrameToFloat(bFrame, t.incoming.buffer)
	t.run(&t.outgoing, progress, volume)
	t.run(&t.incoming, progress, volume)
	for i := range t.mixBuffer {
		t.mixBuffer[i] = t.outgoing.buffer[i] + t.incoming.buffer[i]
	}
	return t.mixBuffer
}

func (t *Processor) Fade(aFrame []int16, progress, volume float64) []float64 {
	dsp.FrameToFloat(aFrame, t.outgoing.buffer)
	t.run(&t.outgoing, progress, volume)
	return t.outgoing.buffer
}

func (t *Processor) run(deck *deckState, progress, volume float64) {
	isOutgoing := deck == &t.outgoing
	buf := deck.buffer
	if isOutgoing && t.jog != nil {
		t.jog.Process(buf, progress, t.frameStep)
	}

	blockLength := paramBlockSamples * dsp.Channels
	blocks := (len(buf) + blockLength - 1) / blockLength
	for k := 0; k < blocks; k++ {
		block := buf[k*blockLength : min((k+1)*blockLength, len(buf))]
		start := progress + float64(k)/float64(blocks)*t.frameStep
		end := progress + float64(k+1)/float64(blocks)*t.frameStep
		middle := (start + end) / 2
		deck.shape(block, start, middle, end, volume)
		if deck.effect != nil {
			deck.effect.apply(block, t.scratch[:len(block)], start, middle, end, volume)
		}
	}
}

func (d *deckState) shape(block []float64, start, middle, end, volume float64) {
	if d.hasEQ() {
		var levels [bandCount]float64
		for band, curve := range d.curves.eq {
			levels[band] = eqUnity
			if len(curve) > 0 {
				levels[band] = curve.At(middle)
			}
		}
		d.isolator.Process(block, levels)
	}
	if len(d.curves.filter) > 0 {
		d.filter.SetKnob(d.curves.filter.At(middle))
		d.filter.ProcessStereo(block)
	}
	if !d.primed {
		d.gain = d.curves.volume.At(start) * volume
		d.primed = true
	}
	gain := d.curves.volume.At(end) * volume
	dsp.ApplyGainRamp(block, d.gain, gain)
	d.gain = gain
}

func (d *deckState) hasEQ() bool {
	for _, curve := range d.curves.eq {
		if len(curve) > 0 {
			return true
		}
	}
	return false
}

type Tail struct {
	send    sendEffect
	wetGain float64
	silence []float64
	buffer  []float64
	frames  int
	quiet   int
	limiter dsp.Limiter
}

func (t *Processor) MakeTail() *Tail {
	effect := t.outgoing.effect
	if !effect.ringsOut() {
		return nil
	}
	_, wet := dsp.DryWet(effect.amount.At(1))
	size := dsp.FrameSize * dsp.Channels
	return &Tail{
		send:    effect.send,
		wetGain: wet,
		silence: make([]float64, size),
		buffer:  make([]float64, size),
	}
}

func (tail *Tail) ringing() bool {
	return tail.frames < tailMaxFrames && tail.quiet < tailQuietFrames
}

func (tail *Tail) Apply(frame []int16) bool {
	if tail == nil {
		return false
	}
	if tail.ringing() {
		tail.ringOut()
	} else if tail.limiter.IsIdle() {
		return false
	} else {
		dsp.SilenceFloat(tail.buffer)
	}

	for i := range frame {
		tail.buffer[i] += float64(frame[i])
	}
	tail.limiter.ProcessStereo(tail.buffer, dsp.FullScale)
	dsp.FloatToFrame(tail.buffer, frame)
	return tail.ringing() || !tail.limiter.IsIdle()
}

func (tail *Tail) ringOut() {
	tail.send.Render(tail.silence, tail.buffer)
	peak := 0.0
	for i := range tail.buffer {
		tail.buffer[i] *= tail.wetGain
		peak = math.Max(peak, math.Abs(tail.buffer[i]))
	}
	tail.frames++
	if peak < tailSilence {
		tail.quiet++
	} else {
		tail.quiet = 0
	}
}
