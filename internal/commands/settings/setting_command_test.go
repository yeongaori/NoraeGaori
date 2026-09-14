package settings

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/database"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

func stringOption(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionString, Value: value}
}

func integerOption(name string, value float64) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionInteger, Value: value}
}

func textSettingInteraction(name string, options ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: checkGuildID,
			Token:   "message_unregistered_channel",
			Data:    discordgo.ApplicationCommandInteractionData{Name: name, Options: options},
		},
	}
}

func seedSetting(t *testing.T, key, value string) {
	t.Helper()

	if err := applySetting(checkGuildID, specFor(t, key), value); err != nil {
		t.Fatalf("failed to seed %s = %q: %v", key, value, err)
	}
}

func storedSetting(t *testing.T, key string) string {
	t.Helper()

	value, ok := currentValue(checkGuildID, specFor(t, key))
	if !ok {
		t.Fatalf("failed to read %s", key)
	}
	return value
}

func settingOptionName(key string) string {
	if key == "repeat" {
		return "mode"
	}
	return "setting"
}

func TestParseSettingArgumentsReadsValuesAndNumbers(t *testing.T) {
	cases := []struct {
		name        string
		key         string
		options     []*discordgo.ApplicationCommandInteractionDataOption
		value       string
		numberKey   string
		numberValue string
	}{
		{"a toggle value", "sponsorblock", []*discordgo.ApplicationCommandInteractionDataOption{stringOption("setting", "on")}, valueOn, "", ""},
		{"unknown text", "sponsorblock", []*discordgo.ApplicationCommandInteractionDataOption{stringOption("setting", "maybe")}, "", "", ""},
		{"the repeat on alias", "repeat", []*discordgo.ApplicationCommandInteractionDataOption{stringOption("mode", "on")}, valueRepeatAll, "", ""},
		{"an uppercase mode", "repeat", []*discordgo.ApplicationCommandInteractionDataOption{stringOption("mode", "SINGLE")}, valueRepeatSingle, "", ""},
		{"a bare number", "fadein", []*discordgo.ApplicationCommandInteractionDataOption{stringOption("setting", "5")}, valueOn, "fadein_duration", "5"},
		{"a state and a number", "fadein", []*discordgo.ApplicationCommandInteractionDataOption{stringOption("setting", "on"), integerOption("duration", 7)}, valueOn, "fadein_duration", "7"},
		{"only a number", "automix", []*discordgo.ApplicationCommandInteractionDataOption{integerOption("beats", 16)}, "", "automix_beats", "16"},
		{"no options", "fadeonstop", nil, "", "", ""},
	}

	for _, c := range cases {
		arguments := parseSettingArguments(specFor(t, c.key), c.options)
		if arguments.hasValue != (c.value != "") || arguments.value != c.value {
			t.Errorf("%s: value = (%q, %v), want %q", c.name, arguments.value, arguments.hasValue, c.value)
		}
		numberKey := ""
		if arguments.number != nil {
			numberKey = arguments.number.key
		}
		if numberKey != c.numberKey || arguments.numberValue != c.numberValue {
			t.Errorf("%s: number = %q=%q, want %q=%q", c.name, numberKey, arguments.numberValue, c.numberKey, c.numberValue)
		}
	}
}

func TestSettingCommandsWithoutArgumentsLeaveTheSettingAlone(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")
	registerSettingMenus()

	for _, key := range dropdownSettingKeys {
		spec := specFor(t, key)
		for _, value := range settingMenuValues(spec) {
			seedSetting(t, key, value)

			_ = HandleSetting(key)(session, textSettingInteraction(key))

			if got := storedSetting(t, key); got != value {
				t.Errorf("%s with no argument changed %q to %q", key, value, got)
			}
		}
	}
}

func TestSettingCommandsStoreTheGivenValue(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")
	registerSettingMenus()

	for _, key := range dropdownSettingKeys {
		spec := specFor(t, key)
		for _, value := range settingMenuValues(spec) {
			_ = HandleSetting(key)(session, textSettingInteraction(key, stringOption(settingOptionName(key), value)))

			if got := storedSetting(t, key); got != value {
				t.Errorf("%s %s stored %q", key, value, got)
			}
		}
	}
}

func TestSettingCommandsRejectOutOfRangeNumbers(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")

	seedSetting(t, "fadein", valueOff)
	seedSetting(t, "fadein_duration", "3")
	if err := HandleSetting("fadein")(session, textSettingInteraction("fadein", stringOption("setting", "99"))); err != nil {
		t.Errorf("a rejected number returned %v, want the rejection handled", err)
	}
	if got := storedSetting(t, "fadein"); got != valueOff {
		t.Errorf("fadein 99 changed fade-in to %q", got)
	}
	if got := storedSetting(t, "fadein_duration"); got != "3" {
		t.Errorf("fadein 99 changed the duration to %q", got)
	}

	seedSetting(t, "automix", valueOff)
	seedSetting(t, "automix_beats", "16")
	_ = HandleSetting("automix")(session, textSettingInteraction("automix", stringOption("setting", "on"), integerOption("beats", 2)))
	if got := storedSetting(t, "automix"); got != valueOff {
		t.Errorf("automix on 2 changed AutoMix to %q", got)
	}
	if got := storedSetting(t, "automix_beats"); got != "16" {
		t.Errorf("automix on 2 changed the beats to %q", got)
	}
}

func TestABareNumberTurnsTheSettingOnWithThatNumber(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")

	seedSetting(t, "fadein", valueOff)
	seedSetting(t, "fadein_duration", "3")
	_ = HandleSetting("fadein")(session, textSettingInteraction("fadein", stringOption("setting", "5")))

	if got := storedSetting(t, "fadein"); got != valueOn {
		t.Errorf("fadein 5 left fade-in %q", got)
	}
	if got := storedSetting(t, "fadein_duration"); got != "5" {
		t.Errorf("fadein 5 stored duration %q", got)
	}
}

func TestHandleSettingWithEmbedUsesTheCustomEmbedOnlyOnSuccess(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")

	calls := 0
	handle := HandleSettingWithEmbed("fadein", func(guildID string) *discordgo.MessageEmbed {
		calls++
		return &discordgo.MessageEmbed{Title: guildID}
	})

	if err := handle(session, textSettingInteraction("fadein", stringOption("setting", "on"))); err != nil {
		t.Fatalf("fadein on failed: %v", err)
	}
	if calls != 1 {
		t.Errorf("the custom embed was built %d times after a success, want 1", calls)
	}

	_ = handle(session, textSettingInteraction("fadein", stringOption("setting", "99")))
	if calls != 1 {
		t.Errorf("the custom embed was built after a rejected number")
	}
}

func closeDatabaseUntilCleanup(t *testing.T) {
	t.Helper()

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	})
}

func TestApplySettingArgumentsNamesTheSettingThatFailedToSave(t *testing.T) {
	dbtest.Setup(t)
	closeDatabaseUntilCleanup(t)
	spec := specFor(t, "automix")

	if failed, err := applySettingArguments(checkGuildID, spec, &settingArguments{value: valueOn, hasValue: true}); err == nil || failed != spec {
		t.Errorf("state write failure = (%v, %v), want the automix spec and an error", failed, err)
	}

	beats := specFor(t, "automix_beats")
	if failed, err := applySettingArguments(checkGuildID, spec, &settingArguments{number: beats, numberValue: "16"}); err == nil || failed != beats {
		t.Errorf("number write failure = (%v, %v), want the automix_beats spec and an error", failed, err)
	}
}

func TestSettingCommandsReportSaveFailuresInsteadOfReturningThem(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")
	closeDatabaseUntilCleanup(t)

	if err := HandleSetting("sponsorblock")(session, textSettingInteraction("sponsorblock", stringOption("setting", valueOn))); err != nil {
		t.Errorf("a save failure returned %v, want it reported in the reply", err)
	}
}

func TestNormalizationWriteFailureIsReturned(t *testing.T) {
	dbtest.Setup(t)
	closeDatabaseUntilCleanup(t)

	if err := writeNormalization(checkGuildID, valueOn); err == nil {
		t.Error("writing normalization to a closed database succeeded")
	}
}

func TestHandleSettingRejectsAnUnknownKey(t *testing.T) {
	if err := HandleSetting("nope")(nil, textSettingInteraction("nope")); err == nil {
		t.Error("an undefined setting key was accepted")
	}
}
