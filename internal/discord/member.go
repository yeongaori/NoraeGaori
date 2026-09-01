package discord

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
)

func InteractionMember(i *discordgo.InteractionCreate) *discordgo.Member {
	if i == nil || i.Member == nil || i.Member.User == nil {
		return nil
	}
	return i.Member
}

func IsAdminMember(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
	if member == nil || member.User == nil {
		return false
	}
	return config.IsAdmin(member.User.ID) || IsGuildAdmin(s, guildID, member)
}
