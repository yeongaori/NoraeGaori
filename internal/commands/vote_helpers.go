package commands

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/pkg/logger"
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
	for _, chunk := range splitLinesIntoChunks(lines, 3900) {
		embed := &discordgo.MessageEmbed{
			Color:       messages.ColorError,
			Title:       messages.T(guildID).Titles.Unavailable,
			Description: chunk,
		}
		if _, err := s.ChannelMessageSendEmbed(channelID, embed); err != nil {
			logger.Errorf("[Playlist] Failed to send batched skip notification: %v", err)
		}
	}
}

func truncateToLimit(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - 3
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func splitLinesIntoChunks(lines []string, limit int) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range lines {
		line = truncateToLimit(line, limit)
		if current.Len() > 0 && current.Len()+1+len(line) > limit {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func cleanErrorMessage(guildID, errorMsg string) string {
	errorLower := strings.ToLower(errorMsg)
	t := messages.T(guildID)
	errorMappings := map[string]string{
		"private video":              t.Music.ErrorPrivateVideo,
		"deleted video":              t.Music.ErrorDeletedVideo,
		"age-restricted":             t.Music.ErrorAgeRestricted,
		"age restricted":             t.Music.ErrorAgeRestricted,
		"not available in your country": t.Music.ErrorGeoRestricted,
		"geo":                        t.Music.ErrorGeoRestricted,
		"members-only":               t.Music.ErrorMembersOnly,
		"members only":               t.Music.ErrorMembersOnly,
		"premium":                    t.Music.ErrorPremiumOnly,
		"copyright":                  t.Music.ErrorCopyright,
		"blocked":                    t.Music.ErrorBlocked,
	}
	for pattern, message := range errorMappings {
		if strings.Contains(errorLower, pattern) {
			return message
		}
	}
	return t.Music.ErrorUnavailable
}

type voteSession struct {
	votes          map[string]bool 
	requiredVotes  int
	startTime      time.Time
	cancelTimer    chan bool
	messageID      string
	channelID      string
	voiceChannelID string
}

var (
	skipVotes      = make(map[string]*voteSession) 
	skipVotesMutex sync.RWMutex
)

var (
	stopVotes      = make(map[string]*voteSession) 
	stopVotesMutex sync.RWMutex
)

const voteExpirationTime = 60 * time.Second

func startVoteWithReaction(s *discordgo.Session, guildID, title, emoji string, vs *voteSession, votesMap map[string]*voteSession, votesMutex *sync.RWMutex, onVotePassed func(currentVotes int)) {
	if err := s.MessageReactionAdd(vs.channelID, vs.messageID, emoji); err != nil {
		logger.Errorf("[VoteReaction] Failed to add reaction to message: %v", err)
	}

	voteDone := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if r.UserID == s.State.User.ID {
			return
		}
		if r.MessageID != vs.messageID {
			return
		}
		if r.Emoji.Name != emoji {
			return
		}

		member, err := s.State.Member(guildID, r.UserID)
		if err != nil || member.User.Bot {
			return
		}

		voiceState, err := s.State.VoiceState(guildID, r.UserID)
		if err != nil || voiceState.ChannelID != vs.voiceChannelID {
			return
		}

		votesMutex.Lock()
		if votesMap[guildID] != vs {
			votesMutex.Unlock()
			return
		}
		if vs.votes[r.UserID] {
			votesMutex.Unlock()
			return
		}
		vs.votes[r.UserID] = true
		currentVotes := len(vs.votes)
		requiredVotes := vs.requiredVotes

		if currentVotes >= requiredVotes {
			delete(votesMap, guildID)
			votesMutex.Unlock()

			onVotePassed(currentVotes)

			select {
			case voteDone <- true:
			default:
			}
		} else {
			votesMutex.Unlock()

			remaining := int(voteExpirationTime.Seconds()) - int(time.Since(vs.startTime).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			embed := messages.CreateWarningEmbed(title, "")
			messages.AddField(embed, messages.T(guildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			messages.SetFooter(embed, fmt.Sprintf(messages.T(guildID).Footers.VoteReaction, emoji, remaining))
			s.ChannelMessageEditEmbed(vs.channelID, vs.messageID, embed)
		}
	}

	removeHandler := s.AddHandler(reactionHandler)
	defer removeHandler()

	select {
	case <-vs.cancelTimer:
		logger.Debugf("[VoteReaction] %s vote cancelled for guild %s", title, guildID)
		s.MessageReactionsRemoveAll(vs.channelID, vs.messageID)
	case <-voteDone:
		logger.Debugf("[VoteReaction] %s vote passed via reaction for guild %s", title, guildID)
		s.MessageReactionsRemoveAll(vs.channelID, vs.messageID)
	case <-time.After(voteExpirationTime):
		logger.Debugf("[VoteReaction] %s vote expired for guild %s", title, guildID)
		votesMutex.Lock()
		delete(votesMap, guildID)
		votesMutex.Unlock()

		embed := messages.CreateWarningEmbed(title, messages.T(guildID).Votes.Expired)
		s.ChannelMessageEditEmbed(vs.channelID, vs.messageID, embed)
		s.MessageReactionsRemoveAll(vs.channelID, vs.messageID)
	}
}

func ClearSkipVotes(guildID string) {
	skipVotesMutex.Lock()
	defer skipVotesMutex.Unlock()

	if session := skipVotes[guildID]; session != nil {
		select {
		case session.cancelTimer <- true:
		default:
		}
	}

	delete(skipVotes, guildID)
}

func ClearStopVotes(guildID string) {
	stopVotesMutex.Lock()
	defer stopVotesMutex.Unlock()

	if session := stopVotes[guildID]; session != nil {
		select {
		case session.cancelTimer <- true:
		default:
		}
	}

	delete(stopVotes, guildID)
}
