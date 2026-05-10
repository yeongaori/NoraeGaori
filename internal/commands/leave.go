package commands

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
)

func HandleLeave(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if err := player.LeaveVoice(i.GuildID); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.LeaveFailedTitle, messages.T(i.GuildID).Voice.LeaveFailedDesc))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.LeaveSuccessTitle, messages.T(i.GuildID).Voice.LeaveSuccessDesc))
	return nil
}
