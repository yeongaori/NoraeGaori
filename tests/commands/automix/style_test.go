package automix_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/commands/automix"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
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

func firstStyle(category transition.Category) string {
	return transition.StyleValues(category)[1]
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

func wantStoredStyles(want map[transition.Category]string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, _ map[string]any) {
		t.Helper()

		stored, err := queue.GetAutoMixOverrides(commandtest.GuildID)
		if err != nil {
			t.Fatalf("failed to read the stored styles: %v", err)
		}
		for category, style := range want {
			got, ok := stored[string(category)]
			if !ok {
				got = transition.StyleAuto
			}
			if got != style {
				t.Errorf("stored %s style = %q, want %q", category, got, style)
			}
		}
	}
}

func joinedCommandCategories() string {
	names := []string{}
	for _, category := range append(transition.StyleCategories(), transition.ShortcutCategories()...) {
		names = append(names, string(category))
	}
	return strings.Join(names, ", ")
}

func TestAutoMixStyleCommand(t *testing.T) {
	everyAuto := map[string]string{}
	for _, category := range transition.StyleCategories() {
		everyAuto[string(category)] = transition.StyleAuto
	}
	styleTitle := func(locale *messages.Locale) string { return locale.Settings.AutoMixStyleTitle }
	saveStyles := func(t *testing.T) {
		t.Helper()

		if err := queue.SetAutoMixOverrides(commandtest.GuildID, map[string]string{"fx_out": "phaser", "volume_out": "slow"}); err != nil {
			t.Fatalf("failed to seed styles: %v", err)
		}
	}
	changedTo := func(category, style string) func(locale *messages.Locale) string {
		return func(locale *messages.Locale) string {
			return fmt.Sprintf(locale.Settings.AutoMixStyleChanged, category, style)
		}
	}

	commandtest.Run(t, "automixstyle", automix.HandleAutoMixStyle, []commandtest.Case{
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
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidCategory, "bogus", joinedCommandCategories())
			},
		},
		{
			Name:    "a song-only setting",
			Options: styleOptions("preset", "3"),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidCategory, "preset", joinedCommandCategories())
			},
		},
		{
			Name:     "one side with a saved style",
			Options:  styleOptions("fx_out", ""),
			Prepare:  saveStyles,
			WantText: styleTitle,
			Check:    wantStyleFields(map[string]string{"fx_out": "phaser"}),
		},
		{
			Name:     "a shortcut shows both sides",
			Options:  styleOptions("volume", ""),
			Prepare:  saveStyles,
			WantText: styleTitle,
			Check:    wantStyleFields(map[string]string{"volume": "slow / auto"}),
		},
		{
			Name:    "an unknown style",
			Options: styleOptions("fx_out", "bogus"),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Settings.AutoMixStyleInvalidValue, "bogus", "fx_out", strings.Join(transition.StyleValues(transition.CategoryFXOut), ", "))
			},
			Check: wantStoredStyles(map[transition.Category]string{transition.CategoryFXOut: transition.StyleAuto}),
		},
		{
			Name:     "a saved side style",
			Options:  styleOptions("fx_out", "phaser"),
			WantText: changedTo("fx_out", "phaser"),
			Check:    wantStoredStyles(map[transition.Category]string{transition.CategoryFXOut: "phaser"}),
		},
		{
			Name:     "a style typed in capitals with spaces",
			Options:  styleOptions(" FX_OUT ", " PHASER "),
			WantText: changedTo("fx_out", "phaser"),
			Check:    wantStoredStyles(map[transition.Category]string{transition.CategoryFXOut: "phaser"}),
		},
		{
			Name:     "a shortcut sets both sides",
			Options:  styleOptions("volume", "overlap"),
			WantText: changedTo("volume", "overlap"),
			Check: wantStoredStyles(map[transition.Category]string{
				transition.CategoryVolumeOut: "fast_at_end", transition.CategoryVolumeIn: "fast_at_start",
			}),
		},
		{
			Name:     "a shortcut set to auto resets both sides",
			Options:  styleOptions("volume", "auto"),
			Prepare:  saveStyles,
			WantText: changedTo("volume", "auto"),
			Check: wantStoredStyles(map[transition.Category]string{
				transition.CategoryVolumeOut: transition.StyleAuto, transition.CategoryVolumeIn: transition.StyleAuto,
				transition.CategoryFXOut: "phaser",
			}),
		},
		{
			Name:    "a closed database",
			Options: styleOptions("fx_out", "phaser"),
			Prepare: dbtest.CloseUntilCleanup,
			WantErr: true,
			WantText: func(locale *messages.Locale) string {
				return commandtest.FormatPrefix(locale.Settings.AutoMixStyleError)
			},
		},
	})
}
