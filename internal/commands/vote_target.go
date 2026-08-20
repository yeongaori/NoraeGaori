package commands

import "noraegaori/internal/queue"

type voteScope int

const (
	voteScopeCurrentSong voteScope = iota
	voteScopeWholeQueue
	voteScopeSongs
)

type voteTarget struct {
	scope   voteScope
	songIDs []int
}

func affectedSongs(q *queue.Queue, target voteTarget) []*queue.Song {
	if q == nil || len(q.Songs) == 0 {
		return nil
	}

	switch target.scope {
	case voteScopeCurrentSong:
		return q.Songs[:1]
	case voteScopeWholeQueue:
		return q.Songs
	default:
		wanted := make(map[int]struct{}, len(target.songIDs))
		for _, songID := range target.songIDs {
			wanted[songID] = struct{}{}
		}

		songs := make([]*queue.Song, 0, len(target.songIDs))
		for _, song := range q.Songs {
			if _, ok := wanted[song.ID]; ok {
				songs = append(songs, song)
			}
		}
		return songs
	}
}

var addersFor = func(guildID string, target voteTarget) []string {
	q, err := queue.GetQueue(guildID, false)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	adders := make([]string, 0, 4)

	for _, song := range affectedSongs(q, target) {
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
