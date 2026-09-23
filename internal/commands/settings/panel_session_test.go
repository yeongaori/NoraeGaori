package settings

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/guild"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

func commandInteraction(category string) *discordgo.InteractionCreate {
	data := discordgo.ApplicationCommandInteractionData{Name: "settings"}
	if category != "" {
		data.Options = []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "category", Type: discordgo.ApplicationCommandOptionString, Value: category},
		}
	}

	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: checkGuildID,
			Data:    data,
		},
	}
}

func TestRequestedCategoryHonoursTheChosenOption(t *testing.T) {
	dbtest.Setup(t)

	for _, category := range settingCategories {
		if got := requestedCategory(commandInteraction(category), true); got != category {
			t.Errorf("got %q, want %q", got, category)
		}
	}
}

func TestRequestedCategoryIsCaseInsensitiveAndTrimmed(t *testing.T) {
	dbtest.Setup(t)

	if got := requestedCategory(commandInteraction("  MIXING "), true); got != categoryMixing {
		t.Errorf("got %q, want %q", got, categoryMixing)
	}
}

func TestRequestedCategoryFallsBackWhenUnknownOrHidden(t *testing.T) {
	dbtest.Setup(t)

	if got := requestedCategory(commandInteraction("nonsense"), true); got != defaultCategory(true) {
		t.Errorf("an unknown category resolved to %q", got)
	}
	if got := requestedCategory(commandInteraction(categoryGeneral), false); got == categoryGeneral {
		t.Error("a non-admin was given the admin-only general category")
	}
	if got := requestedCategory(commandInteraction(""), true); got != defaultCategory(true) {
		t.Errorf("a missing option resolved to %q", got)
	}
}

func TestAPanelRendersTheCategoryItWasBuiltFor(t *testing.T) {
	dbtest.Setup(t)

	embed, components := renderPanel(checkGuildID, &panelTarget{isAdmin: true, category: categoryMixing})

	if !strings.Contains(embed.Title, categoryLabel(checkGuildID, categoryMixing)) {
		t.Errorf("embed title %q does not name the mixing category", embed.Title)
	}
	if len(embed.Fields) != len(settingsInCategory(categoryMixing, true)) {
		t.Errorf("embed shows %d fields, want %d", len(embed.Fields), len(settingsInCategory(categoryMixing, true)))
	}
	if len(components) != 2 {
		t.Errorf("the rendered panel has %d rows, want 2", len(components))
	}
}

func TestTheSettingsPanelIsSentWithoutFetchingTheMessage(t *testing.T) {
	dbtest.Setup(t)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	ic := commandInteraction(categoryMixing)
	ic.ID, ic.AppID, ic.Token = "111", "app", "token"

	if err := HandleSettingsPanel(session, ic); err != nil {
		t.Fatalf("HandleSettingsPanel returned %v", err)
	}

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want only the interaction reply", len(sent))
	}
	want := discord.ComponentID(pickRoute, discord.ViewArgument(false), categoryMixing)
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 1, "components", 0, "custom_id"); got != want {
		t.Errorf("the picker routes to %v, want %q", got, want)
	}
}

func TestAMissingMemberCannotEditAdminSettings(t *testing.T) {
	if canEditAdminSettings(nil, checkGuildID, nil) {
		t.Error("a nil member was treated as an admin")
	}
	if canEditAdminSettings(nil, checkGuildID, &discordgo.Member{}) {
		t.Error("a member with no user was treated as an admin")
	}
}

func TestPrefixWritesReachTheDatabase(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "prefix")
	if err := applySetting(checkGuildID, spec, "!!"); err != nil {
		t.Fatalf("failed to set the prefix: %v", err)
	}

	stored, err := guild.GetPrefix(checkGuildID)
	if err != nil {
		t.Fatalf("failed to read the prefix: %v", err)
	}
	if stored != "!!" {
		t.Errorf("stored prefix is %q, want \"!!\"", stored)
	}

	if err := applySetting(checkGuildID, spec, ""); err != nil {
		t.Fatalf("failed to reset the prefix: %v", err)
	}
	if stored, _ := guild.GetPrefix(checkGuildID); stored != "" {
		t.Errorf("the prefix is %q after a reset, want empty", stored)
	}
}

func TestTheLanguageDefaultOptionClearsTheStoredLanguage(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "language")

	if err := applySetting(checkGuildID, spec, "ko"); err != nil {
		t.Fatalf("failed to set the language: %v", err)
	}
	if stored, _ := guild.GetLanguage(checkGuildID); stored != "ko" {
		t.Fatalf("stored language is %q, want \"ko\"", stored)
	}

	if err := applySetting(checkGuildID, spec, defaultChoiceValue); err != nil {
		t.Fatalf("failed to reset the language: %v", err)
	}
	if stored, _ := guild.GetLanguage(checkGuildID); stored != "" {
		t.Errorf("the language is %q after choosing the default, want empty", stored)
	}
}

func TestRepeatWritesMapOntoEveryQueueMode(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "repeat")

	for value, want := range map[string]int{
		valueRepeatOff:    queue.RepeatOff,
		valueRepeatAll:    queue.RepeatAll,
		valueRepeatSingle: queue.RepeatSingle,
	} {
		if err := applySetting(checkGuildID, spec, value); err != nil {
			t.Fatalf("failed to set repeat to %q: %v", value, err)
		}

		mode, err := queue.GetRepeatMode(checkGuildID)
		if err != nil {
			t.Fatalf("failed to read the repeat mode: %v", err)
		}
		if mode != want {
			t.Errorf("repeat %q stored mode %d, want %d", value, mode, want)
		}
		if back, _ := currentValue(checkGuildID, spec); back != value {
			t.Errorf("repeat %q read back as %q", value, back)
		}
	}
}

func TestChoiceValuesMustBeOnOffer(t *testing.T) {
	language := specFor(t, "language")
	repeat := specFor(t, "repeat")

	for _, check := range []struct {
		spec  *settingSpec
		input string
		want  string
		err   error
	}{
		{language, " KO ", "ko", nil},
		{language, defaultChoiceValue, defaultChoiceValue, nil},
		{language, "xx", "", errUnknownValue},
		{repeat, "on", valueRepeatAll, nil},
		{repeat, "Single", valueRepeatSingle, nil},
		{repeat, defaultChoiceValue, "", errUnknownValue},
	} {
		got, err := normalizeValue(check.spec, check.input)
		if got != check.want || !errors.Is(err, check.err) {
			t.Errorf("%s %q normalized to (%q, %v), want (%q, %v)", check.spec.key, check.input, got, err, check.want, check.err)
		}
	}
}

func TestAutoMixBeatsAreStoredAsWholeBeats(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "automix_beats")

	if err := applySetting(checkGuildID, spec, "32"); err != nil {
		t.Fatalf("failed to set the beats: %v", err)
	}
	if beats, _ := queue.GetAutoMixBeats(checkGuildID); beats != 32 {
		t.Errorf("stored %d beats, want 32", beats)
	}

	if err := applySetting(checkGuildID, spec, "16.9"); !errors.Is(err, errNotInteger) {
		t.Fatalf("fractional beats returned %v, want errNotInteger", err)
	}
	if beats, _ := queue.GetAutoMixBeats(checkGuildID); beats != 32 {
		t.Errorf("a rejected fractional value changed the stored beats to %d, want 32", beats)
	}
}

func TestValidationMessagesNameTheSettingAndItsBounds(t *testing.T) {
	volume := specFor(t, "volume")

	message := validationMessage(checkGuildID, volume, errOutOfRange)
	for _, want := range []string{settingLabel(checkGuildID, "volume"), "0", "1000"} {
		if !strings.Contains(message, want) {
			t.Errorf("the out-of-range message %q does not mention %q", message, want)
		}
	}

	if message := validationMessage(checkGuildID, volume, errNotNumber); !strings.Contains(message, settingLabel(checkGuildID, "volume")) {
		t.Errorf("the not-a-number message %q does not name the setting", message)
	}

	beats := specFor(t, "automix_beats")
	if message := validationMessage(checkGuildID, beats, errNotInteger); !strings.Contains(message, settingLabel(checkGuildID, "automix_beats")) {
		t.Errorf("the not-an-integer message %q does not name the setting", message)
	}

	prefix := specFor(t, "prefix")
	if message := validationMessage(checkGuildID, prefix, errTooLong); !strings.Contains(message, "5") {
		t.Errorf("the too-long message %q does not mention the 5 character limit", message)
	}
}
