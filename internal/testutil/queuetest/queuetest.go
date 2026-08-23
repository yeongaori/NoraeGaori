package queuetest

import "noraegaori/internal/queue"

func QueueOf(requesters ...string) *queue.Queue {
	songs := make([]*queue.Song, 0, len(requesters))
	for index, requester := range requesters {
		songs = append(songs, &queue.Song{ID: index + 1, RequestedByID: requester})
	}
	return &queue.Queue{Songs: songs}
}
