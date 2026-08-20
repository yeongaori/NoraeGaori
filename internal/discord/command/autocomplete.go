package command

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
)

const MaxAutocompleteChoices = 25

type AutocompleteRequest struct {
	GuildID     string
	UserID      string
	CommandName string
	Query       string
}

func HandleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	cmd, exists := lookupCommand(data.Name)
	if !exists || cmd.AutocompleteHandler == nil {
		respondAutocompleteChoices(s, i, nil)
		return
	}

	request := AutocompleteRequest{
		GuildID:     i.GuildID,
		UserID:      interactionUserID(i),
		CommandName: data.Name,
		Query:       focusedStringOption(data.Options),
	}

	choices := cmd.AutocompleteHandler(request)
	if len(choices) > MaxAutocompleteChoices {
		choices = choices[:MaxAutocompleteChoices]
	}

	respondAutocompleteChoices(s, i, choices)
}

func focusedStringOption(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	for _, option := range options {
		if option == nil || !option.Focused {
			continue
		}
		if option.Type != discordgo.ApplicationCommandOptionString {
			return ""
		}
		value, ok := option.Value.(string)
		if !ok {
			return ""
		}
		return value
	}
	return ""
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func respondAutocompleteChoices(s *discordgo.Session, i *discordgo.InteractionCreate, choices []*discordgo.ApplicationCommandOptionChoice) {
	if choices == nil {
		choices = []*discordgo.ApplicationCommandOptionChoice{}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		logger.Debugf("Failed to respond to autocomplete for %s: %v", i.ApplicationCommandData().Name, err)
	}
}
