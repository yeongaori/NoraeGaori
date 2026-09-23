package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func switchCategory(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget) {
	category, isSelected := discord.SelectedValue(ic)
	if !isSelected || !isVisibleCategory(category, target.isAdmin) {
		return
	}
	refreshPanel(s, ic, &panelTarget{isAdmin: target.isAdmin, category: category})
}

func pickSetting(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget) {
	key, isSelected := discord.SelectedValue(ic)
	if !isSelected {
		return
	}

	spec, found := findSetting(key)
	if !found || !allowedToEdit(s, ic, spec) {
		return
	}

	if spec.kind == settingToggle {
		toggleSetting(s, ic, target, spec)
		return
	}
	openSettingModal(s, ic, target, spec)
}

func toggleSetting(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget, spec *settingSpec) {
	current, isRead := currentValue(ic.GuildID, spec)
	if !isRead {
		respondPanelError(s, ic, panelStrings(ic.GuildID).ReadFailed)
		return
	}
	writeSetting(s, ic, target, spec, nextValue(spec, current))
}

func openSettingModal(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget, spec *settingSpec) {
	if err := s.InteractionRespond(ic.Interaction, buildSettingModal(ic.GuildID, target, spec)); err != nil {
		logger.Errorf("Failed to open the settings modal for %s: %v", spec.key, err)
		respondPanelError(s, ic, fmt.Sprintf(panelStrings(ic.GuildID).ModalFailed, settingLabel(ic.GuildID, spec.key)))
	}
}

func submitSettingModal(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget) {
	components, isModal := discord.ModalComponents(ic)
	if !isModal {
		return
	}

	spec, found := findSetting(target.key)
	if !found || !allowedToEdit(s, ic, spec) {
		return
	}

	if value, found := findModalValue(components); found {
		writeSetting(s, ic, target, spec, value)
	}
}

func writeSetting(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget, spec *settingSpec, value string) {
	if err := applySetting(ic.GuildID, spec, value); err != nil {
		respondPanelError(s, ic, validationMessage(ic.GuildID, spec, err))
		return
	}
	refreshPanel(s, ic, target)
}

func allowedToEdit(s *discordgo.Session, ic *discordgo.InteractionCreate, spec *settingSpec) bool {
	if !spec.adminOnly || canEditAdminSettings(s, ic.GuildID, ic.Member) {
		return true
	}
	respondPanelError(s, ic, panelStrings(ic.GuildID).NotAdmin)
	return false
}

func refreshPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget) {
	embed, components := renderPanel(ic.GuildID, target)
	if err := discord.UpdateComponentMessage(s, ic, embed, components); err != nil {
		logger.Errorf("Failed to refresh the settings panel: %v", err)
	}
}

func respondPanelError(s *discordgo.Session, ic *discordgo.InteractionCreate, message string) {
	embed := messages.CreateErrorEmbed(messages.T(ic.GuildID).Titles.Error, message)
	if err := discord.RespondEphemeralEmbed(s, ic, embed); err != nil {
		logger.Errorf("Failed to report a settings panel error: %v", err)
	}
}
