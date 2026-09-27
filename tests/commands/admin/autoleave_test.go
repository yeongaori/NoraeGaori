package admin_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/admin"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func autoLeaveCases(songs []*queue.Song, options []*discordgo.ApplicationCommandInteractionDataOption, isLoading bool, wantText func(isEnabled bool) func(*messages.Locale) string) []commandtest.Case {
	cases := make([]commandtest.Case, 0, 2)
	for _, isEnabled := range []bool{true, false} {
		prepare, check := commandtest.AutoLeave(isEnabled)
		if isLoading {
			prepareAutoLeave := prepare
			prepare = func(t *testing.T) {
				prepareAutoLeave(t)
				commandtest.Loading(t)
			}
		}
		cases = append(cases, commandtest.Case{
			Name:     fmt.Sprintf("auto-leave %t", isEnabled),
			Songs:    songs,
			Options:  options,
			Prepare:  prepare,
			WantText: wantText(isEnabled),
			Check:    check,
		})
	}
	return cases
}

func TestForceSkippingTheLastSongFollowsAutoLeave(t *testing.T) {
	song := queuetest.Song("Last song", commandtest.CallerID)
	link := messages.FormatMaskedLink(song.Title, song.URL)
	leaving := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Music.ForceSkippedEnded, link) }
	staying := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Music.ForceSkippedEndedStay, link) }

	cases := autoLeaveCases([]*queue.Song{song}, nil, false, func(isEnabled bool) func(*messages.Locale) string {
		if isEnabled {
			return leaving
		}
		return staying
	})
	checkPlayer := cases[1].Check
	cases[1].Check = func(t *testing.T, reply map[string]any) {
		checkPlayer(t, reply)
		if text := discordtest.EmbedText(reply); strings.Contains(text, leaving(messages.T(commandtest.GuildID))) {
			t.Errorf("the reply %q says the bot is leaving with auto-leave off", text)
		}
	}

	commandtest.Run(t, "forceskip", admin.HandleForceSkip, cases)
}

func TestForceStopFollowsAutoLeave(t *testing.T) {
	commandtest.Run(t, "forcestop", admin.HandleForceStop, autoLeaveCases(queuetest.SongsBy(commandtest.CallerID, 1), nil, false, func(bool) func(*messages.Locale) string {
		return func(locale *messages.Locale) string { return locale.Admin.ForceStopDesc }
	}))
}

func TestForceRemovingTheLastPlayingSongFollowsAutoLeave(t *testing.T) {
	song := queuetest.Song("Last song", commandtest.CallerID)

	commandtest.Run(t, "forceremove", admin.HandleForceRemove, autoLeaveCases([]*queue.Song{song}, targetOption("1"), true, func(bool) func(*messages.Locale) string {
		return func(locale *messages.Locale) string {
			return fmt.Sprintf(locale.Queue.SongRemoved, messages.EscapeMarkdown(song.Title))
		}
	}))
}
