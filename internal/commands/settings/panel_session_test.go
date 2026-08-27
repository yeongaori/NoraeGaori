package settings

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/guild"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/dbtest"
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

func TestSessionRendersWhicheverCategoryIsCurrent(t *testing.T) {
	dbtest.Setup(t)

	session := &panelSession{guildID: checkGuildID, token: "token", panelAdmin: true, category: categoryPlayback}

	if got := session.currentCategory(); got != categoryPlayback {
		t.Fatalf("got %q, want %q", got, categoryPlayback)
	}

	session.setCategory(categoryMixing)
	embed, components := session.render()

	if !strings.Contains(embed.Title, categoryLabel(checkGuildID, categoryMixing)) {
		t.Errorf("embed title %q does not name the mixing category", embed.Title)
	}
	if len(embed.Fields) != len(settingsInCategory(categoryMixing, true)) {
		t.Errorf("embed shows %d fields, want %d", len(embed.Fields), len(settingsInCategory(categoryMixing, true)))
	}
	if len(components) == 0 {
		t.Error("the rendered panel has no components")
	}
}

func TestSwitchingCategoryClosesAnOpenValueList(t *testing.T) {
	dbtest.Setup(t)

	session := &panelSession{guildID: checkGuildID, token: "token", panelAdmin: true, category: categoryGeneral}
	session.setOpenSetting("language")

	if got := session.currentOpenSetting(); got != "language" {
		t.Fatalf("got %q, want \"language\"", got)
	}

	session.setCategory(categoryPlayback)

	if got := session.currentOpenSetting(); got != "" {
		t.Errorf("the language list stayed open as %q after a category switch", got)
	}

	_, components := session.render()
	if menu := rowMenu(t, components[1]); menu.CustomID != pickPrefix+"token" {
		t.Errorf("the second row is %q, want the picker", menu.CustomID)
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

func TestAutoMixBeatsAreStoredAsWholeBeats(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "automix_beats")

	if err := applySetting(checkGuildID, spec, "32"); err != nil {
		t.Fatalf("failed to set the beats: %v", err)
	}
	if beats, _ := queue.GetAutoMixBeats(checkGuildID); beats != 32 {
		t.Errorf("stored %d beats, want 32", beats)
	}

	if err := applySetting(checkGuildID, spec, "16.9"); err != nil {
		t.Fatalf("failed to set fractional beats: %v", err)
	}
	if beats, _ := queue.GetAutoMixBeats(checkGuildID); beats != 16 {
		t.Errorf("16.9 beats stored as %d, want 16", beats)
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

	prefix := specFor(t, "prefix")
	if message := validationMessage(checkGuildID, prefix, errTooLong); !strings.Contains(message, "5") {
		t.Errorf("the too-long message %q does not mention the 5 character limit", message)
	}
}
