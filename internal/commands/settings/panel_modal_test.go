package settings

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/testutil/dbtest"
)

func TestModalInputIsReadFromAnActionsRow(t *testing.T) {
	data := discordgo.ModalSubmitInteractionData{
		CustomID: customID(modalPrefix, "volume", "token"),
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: inputPrefix + "volume", Value: "75"},
				},
			},
		},
	}

	value, found := modalInputValue(data, "volume")
	if !found {
		t.Fatal("the submitted value was not found")
	}
	if value != "75" {
		t.Errorf("got %q, want \"75\"", value)
	}
}

func TestModalInputIsReadFromALabelWrapper(t *testing.T) {
	data := discordgo.ModalSubmitInteractionData{
		CustomID: customID(modalPrefix, "prefix", "token"),
		Components: []discordgo.MessageComponent{
			&discordgo.Label{
				Label:     "Prefix",
				Component: &discordgo.TextInput{CustomID: inputPrefix + "prefix", Value: "?"},
			},
		},
	}

	value, found := modalInputValue(data, "prefix")
	if !found {
		t.Fatal("the submitted value was not found")
	}
	if value != "?" {
		t.Errorf("got %q, want \"?\"", value)
	}
}

func TestModalInputIgnoresAnotherSettingsField(t *testing.T) {
	data := discordgo.ModalSubmitInteractionData{
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: inputPrefix + "volume", Value: "75"},
				},
			},
		},
	}

	if _, found := modalInputValue(data, "prefix"); found {
		t.Error("a volume submission was read as a prefix submission")
	}
}

func TestModalCarriesTheCurrentValueAndTheSettingToken(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "volume")
	if err := applySetting(checkGuildID, spec, "120"); err != nil {
		t.Fatalf("failed to set volume: %v", err)
	}

	response := buildSettingModal(checkGuildID, spec, "token")

	if response.Type != discordgo.InteractionResponseModal {
		t.Errorf("got response type %v, want a modal", response.Type)
	}
	if response.Data.CustomID != customID(modalPrefix, "volume", "token") {
		t.Errorf("modal custom id is %q", response.Data.CustomID)
	}

	value, found := findTextInput(response.Data.Components, inputPrefix+"volume")
	if !found {
		t.Fatal("the modal holds no input for volume")
	}
	if value != "120" {
		t.Errorf("the modal is prefilled with %q, want \"120\"", value)
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
		response := buildSettingModal(checkGuildID, spec, "token")

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
