package settings

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/dbtest"
)

func TestModalValuesAreFoundInEverySubmittedShape(t *testing.T) {
	for name, check := range map[string]struct {
		component discordgo.MessageComponent
		want      string
	}{
		"a text input in a row":       {textInput("75"), "75"},
		"a text input in a value row": {discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: modalValueID, Value: "80"}}}, "80"},
		"a text input in a label":     {&discordgo.Label{Component: &discordgo.TextInput{CustomID: modalValueID, Value: "?"}}, "?"},
		"a select in a label":         {choiceInput(valueRepeatSingle), valueRepeatSingle},
		"a select in a value label":   {discordgo.Label{Component: discordgo.SelectMenu{CustomID: modalValueID, Values: []string{"ko"}}}, "ko"},
	} {
		value, found := findModalValue([]discordgo.MessageComponent{check.component})
		if !found || value != check.want {
			t.Errorf("%s = (%q, %v), want (%q, true)", name, value, found, check.want)
		}
	}
}

func TestModalValuesIgnoreForeignFieldsAndEmptySelects(t *testing.T) {
	for name, component := range map[string]discordgo.MessageComponent{
		"another input":   &discordgo.TextInput{CustomID: "other", Value: "75"},
		"an empty select": &discordgo.Label{Component: &discordgo.SelectMenu{CustomID: modalValueID}},
		"an empty label":  &discordgo.Label{},
		"a button":        discordgo.Button{CustomID: modalValueID},
	} {
		if _, found := findModalValue([]discordgo.MessageComponent{component}); found {
			t.Errorf("a value was read from %s", name)
		}
	}
}

func TestModalCarriesTheCurrentValueAndItsRoute(t *testing.T) {
	dbtest.Setup(t)
	seedSetting(t, "volume", "120")

	response := buildSettingModal(checkGuildID, &panelTarget{isAdmin: true, category: categoryPlayback}, specFor(t, "volume"))

	if response.Type != discordgo.InteractionResponseModal {
		t.Errorf("got response type %v, want a modal", response.Type)
	}
	if want := discord.ComponentID(modalRoute, discord.ViewArgument(true), categoryPlayback, "volume"); response.Data.CustomID != want {
		t.Errorf("modal custom id is %q, want %q", response.Data.CustomID, want)
	}
	if value, found := findModalValue(response.Data.Components); !found || value != "120" {
		t.Errorf("the modal is prefilled with (%q, %v), want \"120\"", value, found)
	}
}

func TestTextModalAllowsAnEmptySubmissionButNumbersDoNot(t *testing.T) {
	dbtest.Setup(t)

	for _, check := range []struct {
		key      string
		required bool
	}{
		{"prefix", false},
		{"volume", true},
	} {
		spec := specFor(t, check.key)
		response := buildSettingModal(checkGuildID, &panelTarget{isAdmin: true, category: spec.category}, spec)

		row, ok := response.Data.Components[0].(discordgo.ActionsRow)
		if !ok {
			t.Fatalf("%s modal row is %T, want discordgo.ActionsRow", check.key, response.Data.Components[0])
		}
		input, ok := row.Components[0].(discordgo.TextInput)
		if !ok {
			t.Fatalf("%s modal component is %T, want discordgo.TextInput", check.key, row.Components[0])
		}
		if input.Required != check.required {
			t.Errorf("%s modal input required=%v, want %v", check.key, input.Required, check.required)
		}
	}
}

func choiceModalMenu(t *testing.T, key string) discordgo.SelectMenu {
	t.Helper()

	spec := specFor(t, key)
	response := buildSettingModal(checkGuildID, &panelTarget{isAdmin: true, category: spec.category}, spec)

	label, ok := response.Data.Components[0].(discordgo.Label)
	if !ok {
		t.Fatalf("%s modal holds %T, want discordgo.Label", key, response.Data.Components[0])
	}
	if label.Label != settingLabel(checkGuildID, key) {
		t.Errorf("%s modal label is %q", key, label.Label)
	}
	menu, ok := label.Component.(discordgo.SelectMenu)
	if !ok {
		t.Fatalf("%s modal label wraps %T, want discordgo.SelectMenu", key, label.Component)
	}
	if menu.CustomID != modalValueID {
		t.Errorf("%s modal select has custom id %q, want %q", key, menu.CustomID, modalValueID)
	}
	return menu
}

func TestChoiceModalsOfferASelectWithTheCurrentValue(t *testing.T) {
	dbtest.Setup(t)
	seedSetting(t, "repeat", valueRepeatSingle)

	repeat := choiceModalMenu(t, "repeat")
	if len(repeat.Options) != len(repeatValues) {
		t.Fatalf("the repeat select offers %d options, want %d", len(repeat.Options), len(repeatValues))
	}
	for index, value := range repeatValues {
		option := repeat.Options[index]
		if option.Value != value || option.Label != formatSettingValue(checkGuildID, specFor(t, "repeat"), value) {
			t.Errorf("repeat option %d = %+v, want %q", index, option, value)
		}
		if option.Default != (value == valueRepeatSingle) {
			t.Errorf("repeat option %q default=%v", value, option.Default)
		}
	}

	language := choiceModalMenu(t, "language")
	if language.Options[0].Value != defaultChoiceValue || !language.Options[0].Default {
		t.Errorf("the first language option is %+v, want the preselected default", language.Options[0])
	}
	for _, code := range messages.AvailableLocales() {
		found := false
		for _, option := range language.Options {
			found = found || option.Value == code
		}
		if !found {
			t.Errorf("locale %q is missing from the language select", code)
		}
	}
}
