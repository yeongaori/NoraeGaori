package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func handlePanelInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession) {
	if ic.GuildID != session.guildID {
		return
	}

	switch ic.Type {
	case discordgo.InteractionMessageComponent:
		handlePanelComponent(s, ic, session)
	case discordgo.InteractionModalSubmit:
		handlePanelModalSubmit(s, ic, session)
	}
}

func handlePanelComponent(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession) {
	data := ic.MessageComponentData()

	switch action, key := routeComponent(data.CustomID, session.token); action {
	case actionSwitchCategory:
		switchCategory(s, ic, session, data.Values)
	case actionPickSetting:
		pickSetting(s, ic, session, data.Values)
	case actionChooseValue:
		chooseValue(s, ic, session, key, data.Values)
	}
}

func switchCategory(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, values []string) {
	if len(values) == 0 || !isKnownCategory(values[0]) {
		return
	}
	session.setCategory(values[0])
	refreshPanel(s, ic, session)
}

func pickSetting(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, values []string) {
	if len(values) == 0 {
		return
	}

	spec, found := findSetting(values[0])
	if !found {
		return
	}
	if !allowedToEdit(s, ic, session, spec) {
		return
	}

	switch spec.kind {
	case settingText, settingNumber:
		openSettingModal(s, ic, session, spec)
	case settingChoice:
		session.setOpenSetting(spec.key)
		refreshPanel(s, ic, session)
	default:
		toggleSetting(s, ic, session, spec)
	}
}

func chooseValue(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, key string, values []string) {
	if len(values) == 0 {
		return
	}

	spec, found := findSetting(key)
	if !found || spec.kind != settingChoice {
		return
	}
	if !allowedToEdit(s, ic, session, spec) {
		return
	}

	session.setOpenSetting("")
	if values[0] == backValue {
		refreshPanel(s, ic, session)
		return
	}
	writeSetting(s, ic, session, spec, values[0])
}

func toggleSetting(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, spec settingSpec) {
	current, ok := currentValue(session.guildID, spec)
	if !ok {
		respondPanelError(s, ic, session, panelStrings(session.guildID).ReadFailed)
		return
	}
	writeSetting(s, ic, session, spec, nextValue(spec, current))
}

func openSettingModal(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, spec settingSpec) {
	if err := s.InteractionRespond(ic.Interaction, buildSettingModal(session.guildID, spec, session.token)); err != nil {
		logger.Errorf("Failed to open the settings modal for %s: %v", spec.key, err)
		respondPanelError(s, ic, session, fmt.Sprintf(panelStrings(session.guildID).ModalFailed, settingLabel(session.guildID, spec.key)))
	}
}

func handlePanelModalSubmit(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession) {
	data := ic.ModalSubmitData()

	action, key := routeModal(data.CustomID, session.token)
	if action != actionSubmitModal {
		return
	}

	spec, found := findSetting(key)
	if !found {
		return
	}
	if !allowedToEdit(s, ic, session, spec) {
		return
	}

	value, found := modalInputValue(data, key)
	if !found {
		return
	}

	writeSetting(s, ic, session, spec, value)
}

func writeSetting(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, spec settingSpec, value string) {
	if err := applySetting(session.guildID, spec, value); err != nil {
		respondPanelError(s, ic, session, validationMessage(session.guildID, spec, err))
		return
	}

	refreshPanel(s, ic, session)
}

func allowedToEdit(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, spec settingSpec) bool {
	if !spec.adminOnly {
		return true
	}
	if canEditAdminSettings(s, session.guildID, ic.Member) {
		return true
	}

	respondPanelError(s, ic, session, panelStrings(session.guildID).NotAdmin)
	return false
}

func refreshPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession) {
	embed, components := session.render()
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		logger.Errorf("Failed to refresh the settings panel: %v", err)
	}
}

func respondPanelError(s *discordgo.Session, ic *discordgo.InteractionCreate, session *panelSession, message string) {
	embed := messages.CreateErrorEmbed(messages.T(session.guildID).Titles.Error, message)
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		logger.Errorf("Failed to report a settings panel error: %v", err)
	}
}
