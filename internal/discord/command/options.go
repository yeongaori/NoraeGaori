package command

import "github.com/bwmarrin/discordgo"

func FloatPointer(value float64) *float64 {
	return &value
}

func PositionOption(name, description string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionInteger,
		Name:        name,
		Description: description,
		Required:    true,
		MinValue:    FloatPointer(1),
	}
}

func QueryOption(description string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:         discordgo.ApplicationCommandOptionString,
		Name:         "query",
		Description:  description,
		Required:     true,
		Autocomplete: true,
	}
}

func VoiceChannelOption(description string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionChannel,
		Name:        "channel",
		Description: description,
		Required:    false,
		ChannelTypes: []discordgo.ChannelType{
			discordgo.ChannelTypeGuildVoice,
		},
	}
}
