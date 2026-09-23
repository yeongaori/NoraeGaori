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

func wantStyleFields(want map[string]string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, reply map[string]any) {
		t.Helper()

		fields, _ := reply["fields"].([]any)
		if len(fields) != len(want) {
			t.Fatalf("the reply has %d fields, want %d", len(fields), len(want))
		}
		for _, field := range fields {
			entry, _ := field.(map[string]any)
			name, _ := entry["name"].(string)
			value, _ := entry["value"].(string)
			current, isExpected := want[name]
			if !isExpected {
				t.Errorf("the reply lists the unexpected category %q", name)
				continue
			}
			if !strings.HasPrefix(value, "**"+current+"**\n") {
				t.Errorf("category %q shows %q, want the current style %q first", name, value, current)
			}
		}
	}
}

func wantStoredStyle(category, want string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, _ map[string]any) {
		t.Helper()

		if stored, err := queue.GetAutoMixStyle(commandtest.GuildID, category); err != nil || stored != want {
			t.Errorf("stored %s style = (%q, %v), want %q", category, stored, err, want)
		}
	}
}

func TestAutoMixStyleCommand(t *testing.T) {
	volumeStyle := firstStyle("volume")
	everyAuto := map[string]string{}
	for _, category := range queue.AutoMixStyleCategories() {
		everyAuto[category] = queue.AutoMixStyleAuto
	}
	styleTitle := func(locale *messages.Locale) string { return locale.Settings.AutoMixStyleTitle }
	saveVolumeStyle := func(t *testing.T) {
		t.Helper()

		if err := queue.SetAutoMixStyle(commandtest.GuildID, "volume", volumeStyle); err != nil {
			t.Fatalf("failed to seed the volume style: %v", err)
		}
	}
	changedTo := func(locale *messages.Locale) string {
		return fmt.Sprintf(locale.Settings.AutoMixStyleChanged, "volume", volumeStyle)
	}

	commandtest.Run(t, "automixstyle", HandleAutoMixStyle, []commandtest.Case{
		{
			Name:     "every category",
			WantText: styleTitle,
			Check:    wantStyleFields(everyAuto),
		},
		{
			Name:     "a category that is not text",
			Options:  []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("category", 3)},
			WantText: styleTitle,
			Check:    wantStyleFields(everyAuto),
		},
		{
			Name:    "an unknown category",
			Options: styleOptions("bogus", ""),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidCategory, "bogus", strings.Join(queue.AutoMixStyleCategories(), ", "))
			},
		},
		{
			Name:     "one category with a saved style",
			Options:  styleOptions("volume", ""),
			Prepare:  saveVolumeStyle,
			WantText: styleTitle,
			Check:    wantStyleFields(map[string]string{"volume": volumeStyle}),
		},
		{
			Name:    "an unknown style",
			Options: styleOptions("volume", "bogus"),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidValue, "bogus", "volume", strings.Join(transition.StyleValues("volume"), ", "))
			},
			Check: wantStoredStyle("volume", queue.AutoMixStyleAuto),
		},
		{
			Name:     "a saved style",
			Options:  styleOptions("volume", volumeStyle),
			WantText: changedTo,
			Check:    wantStoredStyle("volume", volumeStyle),
		},
		{
			Name:     "a style typed in capitals with spaces",
			Options:  styleOptions(" VOLUME ", " "+strings.ToUpper(volumeStyle)+" "),
			WantText: changedTo,
			Check:    wantStoredStyle("volume", volumeStyle),
		},
		{
			Name:    "a closed database",
			Options: styleOptions("volume", volumeStyle),
			Prepare: dbtest.CloseUntilCleanup,
			WantErr: true,
			WantText: func(locale *messages.Locale) string {
				return commandtest.FormatPrefix(locale.Settings.AutoMixStyleError)
			},
		},
	})
}
