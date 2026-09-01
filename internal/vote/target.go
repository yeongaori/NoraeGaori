package vote

import "noraegaori/internal/queue"

type Scope int

const (
	ScopeCurrentSong Scope = iota
	ScopeWholeQueue
	ScopeSongs
)

type Target struct {
	Scope   Scope
	SongIDs []int
}

func AffectedSongs(q *queue.Queue, target Target) []*queue.Song {
	if q == nil || len(q.Songs) == 0 {
		return nil
	}

	switch target.Scope {
	case ScopeCurrentSong:
		return q.Songs[:1]
	case ScopeWholeQueue:
		return q.Songs
	default:
		wanted := make(map[int]struct{}, len(target.SongIDs))
		for _, songID := range target.SongIDs {
			wanted[songID] = struct{}{}
		}

		songs := make([]*queue.Song, 0, len(target.SongIDs))
		for _, song := range q.Songs {
			if _, ok := wanted[song.ID]; ok {
				songs = append(songs, song)
			}
		}
		return songs
	}
}

var addersFor = func(guildID string, target Target) []string {
	q, err := queue.GetQueue(guildID, false)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	adders := make([]string, 0, 4)

	for _, song := range AffectedSongs(q, target) {
		if song.RequestedByID == "" {
			continue
		}
		if _, known := seen[song.RequestedByID]; known {
			continue
		}
		seen[song.RequestedByID] = struct{}{}
		adders = append(adders, song.RequestedByID)
	}

	return adders
}

func everyAdderVoted(adderVotes map[string]struct{}, adders []string) bool {
	if len(adders) == 0 {
		return false
	}

	for _, adder := range adders {
		if _, voted := adderVotes[adder]; !voted {
			return false
		}
	}
	return true
}

func isAdder(adders []string, userID string) bool {
	for _, adder := range adders {
		if adder == userID {
			return true
		}
	}
	return false
}
