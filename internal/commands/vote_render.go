package commands

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/pkg/logger"
)

var editVoteMessage = func(s *discordgo.Session, channelID, messageID string, embed *discordgo.MessageEmbed) {
	if channelID == "" || messageID == "" {
		return
	}

	if _, err := s.ChannelMessageEditEmbed(channelID, messageID, embed); err != nil {
		logger.Warnf("Failed to edit vote message %s: %v", messageID, err)
	}
}

func voteProgressEmbed(guildID, title, description, emoji string, startTime time.Time, tally voteTally) *discordgo.MessageEmbed {
	remaining := int(voteExpirationTime.Seconds()) - int(time.Since(startTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	embed := messages.CreateWarningEmbed(title, description)
	messages.AddField(embed, messages.T(guildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", tally.current, tally.required), true)
	addAdderField(embed, guildID, tally)
	messages.SetFooter(embed, fmt.Sprintf(messages.T(guildID).Footers.VoteReaction, emoji, remaining))
	return embed
}

func addAdderField(embed *discordgo.MessageEmbed, guildID string, tally voteTally) {
	if tally.adderTotal == 0 {
		return
	}

	messages.AddField(embed, messages.T(guildID).Fields.AdderVote, fmt.Sprintf("%d/%d", tally.adderVotes, tally.adderTotal), true)
}

func renderVoteProgress(s *discordgo.Session, session *voteSession, tally voteTally) {
	editVoteMessage(s, session.channelID, session.messageID, voteProgressEmbed(session.guildID, session.title, session.description, session.emoji, session.startTime, tally))
}

func renderVoteEnded(s *discordgo.Session, session *voteSession, reason voteEndReason) {
	editVoteMessage(s, session.channelID, session.messageID, messages.CreateWarningEmbed(session.title, voteEndDescription(session.guildID, reason)))
}

func renderVoteFailure(s *discordgo.Session, session *voteSession, title, description string) {
	editVoteMessage(s, session.channelID, session.messageID, messages.CreateErrorEmbed(title, description))
}

func renderVoteResult(s *discordgo.Session, session *voteSession, embed *discordgo.MessageEmbed, tally voteTally) {
	messages.AddField(embed, messages.T(session.guildID).Fields.VoteResult, fmt.Sprintf("%d/%d", tally.current, tally.required), true)
	if tally.byAdderConsent {
		messages.SetFooter(embed, messages.T(session.guildID).Votes.AllAddersAgreed)
	}
	editVoteMessage(s, session.channelID, session.messageID, embed)
}
