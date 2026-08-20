package commands

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/pkg/logger"
)

var removeUserReaction = func(s *discordgo.Session, channelID, messageID, emoji, userID string) {
	if err := s.MessageReactionRemove(channelID, messageID, emoji, userID); err != nil {
		logger.Warnf("Failed to remove reaction %s from user %s: %v", emoji, userID, err)
	}
}

var addPromptReaction = func(s *discordgo.Session, channelID, messageID, emoji string) {
	if err := s.MessageReactionAdd(channelID, messageID, emoji); err != nil {
		logger.Errorf("Failed to add reaction %s to message %s: %v", emoji, messageID, err)
	}
}

var clearPromptReactions = func(s *discordgo.Session, channelID, messageID string) {
	if err := s.MessageReactionsRemoveAll(channelID, messageID); err != nil {
		logger.Warnf("Failed to clear reactions on message %s: %v", messageID, err)
	}
}
