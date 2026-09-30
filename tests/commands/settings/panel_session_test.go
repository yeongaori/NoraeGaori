package settings_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord"
	"noraegaori/internal/guild"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
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

	for _, category := range *settings.HookSettingCategories {
		if got := settings.HookRequestedCategory(commandInteraction(category), true); got != category {
			t.Errorf("got %q, want %q", got, category)
		}
	}
}

func TestRequestedCategoryIsCaseInsensitiveAndTrimmed(t *testing.T) {
	dbtest.Setup(t)

	if got := settings.HookRequestedCategory(commandInteraction("  MIXING "), true); got != settings.HookCategoryMixing {
		t.Errorf("got %q, want %q", got, settings.HookCategoryMixing)
	}
}

func TestRequestedCategoryFallsBackWhenUnknownOrHidden(t *testing.T) {
	dbtest.Setup(t)

	if got := settings.HookRequestedCategory(commandInteraction("nonsense"), true); got != settings.HookDefaultCategory(true) {
		t.Errorf("an unknown category resolved to %q", got)
	}
	if got := settings.HookRequestedCategory(commandInteraction(settings.HookCategoryGeneral), false); got == settings.HookCategoryGeneral {
		t.Error("a non-admin was given the admin-only general category")
	}
	if got := settings.HookRequestedCategory(commandInteraction(""), true); got != settings.HookDefaultCategory(true) {
		t.Errorf("a missing option resolved to %q", got)
	}
}

func TestAPanelRendersTheCategoryItWasBuiltFor(t *testing.T) {
	dbtest.Setup(t)

	embed, components := settings.HookRenderPanel(checkGuildID, settings.HookBuildPanelTarget(settings.HookPanelTargetFields{IsAdmin: true, Category: settings.HookCategoryMixing}))

	if !strings.Contains(embed.Title, settings.HookCategoryLabel(checkGuildID, settings.HookCategoryMixing)) {
		t.Errorf("embed title %q does not name the mixing category", embed.Title)
	}
	if len(embed.Fields) != len(settings.HookSettingsInCategory(settings.HookCategoryMixing, true)) {
		t.Errorf("embed shows %d fields, want %d", len(embed.Fields), len(settings.HookSettingsInCategory(settings.HookCategoryMixing, true)))
	}
	if len(components) != 2 {
		t.Errorf("the rendered panel has %d rows, want 2", len(components))
	}
}

func TestTheSettingsPanelIsSentWithoutFetchingTheMessage(t *testing.T) {
	dbtest.Setup(t)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	ic := commandInteraction(settings.HookCategoryMixing)
	ic.ID, ic.AppID, ic.Token = "111", "app", "token"

	if err := settings.HandleSettingsPanel(session, ic); err != nil {
		t.Fatalf("HandleSettingsPanel returned %v", err)
	}

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want only the interaction reply", len(sent))
	}
	want := discord.ComponentID(settings.HookPickRoute, discord.ViewArgument(false), settings.HookCategoryMixing)
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 1, "components", 0, "custom_id"); got != want {
		t.Errorf("the picker routes to %v, want %q", got, want)
	}
}

func TestAMissingMemberCannotEditAdminSettings(t *testing.T) {
	if settings.HookCanEditAdminSettings(nil, checkGuildID, nil) {
		t.Error("a nil member was treated as an admin")
	}
	if settings.HookCanEditAdminSettings(nil, checkGuildID, &discordgo.Member{}) {
		t.Error("a member with no user was treated as an admin")
	}
}

func TestPrefixWritesReachTheDatabase(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "prefix")
	if err := settings.HookApplySetting(checkGuildID, spec, "!!"); err != nil {
		t.Fatalf("failed to set the prefix: %v", err)
	}

	stored, err := guild.GetPrefix(checkGuildID)
	if err != nil {
		t.Fatalf("failed to read the prefix: %v", err)
	}
	if stored != "!!" {
		t.Errorf("stored prefix is %q, want \"!!\"", stored)
	}

	if err := settings.HookApplySetting(checkGuildID, spec, ""); err != nil {
		t.Fatalf("failed to reset the prefix: %v", err)
	}
	if stored, _ := guild.GetPrefix(checkGuildID); stored != "" {
		t.Errorf("the prefix is %q after a reset, want empty", stored)
	}
}

func TestRepeatWritesMapOntoEveryQueueMode(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "repeat")

	for value, want := range map[string]int{
		settings.HookValueRepeatOff:    queue.RepeatOff,
		settings.HookValueRepeatAll:    queue.RepeatAll,
		settings.HookValueRepeatSingle: queue.RepeatSingle,
	} {
		if err := settings.HookApplySetting(checkGuildID, spec, value); err != nil {
			t.Fatalf("failed to set repeat to %q: %v", value, err)
		}

		mode, err := queue.GetRepeatMode(checkGuildID)
		if err != nil {
			t.Fatalf("failed to read the repeat mode: %v", err)
		}
		if mode != want {
			t.Errorf("repeat %q stored mode %d, want %d", value, mode, want)
		}
		if back, _ := settings.HookCurrentValue(checkGuildID, spec); back != value {
			t.Errorf("repeat %q read back as %q", value, back)
		}
	}
}

func TestChoiceValuesMustBeOnOffer(t *testing.T) {
	language := specFor(t, "language")
	repeat := specFor(t, "repeat")

	for _, check := range []struct {
		spec  *settings.HookSettingSpec
		input string
		want  string
		err   error
	}{
		{language, " KO ", "ko", nil},
		{language, settings.HookDefaultChoiceValue, settings.HookDefaultChoiceValue, nil},
		{language, "xx", "", *settings.HookErrUnknownValue},
		{repeat, "on", settings.HookValueRepeatAll, nil},
		{repeat, "Single", settings.HookValueRepeatSingle, nil},
		{repeat, settings.HookDefaultChoiceValue, "", *settings.HookErrUnknownValue},
	} {
		got, err := settings.HookNormalizeValue(check.spec, check.input)
		if got != check.want || !errors.Is(err, check.err) {
			t.Errorf("%s %q normalized to (%q, %v), want (%q, %v)", *check.spec.HookKey(), check.input, got, err, check.want, check.err)
		}
	}
}

func TestAutoMixBeatsAreStoredAsWholeBeats(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "automix_beats")

	if err := settings.HookApplySetting(checkGuildID, spec, "32"); err != nil {
		t.Fatalf("failed to set the beats: %v", err)
	}
	if beats, _ := queue.GetAutoMixBeats(checkGuildID); beats != 32 {
		t.Errorf("stored %d beats, want 32", beats)
	}

	if err := settings.HookApplySetting(checkGuildID, spec, "16.9"); !errors.Is(err, *settings.HookErrNotInteger) {
		t.Fatalf("fractional beats returned %v, want errNotInteger", err)
	}
	if beats, _ := queue.GetAutoMixBeats(checkGuildID); beats != 32 {
		t.Errorf("a rejected fractional value changed the stored beats to %d, want 32", beats)
	}
}

func TestValidationMessagesNameTheSettingAndItsBounds(t *testing.T) {
	label := func(key string) string { return settings.HookSettingLabel(checkGuildID, key) }

	for name, check := range map[string]struct {
		key  string
		err  error
		want string
	}{
		"a number out of range":   {"volume", *settings.HookErrOutOfRange, label("volume") + " must be between 0 and 1000."},
		"a word for a number":     {"volume", *settings.HookErrNotNumber, label("volume") + " must be a number."},
		"a fraction for a count":  {"automix_beats", *settings.HookErrNotInteger, label("automix_beats") + " must be a whole number."},
		"a prefix over the limit": {"prefix", *settings.HookErrTooLong, label("prefix") + " must be at most 5 characters."},
		"a failed save":           {"volume", errors.New("disk full"), "Could not save " + label("volume") + ": disk full"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := settings.HookValidationMessage(checkGuildID, specFor(t, check.key), check.err); got != check.want {
				t.Errorf("message = %q, want %q", got, check.want)
			}
		})
	}
}
