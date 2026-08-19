package commands

import (
	"fmt"
	"math"
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
			logger.Errorf("Failed to send batched skip notification: %v", err)
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

func requiredVotesInChannel(s *discordgo.Session, guildID, voiceChannelID string) (int, error) {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return 0, err
	}

	humansPresent := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID != voiceChannelID {
			continue
		}
		if member, err := s.State.Member(guildID, vs.UserID); err == nil && !member.User.Bot {
			humansPresent++
		}
	}

	required := int(math.Ceil(float64(humansPresent) * 0.5))
	if required < 1 {
		return 1, nil
	}
	return required, nil
}

type voteSession struct {
	votes          map[string]bool
	requiredVotes  int
	startTime      time.Time
	cancelTimer    chan bool
	messageID      string
	channelID      string
	voiceChannelID string
	resolved       bool
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

func activeVoteFor(votesMap map[string]*voteSession, votesMutex *sync.RWMutex, guildID string) *voteSession {
	votesMutex.RLock()
	defer votesMutex.RUnlock()
	return votesMap[guildID]
}

func voteMessageURL(guildID, channelID, messageID string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func claimVoteSession(votesMap map[string]*voteSession, votesMutex *sync.RWMutex, guildID string, session *voteSession) *voteSession {
	votesMutex.Lock()
	defer votesMutex.Unlock()

	if existing := votesMap[guildID]; existing != nil {
		return existing
	}
	votesMap[guildID] = session
	return nil
}

func releaseVoteSession(votesMap map[string]*voteSession, votesMutex *sync.RWMutex, guildID string, session *voteSession) {
	votesMutex.Lock()
	defer votesMutex.Unlock()

	if votesMap[guildID] == session {
		delete(votesMap, guildID)
	}
}

func replyVoteInProgress(s *discordgo.Session, i *discordgo.InteractionCreate, title string, vs *voteSession) {
	description := messages.T(i.GuildID).Votes.InProgress
	if vs.messageID != "" && vs.channelID != "" {
		description = fmt.Sprintf("%s\n%s", description, voteMessageURL(i.GuildID, vs.channelID, vs.messageID))
	}
	UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(title, description))
}

func (vs *voteSession) castVote(userID string) (int, bool) {
	if vs.votes[userID] {
		return len(vs.votes), false
	}
	vs.votes[userID] = true
	return len(vs.votes), true
}

func (vs *voteSession) withdrawVote(userID string) (int, bool) {
	if !vs.votes[userID] {
		return len(vs.votes), false
	}
	delete(vs.votes, userID)
	return len(vs.votes), true
}

func renderVoteProgress(s *discordgo.Session, guildID, title, emoji string, vs *voteSession, currentVotes, requiredVotes int) {
	remaining := int(voteExpirationTime.Seconds()) - int(time.Since(vs.startTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	embed := messages.CreateWarningEmbed(title, "")
	messages.AddField(embed, messages.T(guildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
	messages.SetFooter(embed, fmt.Sprintf(messages.T(guildID).Footers.VoteReaction, emoji, remaining))
	s.ChannelMessageEditEmbed(vs.channelID, vs.messageID, embed)
}

type voteReaction struct {
	guildID      string
	title        string
	emoji        string
	session      *voteSession
	votesMap     map[string]*voteSession
	votesMutex   *sync.RWMutex
	onVotePassed func(currentVotes int)
	voteDone     chan bool
}

func (v *voteReaction) targetsThisVote(s *discordgo.Session, userID, messageID, emojiName string) bool {
	return userID != s.State.User.ID && messageID == v.session.messageID && emojiName == v.emoji
}

func (v *voteReaction) voterIsEligible(s *discordgo.Session, userID string) bool {
	member, err := s.State.Member(v.guildID, userID)
	if err != nil || member.User.Bot {
		return false
	}

	voiceState, err := s.State.VoiceState(v.guildID, userID)
	return err == nil && voiceState.ChannelID == v.session.voiceChannelID
}

func (v *voteReaction) rejectReaction(s *discordgo.Session, userID string) {
	removeUserReaction(s, v.session.channelID, v.session.messageID, v.emoji, userID)
}

func (v *voteReaction) onReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if !v.targetsThisVote(s, r.UserID, r.MessageID, r.Emoji.Name) {
		return
	}

	if !v.voterIsEligible(s, r.UserID) {
		v.rejectReaction(s, r.UserID)
		return
	}

	v.votesMutex.Lock()
	if v.votesMap[v.guildID] != v.session {
		v.votesMutex.Unlock()
		v.rejectReaction(s, r.UserID)
		return
	}

	currentVotes, counted := v.session.castVote(r.UserID)
	if !counted {
		v.votesMutex.Unlock()
		return
	}
	requiredVotes := v.session.requiredVotes

	if currentVotes < requiredVotes {
		v.votesMutex.Unlock()
		renderVoteProgress(s, v.guildID, v.title, v.emoji, v.session, currentVotes, requiredVotes)
		return
	}

	v.session.resolved = true
	delete(v.votesMap, v.guildID)
	v.votesMutex.Unlock()

	v.onVotePassed(currentVotes)

	select {
	case v.voteDone <- true:
	default:
	}
}

func (v *voteReaction) onReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	if !v.targetsThisVote(s, r.UserID, r.MessageID, r.Emoji.Name) {
		return
	}

	v.votesMutex.Lock()
	if v.votesMap[v.guildID] != v.session {
		v.votesMutex.Unlock()
		return
	}

	currentVotes, withdrawn := v.session.withdrawVote(r.UserID)
	if !withdrawn {
		v.votesMutex.Unlock()
		return
	}
	requiredVotes := v.session.requiredVotes
	v.votesMutex.Unlock()

	renderVoteProgress(s, v.guildID, v.title, v.emoji, v.session, currentVotes, requiredVotes)
}

func (v *voteReaction) awaitOutcome(s *discordgo.Session) {
	select {
	case <-v.session.cancelTimer:
		logger.Debugf("%s vote cancelled for guild %s", v.title, v.guildID)
		v.votesMutex.RLock()
		resolved := v.session.resolved
		v.votesMutex.RUnlock()
		if !resolved {
			s.ChannelMessageEditEmbed(v.session.channelID, v.session.messageID, messages.CreateWarningEmbed(v.title, messages.T(v.guildID).Votes.Cancelled))
		}
	case <-v.voteDone:
		logger.Debugf("%s vote passed via reaction for guild %s", v.title, v.guildID)
	case <-time.After(voteExpirationTime):
		logger.Debugf("%s vote expired for guild %s", v.title, v.guildID)
		v.votesMutex.Lock()
		delete(v.votesMap, v.guildID)
		v.votesMutex.Unlock()

		s.ChannelMessageEditEmbed(v.session.channelID, v.session.messageID, messages.CreateWarningEmbed(v.title, messages.T(v.guildID).Votes.Expired))
	}

	clearPromptReactions(s, v.session.channelID, v.session.messageID)
}

func startVoteWithReaction(s *discordgo.Session, guildID, title, emoji string, vs *voteSession, votesMap map[string]*voteSession, votesMutex *sync.RWMutex, onVotePassed func(currentVotes int)) {
	vote := &voteReaction{
		guildID:      guildID,
		title:        title,
		emoji:        emoji,
		session:      vs,
		votesMap:     votesMap,
		votesMutex:   votesMutex,
		onVotePassed: onVotePassed,
		voteDone:     make(chan bool, 1),
	}

	removeAddHandler := s.AddHandler(vote.onReactionAdd)
	defer removeAddHandler()
	removeRemoveHandler := s.AddHandler(vote.onReactionRemove)
	defer removeRemoveHandler()

	if err := s.MessageReactionAdd(vs.channelID, vs.messageID, emoji); err != nil {
		logger.Errorf("Failed to add reaction to message: %v", err)
	}

	vote.awaitOutcome(s)
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
