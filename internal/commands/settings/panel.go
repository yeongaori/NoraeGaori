package settings

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
)

func canEditAdminSettings(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
	if member == nil || member.User == nil {
		return false
	}
	return config.IsAdmin(member.User.ID) || discord.IsGuildAdmin(s, guildID, member)
}

func requestedCategory(i *discordgo.InteractionCreate, isAdmin bool) string {
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		requested := strings.ToLower(strings.TrimSpace(options[0].StringValue()))
		if isVisibleCategory(requested, isAdmin) {
			return requested
		}
	}
	return defaultCategory(isAdmin)
}

func renderPanel(guildID string, target *panelTarget) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	view := newPanelView(guildID, target.category, target.isAdmin)
	return buildSettingsEmbed(view), buildSettingsComponents(view)
}

func HandleSettingsPanel(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	isAdmin := canEditAdminSettings(s, i.GuildID, i.Member)
	embed, components := renderPanel(i.GuildID, &panelTarget{isAdmin: isAdmin, category: requestedCategory(i, isAdmin)})

	if _, err := discord.SendEmbedWithComponents(s, i, embed, components); err != nil {
		logger.Errorf("Failed to send the settings panel: %v", err)
		return err
	}
	return nil
}
