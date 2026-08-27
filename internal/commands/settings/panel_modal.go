package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
)

const (
	modalTitleLimit = 45
	modalLabelLimit = 45
)

func buildSettingModal(guildID string, spec settingSpec, token string) *discordgo.InteractionResponse {
	panel := panelStrings(guildID)
	label := settingLabel(guildID, spec.key)
	value, _ := currentValue(guildID, spec)

	input := discordgo.TextInput{
		CustomID:    inputPrefix + spec.key,
		Label:       discord.TruncateRunes(label, modalLabelLimit),
		Style:       discordgo.TextInputShort,
		Placeholder: settingHint(guildID, spec.key),
		Value:       value,
		Required:    spec.kind != settingText,
	}
	if spec.kind == settingText {
		input.MaxLength = int(spec.max)
	}

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: customID(modalPrefix, spec.key, token),
			Title:    discord.TruncateRunes(fmt.Sprintf(panel.ModalTitle, label), modalTitleLimit),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{input}},
			},
		},
	}
}

func modalInputValue(data discordgo.ModalSubmitInteractionData, key string) (string, bool) {
	return findTextInput(data.Components, inputPrefix+key)
}

func findTextInput(components []discordgo.MessageComponent, customID string) (string, bool) {
	for _, component := range components {
		switch typed := component.(type) {
		case *discordgo.TextInput:
			if typed.CustomID == customID {
				return typed.Value, true
			}
		case discordgo.TextInput:
			if typed.CustomID == customID {
				return typed.Value, true
			}
		case *discordgo.ActionsRow:
			if value, found := findTextInput(typed.Components, customID); found {
				return value, true
			}
		case discordgo.ActionsRow:
			if value, found := findTextInput(typed.Components, customID); found {
				return value, true
			}
		case *discordgo.Label:
			if value, found := findTextInput([]discordgo.MessageComponent{typed.Component}, customID); found {
				return value, true
			}
		case discordgo.Label:
			if value, found := findTextInput([]discordgo.MessageComponent{typed.Component}, customID); found {
				return value, true
			}
		}
	}
	return "", false
}
