package settings_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/settings"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
)

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

	if err := settings.HookApplySetting(checkGuildID, specFor(t, key), value); err != nil {
		t.Fatalf("failed to seed %s = %q: %v", key, value, err)
	}
}

func storedSetting(t *testing.T, key string) string {
	t.Helper()

	value, ok := settings.HookCurrentValue(checkGuildID, specFor(t, key))
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
		{"a toggle value", "sponsorblock", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "on")}, settings.HookValueOn, "", ""},
		{"unknown text", "sponsorblock", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "maybe")}, "", "", ""},
		{"the repeat on alias", "repeat", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("mode", "on")}, settings.HookValueRepeatAll, "", ""},
		{"an uppercase mode", "repeat", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("mode", "SINGLE")}, settings.HookValueRepeatSingle, "", ""},
		{"a bare number", "fadein", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "5")}, settings.HookValueOn, "fadein_duration", "5"},
		{"a state and a number", "fadein", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "on"), discordtest.IntegerOption("duration", 7)}, settings.HookValueOn, "fadein_duration", "7"},
		{"only a number", "automix", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("beats", 16)}, "", "automix_beats", "16"},
		{"no options", "fadeonstop", nil, "", "", ""},
	}

	for _, c := range cases {
		arguments := settings.HookParseSettingArguments(specFor(t, c.key), c.options)
		if *arguments.HookHasValue() != (c.value != "") || *arguments.HookValue() != c.value {
			t.Errorf("%s: value = (%q, %v), want %q", c.name, *arguments.HookValue(), *arguments.HookHasValue(), c.value)
		}
		numberKey := ""
		if *arguments.HookNumber() != nil {
			numberKey = *(*arguments.HookNumber()).HookKey()
		}
		if numberKey != c.numberKey || *arguments.HookNumberValue() != c.numberValue {
			t.Errorf("%s: number = %q=%q, want %q=%q", c.name, numberKey, *arguments.HookNumberValue(), c.numberKey, c.numberValue)
		}
	}
}

func TestSettingCommandsWithoutArgumentsLeaveTheSettingAlone(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")
	settings.HookRegisterSettingMenus()

	for _, key := range dropdownSettingKeys {
		spec := specFor(t, key)
		for _, value := range settings.HookSettingMenuValues(spec) {
			seedSetting(t, key, value)

			_ = settings.HandleSetting(key)(session, textSettingInteraction(key))

			if got := storedSetting(t, key); got != value {
				t.Errorf("%s with no argument changed %q to %q", key, value, got)
			}
		}
	}
}

func TestSettingCommandsStoreTheGivenValue(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")
	settings.HookRegisterSettingMenus()

	for _, key := range dropdownSettingKeys {
		spec := specFor(t, key)
		for _, value := range settings.HookSettingMenuValues(spec) {
			_ = settings.HandleSetting(key)(session, textSettingInteraction(key, discordtest.StringOption(settingOptionName(key), value)))

			if got := storedSetting(t, key); got != value {
				t.Errorf("%s %s stored %q", key, value, got)
			}
		}
	}
}

func TestSettingCommandsRejectOutOfRangeNumbers(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")

	seedSetting(t, "fadein", settings.HookValueOff)
	seedSetting(t, "fadein_duration", "3")
	if err := settings.HandleSetting("fadein")(session, textSettingInteraction("fadein", discordtest.StringOption("setting", "99"))); err != nil {
		t.Errorf("a rejected number returned %v, want the rejection handled", err)
	}
	if got := storedSetting(t, "fadein"); got != settings.HookValueOff {
		t.Errorf("fadein 99 changed fade-in to %q", got)
	}
	if got := storedSetting(t, "fadein_duration"); got != "3" {
		t.Errorf("fadein 99 changed the duration to %q", got)
	}

	seedSetting(t, "automix", settings.HookValueOff)
	seedSetting(t, "automix_beats", "16")
	_ = settings.HandleSetting("automix")(session, textSettingInteraction("automix", discordtest.StringOption("setting", "on"), discordtest.IntegerOption("beats", 2)))
	if got := storedSetting(t, "automix"); got != settings.HookValueOff {
		t.Errorf("automix on 2 changed AutoMix to %q", got)
	}
	if got := storedSetting(t, "automix_beats"); got != "16" {
		t.Errorf("automix on 2 changed the beats to %q", got)
	}
}

func TestABareNumberTurnsTheSettingOnWithThatNumber(t *testing.T) {
	dbtest.Setup(t)
	session := discordtest.Session(t, "bot")

	seedSetting(t, "fadein", settings.HookValueOff)
	seedSetting(t, "fadein_duration", "3")
	_ = settings.HandleSetting("fadein")(session, textSettingInteraction("fadein", discordtest.StringOption("setting", "5")))

	if got := storedSetting(t, "fadein"); got != settings.HookValueOn {
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
	handle := settings.HandleSettingWithEmbed("fadein", func(guildID string) *discordgo.MessageEmbed {
		calls++
		return &discordgo.MessageEmbed{Title: guildID}
	})

	if err := handle(session, textSettingInteraction("fadein", discordtest.StringOption("setting", "on"))); err != nil {
		t.Fatalf("fadein on failed: %v", err)
	}
	if calls != 1 {
		t.Errorf("the custom embed was built %d times after a success, want 1", calls)
	}

	_ = handle(session, textSettingInteraction("fadein", discordtest.StringOption("setting", "99")))
	if calls != 1 {
		t.Errorf("the custom embed was built after a rejected number")
	}
}

func TestApplySettingArgumentsNamesTheSettingThatFailedToSave(t *testing.T) {
	dbtest.Setup(t)
	dbtest.CloseUntilCleanup(t)
	spec := specFor(t, "automix")

	if failed, err := settings.HookApplySettingArguments(checkGuildID, spec, settings.HookBuildSettingArguments(settings.HookSettingArgumentsFields{Value: settings.HookValueOn, HasValue: true})); err == nil || failed != spec {
		t.Errorf("state write failure = (%v, %v), want the automix spec and an error", failed, err)
	}

	beats := specFor(t, "automix_beats")
	if failed, err := settings.HookApplySettingArguments(checkGuildID, spec, settings.HookBuildSettingArguments(settings.HookSettingArgumentsFields{Number: beats, NumberValue: "16"})); err == nil || failed != beats {
		t.Errorf("number write failure = (%v, %v), want the automix_beats spec and an error", failed, err)
	}
}

func TestSettingCommandsReportSaveFailuresInsteadOfReturningThem(t *testing.T) {
	dbtest.Setup(t)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	dbtest.CloseUntilCleanup(t)
	ic := discordtest.SlashInteraction(checkGuildID, "sponsorblock", nil, discordtest.StringOption("setting", settings.HookValueOn))

	if err := settings.HandleSetting("sponsorblock")(session, ic); err != nil {
		t.Errorf("a save failure returned %v, want it reported in the reply", err)
	}
	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d replies, want one", len(sent))
	}
	want := "Could not save " + settings.HookSettingLabel(checkGuildID, "sponsorblock") + ": "
	if text := discordtest.EmbedText(discordtest.ReplyEmbed(t, &sent[0])); !strings.Contains(text, want) {
		t.Errorf("the reply %q does not contain %q", text, want)
	}
}

func TestNormalizationWriteFailureIsReturned(t *testing.T) {
	dbtest.Setup(t)
	dbtest.CloseUntilCleanup(t)

	if err := settings.HookWriteNormalization(checkGuildID, settings.HookValueOn); err == nil {
		t.Error("writing normalization to a closed database succeeded")
	}
}

func TestHandleSettingRejectsAnUnknownKey(t *testing.T) {
	if err := settings.HandleSetting("nope")(nil, textSettingInteraction("nope")); err == nil {
		t.Error("an undefined setting key was accepted")
	}
}
