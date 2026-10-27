package play_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/play"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
)

func badPlayRequestCases() []commandtest.Case {
	return []commandtest.Case{
		{
			Name:       "a missing query",
			VoiceUsers: []string{commandtest.CallerID},
			WantText:   func(locale *messages.Locale) string { return locale.Music.EnterQuery },
		},
		{
			Name:     "a caller outside voice",
			Options:  []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("query", "a song")},
			WantText: func(locale *messages.Locale) string { return locale.Errors.NotInVoiceChannel },
		},
		{
			Name:       "an unsupported YouTube link",
			VoiceUsers: []string{commandtest.CallerID},
			Options:    []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("query", "https://www.youtube.com/")},
			WantText:   func(locale *messages.Locale) string { return locale.Music.UnsupportedURL },
		},
	}
}

func TestPlayRejectsBadRequests(t *testing.T) {
	commandtest.Run(t, "play", play.HandlePlay, badPlayRequestCases())
}

func TestPlayNextRejectsBadRequests(t *testing.T) {
	commandtest.Run(t, "playnext", play.HandlePlayNext, badPlayRequestCases())
}

func TestPlayRequestMarkdown(t *testing.T) {
	fixture := commandtest.NewFixture(t, []string{commandtest.CallerID})
	ic := discordtest.SlashInteraction(commandtest.GuildID, "play", discordtest.Member(commandtest.GuildID, commandtest.CallerID), discordtest.StringOption("query", "**a song**"))

	if query, _, ok := play.HookReadPlayRequest(fixture.Session, ic); !ok || query != "a song" {
		t.Errorf("readPlayRequest = (%q, %v), want (%q, true)", query, ok, "a song")
	}
}
