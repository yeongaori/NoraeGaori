package playback

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/settings"
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

func seedVolume(volume float64) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()

		if err := queue.SetVolume(commandtest.GuildID, volume); err != nil {
			t.Fatalf("failed to seed the volume: %v", err)
		}
	}
}

func TestVolume(t *testing.T) {
	notNumber := func(locale *messages.Locale) string { return locale.Music.VolumeNotNumber }
	outOfRange := func(locale *messages.Locale) string { return locale.Music.VolumeOutOfRange }
	volumeSetTo := func(volume float64) func(*messages.Locale) string {
		return func(locale *messages.Locale) string { return fmt.Sprintf(locale.Music.VolumeSetDesc, volume) }
	}
	integer := func(volume float64) []*discordgo.ApplicationCommandInteractionDataOption {
		return levelOption(volume, discordgo.ApplicationCommandOptionInteger)
	}

	commandtest.Run(t, "volume", HandleVolume, []commandtest.Case{
		{
			Name:     "the current volume",
			Prepare:  seedVolume(70),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Music.CurrentVolumeDesc, 70.0) },
		},
		{
			Name:     "a whole number",
			Options:  integer(50),
			WantText: volumeSetTo(50),
			Check:    wantVolume(50),
		},
		{
			Name:     "silence",
			Options:  integer(0),
			WantText: volumeSetTo(0),
			Check:    wantVolume(0),
		},
		{
			Name:     "the upper limit",
			Options:  integer(1000),
			WantText: volumeSetTo(1000),
			Check:    wantVolume(1000),
		},
		{
			Name:     "a number past the limit",
			Options:  integer(1001),
			Prepare:  seedVolume(70),
			WantText: outOfRange,
			Check:    wantVolume(70),
		},
		{
			Name:     "a negative number",
			Options:  integer(-1),
			Prepare:  seedVolume(70),
			WantText: outOfRange,
			Check:    wantVolume(70),
		},
		{
			Name:     "a word",
			Options:  levelOption("loud", discordgo.ApplicationCommandOptionString),
			Prepare:  seedVolume(70),
			WantText: notNumber,
			Check:    wantVolume(70),
		},
		{
			Name:     "a fraction written as text",
			Options:  levelOption("40.5", discordgo.ApplicationCommandOptionString),
			WantText: volumeSetTo(40.5),
			Check:    wantVolume(40.5),
		},
		{
			Name:     "a value of another type",
			Options:  levelOption(true, discordgo.ApplicationCommandOptionBoolean),
			Prepare:  seedVolume(70),
			WantText: notNumber,
			Check:    wantVolume(70),
		},
		{
			Name:     "a closed database when reading",
			Prepare:  closeDatabase,
			WantErr:  true,
			WantText: func(locale *messages.Locale) string { return commandtest.FormatPrefix(locale.Music.VolumeQueryFailed) },
		},
		{
			Name:     "a closed database when saving",
			Options:  integer(50),
			Prepare:  closeDatabase,
			WantErr:  true,
			WantText: func(locale *messages.Locale) string { return commandtest.FormatPrefix(locale.Music.VolumeSetFailed) },
		},
	})
}

func TestRegisterAddsThePlaybackCommands(t *testing.T) {
	commandStrings := func(string) messages.CommandStrings { return messages.CommandStrings{} }
	settings.Register(commandStrings)
	Register(commandStrings)

	commandtest.WantRegistered(t, map[string]commandtest.Registration{
		"nowplaying": {Handler: HandleNowPlaying},
		"volume":     {Handler: HandleVolume},
		"repeat":     {SettingKey: "repeat"},
		"pause":      {Handler: HandlePause},
		"resume":     {Handler: HandleResume},
		"skip":       {Handler: HandleSkip},
		"seek":       {Handler: HandleSeek},
		"stop":       {Handler: HandleStop},
	})
}
