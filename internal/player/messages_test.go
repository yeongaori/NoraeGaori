package player

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

type sentEmbed struct {
	channelID string
	messageID string
	embed     *discordgo.MessageEmbed
}

type fakeEmbedSender struct {
	mu       sync.Mutex
	sends    []sentEmbed
	edits    []sentEmbed
	editFail bool
}

func (f *fakeEmbedSender) ChannelMessageSendEmbed(channelID string, embed *discordgo.MessageEmbed, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, sentEmbed{channelID: channelID, embed: embed})
	return &discordgo.Message{ID: "sent", ChannelID: channelID}, nil
}

func (f *fakeEmbedSender) ChannelMessageEditEmbed(channelID, messageID string, embed *discordgo.MessageEmbed, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editFail {
		return nil, fmt.Errorf("edit rejected")
	}
	f.edits = append(f.edits, sentEmbed{channelID: channelID, messageID: messageID, embed: embed})
	return &discordgo.Message{ID: messageID, ChannelID: channelID}, nil
}

func nowPlayingFixture(guildID string, showStartedTrack bool) (*queue.Song, *queue.Queue) {
	song := &queue.Song{
		ID:             7,
		URL:            "https://youtube.com/watch?v=announce",
		Title:          "Announced Song",
		Duration:       "3:00",
		Uploader:       "Uploader",
		RequestedByTag: "User#1234",
	}
	q := &queue.Queue{
		GuildID:          guildID,
		TextChannelID:    "text",
		ShowStartedTrack: showStartedTrack,
	}
	return song, q
}

func TestSetLoadingMessageClearsAnnouncement(t *testing.T) {
	guildID := "loadingannounce"
	t.Cleanup(func() {
		DeleteLoadingMessage(guildID)
		clearAnnounced(guildID)
	})

	markAnnounced(guildID, 7)

	SetLoadingMessage(guildID, &discordgo.Message{ID: "msg", ChannelID: "channel"})

	if GetLoadingMessage(guildID) == nil {
		t.Fatal("loading message was not stored")
	}

	if !markAnnounced(guildID, 7) {
		t.Error("stale announcement would leave the loading message unresolved")
	}
}

func TestDeliverNowPlayingEditsLoadingMessage(t *testing.T) {
	guildID := "deliveredit"
	t.Cleanup(func() { DeleteLoadingMessage(guildID) })

	SetLoadingMessage(guildID, &discordgo.Message{ID: "loading", ChannelID: "loadingchannel"})
	song, q := nowPlayingFixture(guildID, true)

	sender := &fakeEmbedSender{}
	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.edits) != 1 {
		t.Fatalf("want the loading message edited once, got %d edits", len(sender.edits))
	}
	edit := sender.edits[0]
	if edit.channelID != "loadingchannel" || edit.messageID != "loading" {
		t.Errorf("edited the wrong message: channel=%s id=%s", edit.channelID, edit.messageID)
	}
	if !strings.Contains(edit.embed.Description, song.Title) {
		t.Errorf("embed does not name the song: %s", edit.embed.Description)
	}
	if edit.embed.Color != messages.ColorSuccess {
		t.Errorf("want success color, got %d", edit.embed.Color)
	}
	if len(sender.sends) != 0 {
		t.Errorf("a successful edit should not also post a message, got %d", len(sender.sends))
	}
	if GetLoadingMessage(guildID) != nil {
		t.Error("loading message should be released once it became the now playing embed")
	}
}

func TestDeliverNowPlayingFallsBackWhenEditFails(t *testing.T) {
	guildID := "deliverfallback"
	t.Cleanup(func() { DeleteLoadingMessage(guildID) })

	SetLoadingMessage(guildID, &discordgo.Message{ID: "loading", ChannelID: "loadingchannel"})
	song, q := nowPlayingFixture(guildID, true)

	sender := &fakeEmbedSender{editFail: true}
	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.sends) != 1 {
		t.Fatalf("want one fallback message, got %d", len(sender.sends))
	}
	if sender.sends[0].channelID != q.TextChannelID {
		t.Errorf("fallback went to %s, want %s", sender.sends[0].channelID, q.TextChannelID)
	}
	if GetLoadingMessage(guildID) != nil {
		t.Error("unresolvable loading message should still be released")
	}
}

func TestDeliverNowPlayingStaysQuietWhenFallbackDisabled(t *testing.T) {
	guildID := "deliverquietfallback"
	t.Cleanup(func() { DeleteLoadingMessage(guildID) })

	SetLoadingMessage(guildID, &discordgo.Message{ID: "loading", ChannelID: "loadingchannel"})
	song, q := nowPlayingFixture(guildID, false)

	sender := &fakeEmbedSender{editFail: true}
	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.sends) != 0 {
		t.Errorf("show started track is off, want no message, got %d", len(sender.sends))
	}
}

func TestDeliverNowPlayingPostsWithoutLoadingMessage(t *testing.T) {
	guildID := "deliverpost"
	song, q := nowPlayingFixture(guildID, true)

	sender := &fakeEmbedSender{}
	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.sends) != 1 {
		t.Fatalf("want one now playing message, got %d", len(sender.sends))
	}
	sent := sender.sends[0]
	if sent.channelID != q.TextChannelID {
		t.Errorf("posted to %s, want %s", sent.channelID, q.TextChannelID)
	}
	if !strings.Contains(sent.embed.Description, song.Title) {
		t.Errorf("embed does not name the song: %s", sent.embed.Description)
	}
	if len(sender.edits) != 0 {
		t.Errorf("nothing to edit, got %d edits", len(sender.edits))
	}
}

func TestDeliverNowPlayingRespectsShowStartedTrack(t *testing.T) {
	guildID := "deliverquiet"
	song, q := nowPlayingFixture(guildID, false)

	sender := &fakeEmbedSender{}
	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.sends) != 0 || len(sender.edits) != 0 {
		t.Errorf("show started track is off, want silence, got %d sends and %d edits", len(sender.sends), len(sender.edits))
	}
}

func TestDeliverNowPlayingResolvesReconnectMessage(t *testing.T) {
	guildID := "deliverreconnect"
	t.Cleanup(func() { deleteReconnectMessage(guildID) })

	setReconnectMessage(guildID, &discordgo.Message{ID: "reconnect", ChannelID: "reconnectchannel"})
	song, q := nowPlayingFixture(guildID, false)

	sender := &fakeEmbedSender{}
	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.edits) != 1 {
		t.Fatalf("want the reconnect message edited, got %d edits", len(sender.edits))
	}
	if sender.edits[0].messageID != "reconnect" {
		t.Errorf("edited %s, want the reconnect message", sender.edits[0].messageID)
	}
	if getReconnectMessage(guildID) != nil {
		t.Error("reconnect message should be released once resolved")
	}
}
