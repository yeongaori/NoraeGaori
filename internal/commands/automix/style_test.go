package automix

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

func styleOptions(category, style string) []*discordgo.ApplicationCommandInteractionDataOption {
	options := make([]*discordgo.ApplicationCommandInteractionDataOption, 0, 2)
	if category != "" {
		options = append(options, discordtest.StringOption("category", category))
	}
	if style != "" {
		options = append(options, discordtest.StringOption("style", style))
	}
	return options
}

func firstStyle(category string) string {
	for _, style := range transition.StyleValues(category) {
		if style != queue.AutoMixStyleAuto {
			return style
		}
	}
	return ""
}

func wantFieldCount(count int) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, reply map[string]any) {
		t.Helper()

		if fields, _ := reply["fields"].([]any); len(fields) != count {
			t.Errorf("the reply has %d fields, want %d", len(fields), count)
		}
	}
}

func TestAutoMixStyleCommand(t *testing.T) {
	volumeStyle := firstStyle("volume")
	categoryCount := len(queue.AutoMixStyleCategories())
	styleTitle := func(locale *messages.Locale) string { return locale.Settings.AutoMixStyleTitle }

	commandtest.Run(t, "automixstyle", HandleAutoMixStyle, []commandtest.Case{
		{
			Name:     "every category",
			WantText: styleTitle,
			Check:    wantFieldCount(categoryCount),
		},
		{
			Name:     "a category that is not text",
			Options:  []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("category", 3)},
			WantText: styleTitle,
			Check:    wantFieldCount(categoryCount),
		},
		{
			Name:    "an unknown category",
			Options: styleOptions("bogus", ""),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidCategory, "bogus", strings.Join(queue.AutoMixStyleCategories(), ", "))
			},
		},
		{
			Name:     "one category",
			Options:  styleOptions("volume", ""),
			WantText: styleTitle,
			Check:    wantFieldCount(1),
		},
		{
			Name:    "an unknown style",
			Options: styleOptions("volume", "bogus"),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidValue, "bogus", "volume", strings.Join(transition.StyleValues("volume"), ", "))
			},
		},
		{
			Name:    "a saved style",
			Options: styleOptions("volume", volumeStyle),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleChanged, "volume", volumeStyle)
			},
			Check: func(t *testing.T, _ map[string]any) {
				if stored, err := queue.GetAutoMixStyle(commandtest.GuildID, "volume"); err != nil || stored != volumeStyle {
					t.Errorf("stored volume style = (%q, %v), want %q", stored, err, volumeStyle)
				}
			},
		},
		{
			Name:     "a closed database",
			Options:  styleOptions("volume", volumeStyle),
			Prepare:  dbtest.CloseUntilCleanup,
			WantText: func(locale *messages.Locale) string { return locale.Titles.Error },
		},
	})
}
