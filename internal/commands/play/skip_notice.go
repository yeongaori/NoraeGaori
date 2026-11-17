package play

import (
	"fmt"
	"noraegaori/internal/discord"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/youtube"
)

type skippedSong struct {
	Title       string
	URL         string
	Thumbnail   string
	Error       string
	Restriction youtube.Restriction
}

func sendSkipNotice(s *discordgo.Session, guildID, channelID string, skipped []skippedSong) {
	if len(skipped) == 0 {
		return
	}

	lines := make([]string, 0, len(skipped))
	for i := range skipped {
		sk := &skipped[i]
		var titlePart string
		if sk.URL != "" {
			titlePart = messages.FormatBoldMaskedLink(sk.Title, sk.URL)
		} else {
			titlePart = "**" + messages.EscapeMarkdown(sk.Title) + "**"
		}
		label := cleanErrorMessage(guildID, sk.Error)
		if sk.Restriction.IsDefinitive() {
			label = restrictionLabel(guildID, sk.Restriction)
		}
		lines = append(lines, fmt.Sprintf("• %s — %s", titlePart, label))
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

func restrictionLabel(guildID string, restriction youtube.Restriction) string {
	music := &messages.T(guildID).Music
	switch restriction {
	case youtube.RestrictionPrivateVideo:
		return music.ErrorPrivateVideo
	case youtube.RestrictionAgeRestricted:
		return music.ErrorAgeRestricted
	case youtube.RestrictionGeoRestricted:
		return music.ErrorGeoRestricted
	case youtube.RestrictionMembersOnly:
		return music.ErrorMembersOnly
	case youtube.RestrictionPremiumOnly:
		return music.ErrorPremiumOnly
	case youtube.RestrictionCopyright:
		return music.ErrorCopyright
	case youtube.RestrictionDeletedVideo:
		return music.ErrorDeletedVideo
	}
	return music.ErrorUnavailable
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
