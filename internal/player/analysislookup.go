package player

import (
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/queue"
)

func LookupAnalysis(guildID string, song *queue.Song, segment string) *analysis.TrackAnalysis {
	if song == nil {
		return nil
	}

	if segment == analysis.SegmentHead {
		if cached := GetCachedAnalysis(guildID, song.ID); cached != nil {
			return cached
		}
	}
	return analysis.LoadTrackAnalysis(song.URL, segment)
}

func LookupAnalysisForDisplay(guildID string, song *queue.Song, segment string) *analysis.TrackAnalysis {
	if found := LookupAnalysis(guildID, song, segment); found != nil {
		return found
	}
	if song == nil {
		return nil
	}

	other := analysis.SegmentHead
	if segment == analysis.SegmentHead {
		other = analysis.SegmentTail
	}
	return analysis.LoadTrackAnalysis(song.URL, other)
}
