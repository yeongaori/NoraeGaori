package player

import "sync"

var (
	pendingAnalyses   = make(map[string]map[int]int)
	pendingAnalysesMu sync.Mutex
)

func changeAnalysisPending(guildID string, step int, songIDs []int) {
	if len(songIDs) == 0 {
		return
	}
	pendingAnalysesMu.Lock()
	defer pendingAnalysesMu.Unlock()

	current := pendingAnalyses[guildID]
	next := make(map[int]int, len(current)+len(songIDs))
	for songID, count := range current {
		next[songID] = count
	}
	for _, songID := range songIDs {
		if count := next[songID] + step; count > 0 {
			next[songID] = count
		} else {
			delete(next, songID)
		}
	}
	if len(next) == 0 {
		delete(pendingAnalyses, guildID)
		return
	}
	pendingAnalyses[guildID] = next
}

func markAnalysisPending(guildID string, songIDs ...int) {
	changeAnalysisPending(guildID, 1, songIDs)
}

func clearAnalysisPending(guildID string, songIDs ...int) {
	changeAnalysisPending(guildID, -1, songIDs)
}

func PendingAnalyses(guildID string) map[int]int {
	pendingAnalysesMu.Lock()
	defer pendingAnalysesMu.Unlock()
	return pendingAnalyses[guildID]
}
