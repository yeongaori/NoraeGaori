package discord

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
)

var RemoveUserReaction = func(s *discordgo.Session, channelID, messageID, emoji, userID string) {
	if userID != "@me" && !canManageMessages(s, channelID) {
		logger.Debugf("Missing Manage Messages in channel %s, keeping %s's reaction", channelID, userID)
		return
	}
	if err := s.MessageReactionRemove(channelID, messageID, emoji, userID); err != nil {
		logger.Warnf("Failed to remove reaction %s from user %s: %v", emoji, userID, err)
	}
}

var AddPromptReaction = func(s *discordgo.Session, channelID, messageID, emoji string) {
	if err := s.MessageReactionAdd(channelID, messageID, emoji); err != nil {
		logger.Errorf("Failed to add reaction %s to message %s: %v", emoji, messageID, err)
	}
}

var ClearPromptReactions = func(s *discordgo.Session, channelID, messageID, emoji string) {
	if canManageMessages(s, channelID) {
		err := s.MessageReactionsRemoveAll(channelID, messageID)
		if err == nil {
			return
		}
		logger.Warnf("Failed to clear reactions on message %s: %v", messageID, err)
	} else {
		logger.Debugf("Missing Manage Messages in channel %s, removing only the bot's own reaction", channelID)
	}
	RemoveUserReaction(s, channelID, messageID, emoji, "@me")
}

func canManageMessages(s *discordgo.Session, channelID string) bool {
	if s.State == nil || s.State.User == nil {
		return true
	}
	permissions, err := s.State.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return true
	}
	return permissions&discordgo.PermissionManageMessages != 0
}
