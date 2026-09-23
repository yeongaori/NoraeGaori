package admin

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/discordtest"
	"noraegaori/internal/testutil/queuetest"
)

const targetUserID = "123456789012345678"

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
		},
		{
			Name:     "a position past the queue",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption("9"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Admin.EnterValidRange, 2) },
		},
		{
			Name:     "a position",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption("2"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SongRemoved, "Song 2") },
			Check:    commandtest.WantTitles("Song 1"),
		},
		{
			Name:     "a user with a single queued song",
			Songs:    queuetest.Songs(targetUserID),
			Options:  targetOption(targetUserID),
			WantText: func(locale *messages.Locale) string { return locale.Admin.NoSongsToDelete },
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
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Admin.UserNoSongs, "") },
		},
		{
			Name:     "a user with no songs while playing",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  targetOption(mention),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Admin.ExcludingCurrent },
		},
		{
			Name:     "a user's songs",
			Songs:    queuetest.Songs(caller, targetUserID, caller, targetUserID),
			Options:  targetOption(mention),
			WantText: func(locale *messages.Locale) string { return locale.Admin.DeleteCompleteTitle },
			Check:    commandtest.WantTitles("Song 1", "Song 3"),
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

	followUps := 0
	for _, request := range fixture.Requests() {
		if request.Method == http.MethodPost && strings.HasPrefix(request.Path, "/webhooks/") {
			followUps++
		}
	}
	if followUps == 0 {
		t.Error("a summary of 30 long titles sent no follow-up message")
	}
	if titles := commandtest.QueueTitles(t); len(titles) != 1 {
		t.Errorf("queue = %v, want only the caller's song", titles)
	}
}

func TestRegisterAddsTheAdminCommands(t *testing.T) {
	Register(func(string) messages.CommandStrings { return messages.CommandStrings{} })

	registered := command.Snapshot()
	for _, name := range []string{"forceskip", "forceremove", "forcestop", "status"} {
		if _, found := registered[name]; !found {
			t.Errorf("command %q was not registered", name)
		}
	}
}
