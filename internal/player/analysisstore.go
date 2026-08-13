package player

import (
	"database/sql"
	"time"

	"noraegaori/internal/database"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

const (
	analysisVersion       = 3
	AnalysisSegmentHead   = "head"
	AnalysisSegmentTail   = "tail"
	analysisRetentionDays = 90
)

func PruneTrackAnalysis() error {
	if database.DB == nil {
		return nil
	}

	cutoff := time.Now().Add(-analysisRetentionDays * 24 * time.Hour).Unix()
	result, err := database.DB.Exec(
		`DELETE FROM track_analysis WHERE analyzed_at < ? OR analysis_version != ?`,
		cutoff, analysisVersion,
	)
	if err != nil {
		logger.Warnf("Failed to prune stored analysis: %v", err)
		return err
	}

	var remaining int
	database.DB.QueryRow(`SELECT count(*) FROM track_analysis`).Scan(&remaining)

	if removed, err := result.RowsAffected(); err == nil {
		logger.Infof("Pruned %d stored rows (older than %d days or not version %d), %d remain",
			removed, analysisRetentionDays, analysisVersion, remaining)
	}
	return nil
}

func SaveTrackAnalysis(url, segment string, analysis *TrackAnalysis) error {
	if url == "" || analysis == nil || database.DB == nil {
		return nil
	}

	minor := 0
	if analysis.Minor {
		minor = 1
	}

	_, err := database.DB.Exec(
		`INSERT OR REPLACE INTO track_analysis
		 (url, segment, bpm, period_sec, first_beat, duration, tonic, minor,
		  key_confidence, downbeat_phase, analysis_version, analyzed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		url, segment, analysis.BPM, analysis.PeriodSec, analysis.FirstBeat,
		analysis.Duration, analysis.Tonic, minor, analysis.KeyConfidence,
		analysis.DownbeatPhase, analysisVersion, time.Now().Unix(),
	)
	if err != nil {
		logger.Warnf("Failed to save %s analysis: %v", segment, err)
	}
	return err
}

func LoadTrackAnalysis(url, segment string) *TrackAnalysis {
	if url == "" || database.DB == nil {
		return nil
	}

	var analysis TrackAnalysis
	var minor, version int

	err := database.DB.QueryRow(
		`SELECT bpm, period_sec, first_beat, duration, tonic, minor,
		 key_confidence, downbeat_phase, analysis_version
		 FROM track_analysis WHERE url = ? AND segment = ?`,
		url, segment,
	).Scan(&analysis.BPM, &analysis.PeriodSec, &analysis.FirstBeat,
		&analysis.Duration, &analysis.Tonic, &minor, &analysis.KeyConfidence,
		&analysis.DownbeatPhase, &version)

	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		logger.Warnf("Failed to load %s analysis: %v", segment, err)
		return nil
	}
	if version != analysisVersion {
		logger.Debugf("Discarding %s row for %s: stored version %d, current %d",
			segment, url, version, analysisVersion)
		return nil
	}

	analysis.Minor = minor == 1
	return &analysis
}

func LookupAnalysis(guildID string, song *queue.Song, segment string) *TrackAnalysis {
	if song == nil {
		return nil
	}

	if segment == AnalysisSegmentHead {
		if cached := GetCachedAnalysis(guildID, song.ID); cached != nil {
			return cached
		}
	}
	return LoadTrackAnalysis(song.URL, segment)
}

func LookupAnalysisForDisplay(guildID string, song *queue.Song, segment string) *TrackAnalysis {
	if analysis := LookupAnalysis(guildID, song, segment); analysis != nil {
		return analysis
	}
	if song == nil {
		return nil
	}

	other := AnalysisSegmentHead
	if segment == AnalysisSegmentHead {
		other = AnalysisSegmentTail
	}
	return LoadTrackAnalysis(song.URL, other)
}

func AnalysisSummary(analysis *TrackAnalysis) (float64, string, string, bool) {
	if analysis == nil {
		return 0, "", "", false
	}
	if analysis.KeyConfidence < keyConfidenceFloor {
		return analysis.BPM, "", "", false
	}
	return analysis.BPM, keyName(analysis.Tonic, analysis.Minor), camelotCode(analysis.Tonic, analysis.Minor), true
}

func TransitionCompatibility(a, b *TrackAnalysis) (float64, int, bool) {
	if a == nil || b == nil || a.BPM <= 0 || b.BPM <= 0 {
		return 0, -1, false
	}
	return signedTempoDelta(a.BPM, b.BPM), camelotDistance(a, b), true
}
