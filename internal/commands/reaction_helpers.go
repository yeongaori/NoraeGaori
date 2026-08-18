package commands

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/pkg/logger"
)

func removeUserReaction(s *discordgo.Session, channelID, messageID, emoji, userID string) {
	if err := s.MessageReactionRemove(channelID, messageID, emoji, userID); err != nil {
		logger.Warnf("Failed to remove reaction %s from user %s: %v", emoji, userID, err)
	}
}

func clearPromptReactions(s *discordgo.Session, channelID, messageID string) {
	if err := s.MessageReactionsRemoveAll(channelID, messageID); err != nil {
		logger.Warnf("Failed to clear reactions on message %s: %v", messageID, err)
	}
}
