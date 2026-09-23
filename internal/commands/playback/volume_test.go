package playback

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/dbtest"
)

func levelOption(value any, optionType discordgo.ApplicationCommandOptionType) []*discordgo.ApplicationCommandInteractionDataOption {
	return []*discordgo.ApplicationCommandInteractionDataOption{{Name: "level", Type: optionType, Value: value}}
}

func wantVolume(want float64) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, _ map[string]any) {
		t.Helper()

		if stored, err := queue.GetVolume(commandtest.GuildID); err != nil || stored != want {
			t.Errorf("stored volume = (%v, %v), want %v", stored, err, want)
		}
	}
}

func closeDatabase(t *testing.T) {
	t.Helper()

	queue.InvalidateCache(commandtest.GuildID)
	dbtest.CloseUntilCleanup(t)
}

func TestVolume(t *testing.T) {
	notNumber := func(locale *messages.Locale) string { return locale.Music.VolumeNotNumber }
	volumeSet := func(locale *messages.Locale) string { return locale.Music.VolumeSetTitle }

	commandtest.Run(t, "volume", HandleVolume, []commandtest.Case{
		{
			Name:     "the current volume",
			WantText: func(locale *messages.Locale) string { return locale.Music.CurrentVolumeTitle },
		},
		{
			Name:     "a whole number",
			Options:  levelOption(float64(50), discordgo.ApplicationCommandOptionInteger),
			WantText: volumeSet,
			Check:    wantVolume(50),
		},
		{
			Name:     "a number past the limit",
			Options:  levelOption(float64(1001), discordgo.ApplicationCommandOptionInteger),
			WantText: func(locale *messages.Locale) string { return locale.Music.VolumeOutOfRange },
		},
		{
			Name:     "a word",
			Options:  levelOption("loud", discordgo.ApplicationCommandOptionString),
			WantText: notNumber,
		},
		{
			Name:     "a number written as text",
			Options:  levelOption("40", discordgo.ApplicationCommandOptionString),
			WantText: volumeSet,
			Check:    wantVolume(40),
		},
		{
			Name:     "a value of another type",
			Options:  levelOption(true, discordgo.ApplicationCommandOptionBoolean),
			WantText: notNumber,
		},
		{
			Name:     "a closed database when reading",
			Prepare:  closeDatabase,
			WantText: func(locale *messages.Locale) string { return commandtest.FormatPrefix(locale.Music.VolumeQueryFailed) },
		},
		{
			Name:     "a closed database when saving",
			Options:  levelOption(float64(50), discordgo.ApplicationCommandOptionInteger),
			Prepare:  closeDatabase,
			WantText: func(locale *messages.Locale) string { return commandtest.FormatPrefix(locale.Music.VolumeSetFailed) },
		},
	})
}

func TestRegisterAddsThePlaybackCommands(t *testing.T) {
	Register(func(string) messages.CommandStrings { return messages.CommandStrings{} })

	registered := command.Snapshot()
	for _, name := range []string{"nowplaying", "volume", "repeat"} {
		if _, found := registered[name]; !found {
			t.Errorf("command %q was not registered", name)
		}
	}
}
