package playback

import (
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func TestSkipResultEmbedSwitchesOnAnEmptiedQueue(t *testing.T) {
	song := &queue.Song{Title: "Song", URL: "https://example.com/song", Thumbnail: "https://example.com/thumb.jpg"}

	skipped := skipResultEmbed("g1", song, false)
	ended := skipResultEmbed("g1", song, true)

	if skipped.Title == ended.Title {
		t.Errorf("both embeds titled %q, want the queue-ended variant to differ", skipped.Title)
	}
	if ended.Title != messages.T("g1").Music.PlaybackEndedTitle {
		t.Errorf("queue-ended title = %q, want the playback-ended title", ended.Title)
	}
	if skipped.Thumbnail == nil || skipped.Thumbnail.URL != song.Thumbnail {
		t.Error("the skip embed lost the song thumbnail")
	}
}
