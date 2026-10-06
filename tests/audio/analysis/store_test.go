package analysis_test

import (
	"math"
	"reflect"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/database"
)

func useAnalysisDatabase(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		database.Close()
		database.DB = nil
	})
	if err := database.Initialize(); err != nil {
		t.Fatalf("Initialize returned %v", err)
	}
}

func TestAnalysisRoundTripsThroughTheDatabase(t *testing.T) {
	useAnalysisDatabase(t)
	saved := &analysis.TrackAnalysis{
		BPM: 128, PeriodSec: 60.0 / 128, FirstBeat: 0.2, Duration: 90, Offset: 120.5,
		Tonic: 3, Minor: true, KeyConfidence: 0.4, DownbeatPhase: 2, BeatStrength: 0.55,
		BarOffsets: []float64{0.01234, -0.004, 0},
	}
	if err := analysis.SaveTrackAnalysis("https://example.com/a", analysis.SegmentTail, saved); err != nil {
		t.Fatalf("SaveTrackAnalysis returned %v", err)
	}

	loaded := analysis.LoadTrackAnalysis("https://example.com/a", analysis.SegmentTail)
	if loaded == nil {
		t.Fatal("the saved analysis did not load")
	}
	offsets := loaded.BarOffsets
	loaded.BarOffsets = nil
	want := *saved
	want.BarOffsets = nil
	if !reflect.DeepEqual(*loaded, want) {
		t.Errorf("loaded %+v, want %+v", *loaded, want)
	}
	if len(offsets) != len(saved.BarOffsets) {
		t.Fatalf("loaded %d bar offsets, want %d", len(offsets), len(saved.BarOffsets))
	}
	for bar := range offsets {
		if math.Abs(offsets[bar]-saved.BarOffsets[bar]) > 1e-5 {
			t.Errorf("bar %d offset %.5f, want %.5f", bar, offsets[bar], saved.BarOffsets[bar])
		}
	}
}

func TestUnreadableBarOffsetsAreDropped(t *testing.T) {
	useAnalysisDatabase(t)
	saved := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, BarOffsets: []float64{0.01, 0.02}}
	if err := analysis.SaveTrackAnalysis("https://example.com/b", analysis.SegmentHead, saved); err != nil {
		t.Fatalf("SaveTrackAnalysis returned %v", err)
	}
	if _, err := database.DB.Exec(`UPDATE track_analysis SET bar_offsets = '0.01,broken,0.03'`); err != nil {
		t.Fatalf("failed to corrupt the row: %v", err)
	}

	loaded := analysis.LoadTrackAnalysis("https://example.com/b", analysis.SegmentHead)
	if loaded == nil {
		t.Fatal("the row did not load")
	}
	if loaded.BarOffsets != nil {
		t.Errorf("bar offsets %v, want none rather than a shifted list", loaded.BarOffsets)
	}
}
