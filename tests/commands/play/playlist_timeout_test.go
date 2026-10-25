package play_test

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/commands/play"
	"noraegaori/internal/messages"
	"noraegaori/internal/youtube"
)

func timeoutSong() *youtube.Song {
	return &youtube.Song{
		Title:       "Timeout Song",
		URL:         "https://www.youtube.com/watch?v=timeout",
		Uploader:    "Uploader",
		Duration:    "3:00",
		RequestedBy: "requester",
		Thumbnail:   "https://i.ytimg.com/vi/timeout/hqdefault.jpg",
	}
}

func assertSongDetails(t *testing.T, embed *discordgo.MessageEmbed) {
	t.Helper()

	wants := map[string]string{
		messages.T("guild1").Fields.Uploader:  "Uploader",
		messages.T("guild1").Fields.Requester: "requester",
	}
	for _, field := range embed.Fields {
		if want, isChecked := wants[field.Name]; isChecked && field.Value != want {
			t.Errorf("got %s field %q, want %q", field.Name, field.Value, want)
		}
	}

	want := "https://i.ytimg.com/vi/timeout/hqdefault.jpg"
	if embed.Thumbnail == nil || embed.Thumbnail.URL != want {
		t.Errorf("got thumbnail %v, want %q", embed.Thumbnail, want)
	}
}

func assertCancelledFooter(t *testing.T, footer *discordgo.MessageEmbedFooter) {
	t.Helper()

	want := messages.T("guild1").Music.PlaylistTimeoutDesc
	if footer == nil || footer.Text != want {
		t.Errorf("got footer %v, want %q", footer, want)
	}
}

func TestPlaylistTimeoutEmbedKeepsTheAddedSong(t *testing.T) {
	embed := play.HookPlaylistTimeoutEmbed("guild1", timeoutSong(), false)

	if want := messages.T("guild1").Titles.Added; embed.Title != want {
		t.Errorf("got title %q, want %q", embed.Title, want)
	}
	if embed.Color != messages.ColorSuccess {
		t.Errorf("got color %d, want %d", embed.Color, messages.ColorSuccess)
	}
	if !strings.Contains(embed.Description, "Timeout Song") {
		t.Errorf("got description %q, want it to name the song", embed.Description)
	}
	if strings.Contains(embed.Description, messages.T("guild1").Errors.DuplicateSong) {
		t.Errorf("got description %q, want no duplicate notice", embed.Description)
	}
	assertSongDetails(t, embed)
	assertCancelledFooter(t, embed.Footer)
}

func TestPlaylistTimeoutEmbedKeepsTheDuplicateSong(t *testing.T) {
	embed := play.HookPlaylistTimeoutEmbed("guild1", timeoutSong(), true)

	if want := messages.T("guild1").Titles.Duplicate; embed.Title != want {
		t.Errorf("got title %q, want %q", embed.Title, want)
	}
	if embed.Color != messages.ColorWarning {
		t.Errorf("got color %d, want %d", embed.Color, messages.ColorWarning)
	}
	if !strings.Contains(embed.Description, "Timeout Song") || !strings.Contains(embed.Description, messages.T("guild1").Errors.DuplicateSong) {
		t.Errorf("got description %q, want the song and the duplicate notice", embed.Description)
	}
	assertSongDetails(t, embed)
	assertCancelledFooter(t, embed.Footer)
}

func TestPlaylistTimeoutEmbedWarnsWhenTheVideoWasUnavailable(t *testing.T) {
	embed := play.HookPlaylistTimeoutEmbed("guild1", nil, false)

	if want := messages.T("guild1").Music.PlaylistTimeoutTitle; embed.Title != want {
		t.Errorf("got title %q, want %q", embed.Title, want)
	}
	if want := messages.T("guild1").Music.PlaylistTimeoutDesc; embed.Description != want {
		t.Errorf("got description %q, want %q", embed.Description, want)
	}
	if embed.Footer != nil {
		t.Errorf("got footer %q, want none", embed.Footer.Text)
	}
}
