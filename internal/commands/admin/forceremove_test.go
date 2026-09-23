package admin

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/discordtest"
	"noraegaori/internal/testutil/queuetest"
)

const (
	targetUserID   = "12345678901234567"
	sixteenDigitID = "1234567890123456"
	stubUsername   = "fetched-user"
)

func targetOption(target string) []*discordgo.ApplicationCommandInteractionDataOption {
	return []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("target", target)}
}

func missingUser(r *http.Request) int {
	if strings.Contains(r.URL.Path, "/users/") {
		return http.StatusNotFound
	}
	return http.StatusOK
}

func longTitledSongs(count int, requesterID string) []*queue.Song {
	songs := make([]*queue.Song, 0, count)
	for index := range count {
		songs = append(songs, queuetest.Song(fmt.Sprintf("%s%d", strings.Repeat("Long title ", 20), index+1), requesterID))
	}
	return songs
}

func TestForceRemove(t *testing.T) {
	caller := commandtest.CallerID
	mention := "<@" + targetUserID + ">"
	validRange := func(count int) func(*messages.Locale) string {
		return func(locale *messages.Locale) string { return fmt.Sprintf(locale.Admin.EnterValidRange, count) }
	}

	commandtest.Run(t, "forceremove", HandleForceRemove, []commandtest.Case{
		{
			Name:     "a missing target",
			WantText: func(locale *messages.Locale) string { return locale.Admin.MentionTarget },
		},
		{
			Name:     "an empty queue",
			Options:  targetOption("1"),
			WantText: func(locale *messages.Locale) string { return locale.Admin.NoSongsToDelete },
		},
		{
			Name:     "a target that is not a mention",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption("abc"),
			WantText: func(locale *messages.Locale) string { return locale.Admin.InvalidMention },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "position zero",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption("0"),
			WantText: validRange(2),
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "the position just past the queue",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption("3"),
			WantText: validRange(2),
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "sixteen digits read as a position",
			Songs:    queuetest.Songs(caller, sixteenDigitID),
			Options:  targetOption(sixteenDigitID),
			WantText: validRange(2),
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "a position",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  targetOption("2"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SongRemoved, "Song 2") },
			Check:    commandtest.WantTitles("Song 1", "Song 3"),
		},
		{
			Name:     "a user with a single queued song",
			Songs:    queuetest.Songs(targetUserID),
			Options:  targetOption(targetUserID),
			WantText: func(locale *messages.Locale) string { return locale.Admin.NoSongsToDelete },
			Check:    commandtest.WantTitles("Song 1"),
		},
		{
			Name:          "a user Discord cannot find",
			Songs:         queuetest.Songs(caller, targetUserID),
			Options:       targetOption(targetUserID),
			DiscordStatus: missingUser,
			WantText:      func(locale *messages.Locale) string { return locale.Admin.UserNotFound },
			Check:         commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "a user with no songs",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption(mention),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Admin.UserNoSongs, stubUsername) },
			Check:    commandtest.ReplyLacks(messages.T(commandtest.GuildID).Admin.ExcludingCurrent),
		},
		{
			Name:    "a user whose only song is playing",
			Songs:   queuetest.Songs(targetUserID, caller),
			Options: targetOption("<@!" + targetUserID + ">"),
			Prepare: commandtest.Playing,
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Admin.UserNoSongs, stubUsername) + locale.Admin.ExcludingCurrent
			},
			Check: commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:    "a user's songs while one of them plays",
			Songs:   queuetest.Songs(targetUserID, caller, targetUserID, targetUserID),
			Options: targetOption(mention),
			Prepare: commandtest.Playing,
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Admin.DeleteCompleteDesc, stubUsername, 2)
			},
			Check: func(t *testing.T, reply map[string]any) {
				commandtest.ReplyContains("Song 3", "Song 4")(t, reply)
				commandtest.WantTitles("Song 1", "Song 2")(t, reply)
			},
		},
	})
}

func TestForceRemoveSplitsALongSummary(t *testing.T) {
	songs := append(queuetest.Songs(commandtest.CallerID), longTitledSongs(30, targetUserID)...)
	fixture := commandtest.NewFixture(t, nil, songs...)
	ic := discordtest.SlashInteraction(commandtest.GuildID, "forceremove", discordtest.Member(commandtest.GuildID, commandtest.CallerID), targetOption(targetUserID)...)

	if err := HandleForceRemove(fixture.Session, ic); err != nil {
		t.Fatalf("HandleForceRemove returned %v", err)
	}

	var sent []discordtest.Request
	for _, request := range fixture.Requests() {
		if request.Method == http.MethodPost {
			sent = append(sent, request)
		}
	}
	if len(sent) < 2 {
		t.Fatalf("sent %d messages, want the summary split over several", len(sent))
	}
	var summary strings.Builder
	for index := range sent {
		description, _ := discordtest.ReplyEmbed(t, &sent[index])["description"].(string)
		if length := len([]rune(description)); length > 4096 {
			t.Errorf("message %d carries %d characters, over the embed limit", index, length)
		}
		summary.WriteString(description)
		summary.WriteByte('\n')
	}
	for _, song := range longTitledSongs(30, targetUserID) {
		if count := strings.Count(summary.String(), "["+song.Title+"]"); count != 1 {
			t.Errorf("%q is listed %d times, want once", song.Title, count)
		}
	}
	if titles := commandtest.QueueTitles(t); len(titles) != 1 || titles[0] != "Song 1" {
		t.Errorf("queue = %v, want only the caller's song", titles)
	}
}

func TestRegisterAddsTheAdminCommands(t *testing.T) {
	Register(func(string) messages.CommandStrings { return messages.CommandStrings{} })

	commandtest.WantRegistered(t, map[string]commandtest.Registration{
		"forceskip":   {Handler: HandleForceSkip, IsAdminOnly: true},
		"forceremove": {Handler: HandleForceRemove, IsAdminOnly: true},
		"forcestop":   {Handler: HandleForceStop, IsAdminOnly: true},
		"status":      {Handler: HandleStatus, IsAdminOnly: true},
	})
}
