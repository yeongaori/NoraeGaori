package settings

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
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
		{"a toggle value", "sponsorblock", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "on")}, valueOn, "", ""},
		{"unknown text", "sponsorblock", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "maybe")}, "", "", ""},
		{"the repeat on alias", "repeat", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("mode", "on")}, valueRepeatAll, "", ""},
		{"an uppercase mode", "repeat", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("mode", "SINGLE")}, valueRepeatSingle, "", ""},
		{"a bare number", "fadein", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "5")}, valueOn, "fadein_duration", "5"},
		{"a state and a number", "fadein", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("setting", "on"), discordtest.IntegerOption("duration", 7)}, valueOn, "fadein_duration", "7"},
		{"only a number", "automix", []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("beats", 16)}, "", "automix_beats", "16"},
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
			_ = HandleSetting(key)(session, textSettingInteraction(key, discordtest.StringOption(settingOptionName(key), value)))

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
	if err := HandleSetting("fadein")(session, textSettingInteraction("fadein", discordtest.StringOption("setting", "99"))); err != nil {
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
	_ = HandleSetting("automix")(session, textSettingInteraction("automix", discordtest.StringOption("setting", "on"), discordtest.IntegerOption("beats", 2)))
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
	_ = HandleSetting("fadein")(session, textSettingInteraction("fadein", discordtest.StringOption("setting", "5")))

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
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	dbtest.CloseUntilCleanup(t)
	ic := discordtest.SlashInteraction(checkGuildID, "sponsorblock", nil, discordtest.StringOption("setting", valueOn))

	if err := HandleSetting("sponsorblock")(session, ic); err != nil {
		t.Errorf("a save failure returned %v, want it reported in the reply", err)
	}
	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d replies, want one", len(sent))
	}
	want := "Could not save " + settingLabel(checkGuildID, "sponsorblock") + ": "
	if text := discordtest.EmbedText(discordtest.ReplyEmbed(t, &sent[0])); !strings.Contains(text, want) {
		t.Errorf("the reply %q does not contain %q", text, want)
	}
}

func TestNormalizationWriteFailureIsReturned(t *testing.T) {
	dbtest.Setup(t)
	dbtest.CloseUntilCleanup(t)

	if err := writeNormalization(checkGuildID, valueOn); err == nil {
		t.Error("writing normalization to a closed database succeeded")
	}
}

func TestHandleSettingRejectsAnUnknownKey(t *testing.T) {
	if err := HandleSetting("nope")(nil, textSettingInteraction("nope")); err == nil {
		t.Error("an undefined setting key was accepted")
	}
}
