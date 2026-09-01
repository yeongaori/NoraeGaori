package play

import (
	"fmt"
	"noraegaori/internal/discord"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

type skippedSong struct {
	Title     string
	URL       string
	Thumbnail string
	Error     string
}

func sendBatchedSkipNotice(s *discordgo.Session, guildID, channelID string, skipped []skippedSong) {
	if len(skipped) == 0 {
		return
	}

	lines := make([]string, 0, len(skipped))
	for _, sk := range skipped {
		var titlePart string
		if sk.URL != "" {
			titlePart = messages.FormatBoldMaskedLink(sk.Title, sk.URL)
		} else {
			titlePart = "**" + messages.EscapeMarkdown(sk.Title) + "**"
		}
		lines = append(lines, fmt.Sprintf("• %s — %s", titlePart, cleanErrorMessage(guildID, sk.Error)))
	}

	for _, chunk := range discord.SplitLinesIntoChunks(lines, 3900) {
		embed := &discordgo.MessageEmbed{
			Color:       messages.ColorError,
			Title:       messages.T(guildID).Titles.Unavailable,
			Description: chunk,
		}
		if _, err := s.ChannelMessageSendEmbed(channelID, embed); err != nil {
			logger.Errorf("Failed to send batched skip notification: %v", err)
		}
	}
}

func cleanErrorMessage(guildID, errorMsg string) string {
	errorLower := strings.ToLower(errorMsg)
	t := messages.T(guildID)
	errorMappings := map[string]string{
		"private video":                 t.Music.ErrorPrivateVideo,
		"deleted video":                 t.Music.ErrorDeletedVideo,
		"age-restricted":                t.Music.ErrorAgeRestricted,
		"age restricted":                t.Music.ErrorAgeRestricted,
		"not available in your country": t.Music.ErrorGeoRestricted,
		"geo":                           t.Music.ErrorGeoRestricted,
		"members-only":                  t.Music.ErrorMembersOnly,
		"members only":                  t.Music.ErrorMembersOnly,
		"premium":                       t.Music.ErrorPremiumOnly,
		"copyright":                     t.Music.ErrorCopyright,
		"blocked":                       t.Music.ErrorBlocked,
	}

	for pattern, message := range errorMappings {
		if strings.Contains(errorLower, pattern) {
			return message
		}
	}
	return t.Music.ErrorUnavailable
}
