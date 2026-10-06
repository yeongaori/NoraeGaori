package transition

import (
	"fmt"
	"math"
	"slices"

	"noraegaori/internal/audio/analysis"
)

const (
	maxOverlapSec       = 35.0
	maxOverlapShare     = 0.2
	maxSpeedChange      = 0.10
	minBeatStrength     = 0.3
	maxBarVariance      = 0.0005
	positionFloor       = 0.40
	kneeShare           = 0.15
	kneeSec             = 90.0
	edgeSec             = 15.0
	edgeGap             = 0.01
	keepPerBarCount     = 5
	compatibleKeyTiers  = 3
	fallbackCapSec      = 5.0
	fallbackReserveSec  = 30.0
	cueScore            = 0.0
	vocalScore          = 1.0
	genreScore          = 1.0
	shortWindowKeyScore = 0.9
	midWindowKeyScore   = 0.8
	longWindowKeyScore  = 0.6
	shortWindowBars     = 4
	midWindowBars       = 8
)

var candidateBars = []int{16, 8, 4, 2}

type Track struct {
	URL      string
	Duration float64
	End      float64
	Analysis *analysis.TrackAnalysis
}

type Pair struct {
	From          Track
	To            Track
	MaxBeats      int
	EarliestStart float64
	Settings      Settings
}

type Overlap struct {
	Beatmatched bool
	Bars        int
	StartA      float64
	StartB      float64
	Length      float64
	SpeedB      float64
	Preset      int
}

func (o Overlap) String() string {
	return fmt.Sprintf("preset=%d bars=%d beatmatched=%t startA=%.3f startB=%.3f length=%.3f speedB=%.4f",
		o.Preset, o.Bars, o.Beatmatched, o.StartA, o.StartB, o.Length, o.SpeedB)
}

type candidate struct {
	bars     int
	startA   float64
	startB   float64
	barA     int
	barB     int
	position float64
	key      float64
	total    float64
	front    int
}

type grid struct {
	firstDownbeat float64
	barLength     float64
	lowest        float64
	highest       float64
}

func SelectOverlap(pair *Pair) (Overlap, bool) {
	overlap, ok := Overlap{}, false
	if pair.Settings.Beatmatch != BeatmatchOff {
		overlap, ok = beatmatchedOverlap(pair)
	}
	if !ok {
		overlap, ok = fallbackOverlap(pair)
	}
	if ok && pair.Settings.Preset > 0 {
		overlap.Preset = pair.Settings.Preset
	}
	return overlap, ok
}

func (pair *Pair) barCounts() []int {
	if pair.Settings.Bars > 0 {
		return []int{pair.Settings.Bars}
	}
	counts := make([]int, 0, len(candidateBars))
	for _, bars := range candidateBars {
		if bars*analysis.BarBeats <= pair.MaxBeats {
			counts = append(counts, bars)
		}
	}
	return counts
}

func beatmatchedOverlap(pair *Pair) (Overlap, bool) {
	a, b := pair.From.Analysis, pair.To.Analysis
	isForced := pair.Settings.Beatmatch == BeatmatchOn
	speedB, ok := beatmatchSpeed(a, b, isForced)
	if !ok {
		return Overlap{}, false
	}

	durationA := pair.From.duration()
	durationB := pair.To.duration()
	gridA := grid{
		firstDownbeat: a.FirstDownbeat(),
		barLength:     a.BarLength(),
		lowest:        math.Max(a.Offset+a.FirstBeat, pair.EarliestStart),
		highest:       pair.From.end(),
	}
	gridB := grid{
		firstDownbeat: b.FirstDownbeat(),
		barLength:     gridA.barLength * speedB,
		lowest:        b.Offset + b.FirstBeat,
		highest:       b.Offset + b.Duration,
	}
	keyTier := analysis.KeyTier(a, b)

	var candidates []candidate
	for _, bars := range pair.barCounts() {
		lengthA := float64(bars) * gridA.barLength
		lengthB := float64(bars) * gridB.barLength
		if !fitsTrack(lengthA, durationA) || !fitsTrack(lengthB, durationB) {
			continue
		}
		startsB := steadyStarts(b, gridB.startsFromBeginning(lengthB), lengthB, isForced)
		for _, startA := range steadyStarts(a, gridA.startsFromEnd(lengthA), lengthA, isForced) {
			for _, startB := range startsB {
				position := positionScore(startA, durationA, startB+lengthB, durationB)
				if position <= 0 {
					continue
				}
				key := keyScore(keyTier, bars)
				candidates = append(candidates, candidate{
					bars:     bars,
					startA:   startA,
					startB:   startB,
					barA:     int(math.Round((gridA.highest - startA - lengthA) / gridA.barLength)),
					barB:     int(math.Round((startB - gridB.firstDownbeat) / gridB.barLength)),
					position: position,
					key:      key,
					total:    (cueScore + vocalScore + key + genreScore) * position,
				})
			}
		}
	}

	best, ok := rankCandidates(candidates)
	if !ok {
		return Overlap{}, false
	}
	return Overlap{
		Beatmatched: true,
		Bars:        best.bars,
		StartA:      correctedStart(a, best.startA),
		StartB:      correctedStart(b, best.startB),
		Length:      float64(best.bars) * gridA.barLength,
		SpeedB:      speedB,
		Preset:      pickPreset(pair.From.URL, pair.To.URL, best.bars, best.barA, best.barB),
	}, true
}

func beatmatchSpeed(a, b *analysis.TrackAnalysis, isForced bool) (float64, bool) {
	if a == nil || b == nil || a.BPM <= 0 || b.BPM <= 0 || a.PeriodSec <= 0 || b.PeriodSec <= 0 {
		return 0, false
	}
	if !isForced && (a.BeatStrength < minBeatStrength || b.BeatStrength < minBeatStrength) {
		return 0, false
	}
	_, factor := analysis.TempoDeltaFactor(a.BPM, b.BPM)
	speed := a.BPM / (b.BPM * factor)
	if math.Abs(speed-1) > maxSpeedChange {
		return 0, false
	}
	return speed, true
}

func steadyStarts(track *analysis.TrackAnalysis, starts []float64, length float64, isForced bool) []float64 {
	if isForced {
		return starts
	}
	steady := starts[:0:0]
	for _, start := range starts {
		if variance, ok := track.BarLengthVariance(start, start+length); ok && variance < maxBarVariance {
			steady = append(steady, start)
		}
	}
	return steady
}

func correctedStart(track *analysis.TrackAnalysis, start float64) float64 {
	if offset, ok := track.OffsetAt(start); ok {
		return start + offset
	}
	return start
}

func (t *Track) duration() float64 {
	if t.Duration > 0 {
		return t.Duration
	}
	if t.End > 0 {
		return t.End
	}
	if t.Analysis != nil {
		return t.Analysis.Offset + t.Analysis.Duration
	}
	return 0
}

func (t *Track) end() float64 {
	if t.End > 0 {
		return t.End
	}
	return t.duration()
}

func fitsTrack(length, duration float64) bool {
	return length < maxOverlapSec && length <= maxOverlapShare*duration
}

func (g grid) startsFromEnd(length float64) []float64 {
	var starts []float64
	last := math.Floor((g.highest - length - g.firstDownbeat) / g.barLength)
	for bar := last; ; bar-- {
		start := g.firstDownbeat + bar*g.barLength
		if start < g.lowest {
			return starts
		}
		starts = append(starts, start)
	}
}

func (g grid) startsFromBeginning(length float64) []float64 {
	var starts []float64
	first := math.Ceil((g.lowest - g.firstDownbeat) / g.barLength)
	for bar := first; ; bar++ {
		start := g.firstDownbeat + bar*g.barLength
		if start+length > g.highest {
			return starts
		}
		starts = append(starts, start)
	}
}

func positionScore(startA, durationA, endB, durationB float64) float64 {
	if durationA <= 0 || durationB <= 0 {
		return 0
	}
	kneeA := math.Min(kneeSec/durationA, kneeShare)
	kneeB := math.Min(kneeSec/durationB, kneeShare)
	rampA := clampedRamp(startA/durationA, 0.5+edgeSec/durationA, 1-kneeA)
	lowB := math.Max(kneeB+edgeGap, 0.5-edgeSec/durationB)
	rampB := clampedRamp(endB/durationB, lowB, kneeB)
	score := rampA * rampB
	if score < positionFloor {
		return 0
	}
	return score
}

func clampedRamp(value, zeroAt, fullAt float64) float64 {
	if zeroAt == fullAt {
		return 0
	}
	return math.Max(0, math.Min(1, (value-zeroAt)/(fullAt-zeroAt)))
}

func keyScore(tier, bars int) float64 {
	switch {
	case tier < compatibleKeyTiers:
		return 1
	case bars <= shortWindowBars:
		return shortWindowKeyScore
	case bars <= midWindowBars:
		return midWindowKeyScore
	}
	return longWindowKeyScore
}

func rankCandidates(candidates []candidate) (candidate, bool) {
	if len(candidates) == 0 {
		return candidate{}, false
	}
	slices.SortStableFunc(candidates, func(x, y candidate) int {
		if x.bars != y.bars {
			return y.bars - x.bars
		}
		return compareDescending(x.total, y.total)
	})

	kept := make([]candidate, 0, len(candidates))
	perBars := map[int]int{}
	for _, entry := range candidates {
		if perBars[entry.bars] < keepPerBarCount {
			perBars[entry.bars]++
			kept = append(kept, entry)
		}
	}

	assignFronts(kept)
	slices.SortStableFunc(kept, func(x, y candidate) int {
		if x.front != y.front {
			return x.front - y.front
		}
		if order := compareDescending(x.total, y.total); order != 0 {
			return order
		}
		return y.bars - x.bars
	})
	return kept[0], true
}

func compareDescending(x, y float64) int {
	switch {
	case x > y:
		return -1
	case x < y:
		return 1
	}
	return 0
}

func assignFronts(candidates []candidate) {
	remaining := len(candidates)
	assigned := make([]bool, len(candidates))
	for front := 0; remaining > 0; front++ {
		var members []int
		for i := range candidates {
			if assigned[i] {
				continue
			}
			dominated := false
			for j := range candidates {
				if !assigned[j] && j != i && dominates(candidates[j], candidates[i]) {
					dominated = true
					break
				}
			}
			if !dominated {
				members = append(members, i)
			}
		}
		for _, i := range members {
			candidates[i].front = front
			assigned[i] = true
			remaining--
		}
	}
}

func dominates(x, y candidate) bool {
	if x.position < y.position || x.key < y.key {
		return false
	}
	return x.position > y.position || x.key > y.key
}

func fallbackOverlap(pair *Pair) (Overlap, bool) {
	duration := pair.From.duration()
	if durationB := pair.To.duration(); durationB > 0 && (duration <= 0 || durationB < duration) {
		duration = durationB
	}
	limit := math.Min((duration-fallbackReserveSec)/2, maxOverlapShare*duration)
	length := math.Min(limit, fallbackCapSec)
	if bars := pair.Settings.Bars; bars > 0 {
		periodSec := 0.0
		if pair.From.Analysis != nil {
			periodSec = pair.From.Analysis.PeriodSec
		}
		length = math.Min(limit, math.Min(maxOverlapSec, float64(bars*analysis.BarBeats)*beatPeriod(periodSec)))
	}
	end := pair.From.end()
	start := math.Max(end-length, pair.EarliestStart)
	if end-start <= 0 {
		return Overlap{}, false
	}
	return Overlap{
		Bars:   pair.Settings.Bars,
		StartA: start,
		Length: end - start,
		SpeedB: 1,
		Preset: FadePreset,
	}, true
}
