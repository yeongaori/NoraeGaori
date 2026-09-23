package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
)

const (
	modalValueID = "settings_value"

	modalTitleLimit       = 45
	modalLabelLimit       = 45
	modalPlaceholderLimit = 100
)

func buildSettingModal(guildID string, target *panelTarget, spec *settingSpec) *discordgo.InteractionResponse {
	label := settingLabel(guildID, spec.key)
	current, _ := currentValue(guildID, spec)

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID:   discord.ComponentID(modalRoute, discord.ViewArgument(target.isAdmin), target.category, spec.key),
			Title:      discord.TruncateRunes(fmt.Sprintf(panelStrings(guildID).ModalTitle, label), modalTitleLimit),
			Components: []discordgo.MessageComponent{settingModalField(guildID, label, spec, current)},
		},
	}
}

func settingModalField(guildID, label string, spec *settingSpec, current string) discordgo.MessageComponent {
	if spec.kind == settingChoice {
		return discordgo.Label{
			Label: discord.TruncateRunes(label, modalLabelLimit),
			Component: discordgo.SelectMenu{
				CustomID:    modalValueID,
				Placeholder: discord.TruncateRunes(fmt.Sprintf(panelStrings(guildID).ChoicePlaceholder, label), modalPlaceholderLimit),
				Options:     choiceOptions(guildID, spec, current),
			},
		}
	}

	input := discordgo.TextInput{
		CustomID:    modalValueID,
		Label:       discord.TruncateRunes(label, modalLabelLimit),
		Style:       discordgo.TextInputShort,
		Placeholder: discord.TruncateRunes(settingHint(guildID, spec.key), modalPlaceholderLimit),
		Value:       current,
		Required:    spec.kind != settingText,
	}
	if spec.kind == settingText {
		input.MaxLength = int(spec.max)
	}
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{input}}
}

func choiceOptions(guildID string, spec *settingSpec, current string) []discordgo.SelectMenuOption {
	values := spec.options()
	options := make([]discordgo.SelectMenuOption, 0, min(len(values)+1, selectOptionLimit))
	if spec.hasDefault {
		options = append(options, discordgo.SelectMenuOption{
			Label:   panelStrings(guildID).DefaultOption,
			Value:   defaultChoiceValue,
			Default: current == "",
		})
	}
	for _, value := range values {
		if len(options) == selectOptionLimit {
			break
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:   discord.TruncateRunes(formatSettingValue(guildID, spec, value), optionLabelLimit),
			Value:   value,
			Default: value == current,
		})
	}
	return options
}

func findModalValue(components []discordgo.MessageComponent) (string, bool) {
	for _, component := range components {
		if value, found := modalValue(component); found {
			return value, true
		}
	}
	return "", false
}

func modalValue(component discordgo.MessageComponent) (string, bool) {
	switch typed := component.(type) {
	case *discordgo.ActionsRow:
		return findModalValue(typed.Components)
	case discordgo.ActionsRow:
		return findModalValue(typed.Components)
	case *discordgo.Label:
		return modalValue(typed.Component)
	case discordgo.Label:
		return modalValue(typed.Component)
	case *discordgo.TextInput:
		return textInputValue(typed)
	case discordgo.TextInput:
		return textInputValue(&typed)
	case *discordgo.SelectMenu:
		return selectMenuValue(typed)
	case discordgo.SelectMenu:
		return selectMenuValue(&typed)
	}
	return "", false
}

func textInputValue(input *discordgo.TextInput) (string, bool) {
	if input.CustomID != modalValueID {
		return "", false
	}
	return input.Value, true
}

func selectMenuValue(menu *discordgo.SelectMenu) (string, bool) {
	if menu.CustomID != modalValueID || len(menu.Values) == 0 {
		return "", false
	}
	return menu.Values[0], true
}
