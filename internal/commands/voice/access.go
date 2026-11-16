package voice

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

type voiceAccess int

const (
	accessAllowed voiceAccess = iota
	accessRefused
	accessNeedsConfirmation
)

const (
	voiceConfirmRoute = "voiceconfirm"
	leaveAction       = "leave"
	moveAction        = "move"
	cancelAction      = "cancel"
)

func checkVoiceAccess(s *discordgo.Session, i *discordgo.InteractionCreate, botChannelID string) voiceAccess {
	if botChannelID == "" {
		return accessAllowed
	}
	callerChannelID, _ := discord.CallerVoiceChannel(s, i)
	if callerChannelID == botChannelID || !hasOtherListeners(s, i.GuildID, botChannelID, i.Member.User.ID) {
		return accessAllowed
	}
	if discord.IsAdminMember(s, i.GuildID, i.Member) {
		return accessNeedsConfirmation
	}
	return accessRefused
}

func hasOtherListeners(s *discordgo.Session, guildID, channelID, callerID string) bool {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return true
	}
	for _, voiceState := range guild.VoiceStates {
		if voiceState.ChannelID == channelID && voiceState.UserID != s.State.User.ID && voiceState.UserID != callerID {
			return true
		}
	}
	return false
}

func guardVoiceAccess(s *discordgo.Session, i *discordgo.InteractionCreate, action, targetChannelID, botChannelID string) bool {
	switch checkVoiceAccess(s, i, botChannelID) {
	case accessRefused:
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Errors.MustBeInBotChannel)
		return false
	case accessNeedsConfirmation:
		askVoiceConfirmation(s, i, action, targetChannelID, botChannelID)
		return false
	}
	return true
}

func askVoiceConfirmation(s *discordgo.Session, i *discordgo.InteractionCreate, action, targetChannelID, botChannelID string) {
	voice := &messages.T(i.GuildID).Voice
	description := fmt.Sprintf(voice.ConfirmLeaveDesc, channelMention(botChannelID))
	label := voice.ConfirmLeaveButton
	style := discordgo.DangerButton
	if action == moveAction {
		description = fmt.Sprintf(voice.ConfirmMoveDesc, channelMention(botChannelID), channelMention(targetChannelID))
		label = voice.ConfirmMoveButton
		style = discordgo.PrimaryButton
	}

	callerID := i.Member.User.ID
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{
			Label:    label,
			Style:    style,
			CustomID: discord.ComponentID(voiceConfirmRoute, action, targetChannelID, botChannelID, callerID),
		},
		discordgo.Button{
			Label:    voice.ConfirmCancelButton,
			Style:    discordgo.SecondaryButton,
			CustomID: discord.ComponentID(voiceConfirmRoute, cancelAction, targetChannelID, botChannelID, callerID),
		},
	}}}

	if _, err := discord.SendPanel(s, i, messages.CreateWarningEmbed(voice.ConfirmTitle, description), components); err != nil {
		logger.Errorf("Failed to update the voice confirmation: %v", err)
	}
}

func channelMention(channelID string) string {
	return "<#" + channelID + ">"
}

func confirmVoiceAction(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	if len(arguments) != 4 {
		return
	}
	action, targetChannelID, promptChannelID, callerID := arguments[0], arguments[1], arguments[2], arguments[3]
	t := messages.T(ic.GuildID)

	if ic.Member == nil || ic.Member.User == nil || ic.Member.User.ID != callerID || !discord.IsAdminMember(s, ic.GuildID, ic.Member) {
		logger.Debugf("Voice confirmation in guild %s pressed by %s, not the requester", ic.GuildID, interactionUserID(ic))
		_ = discord.RespondEphemeral(s, ic, messages.CreateErrorEmbed(t.Titles.NoPermission, t.Errors.AdminOnly))
		return
	}

	if action == cancelAction {
		updateVoiceConfirmation(s, ic, messages.CreateInfoEmbed(t.Voice.ConfirmTitle, t.Voice.ConfirmCancelled))
		return
	}

	botChannelID := botVoiceChannelID(s, ic.GuildID)
	if botChannelID != promptChannelID {
		logger.Debugf("Voice confirmation for guild %s is stale, the bot moved from %s to %s", ic.GuildID, promptChannelID, botChannelID)
		updateVoiceConfirmation(s, ic, messages.CreateErrorEmbed(t.Voice.ConfirmTitle, t.Voice.ConfirmStale))
		return
	}

	updateVoiceConfirmation(s, ic, &discordgo.MessageEmbed{
		Color:       0xFFA500,
		Title:       t.Titles.Loading,
		Description: t.Descriptions.Loading,
	})
	switch action {
	case leaveAction:
		leaveVoice(s, ic)
	case moveAction:
		switchChannel(s, ic, targetChannelID)
	}
}

func updateVoiceConfirmation(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if err := discord.UpdatePanel(s, ic, embed, nil); err != nil {
		logger.Errorf("Failed to update the voice confirmation: %v", err)
	}
}

func interactionUserID(ic *discordgo.InteractionCreate) string {
	if ic.Member != nil && ic.Member.User != nil {
		return ic.Member.User.ID
	}
	return ""
}

func canConnect(s *discordgo.Session, userID, channelID string) bool {
	permissions, err := s.UserChannelPermissions(userID, channelID)
	return err == nil && permissions&discordgo.PermissionVoiceConnect != 0
}
