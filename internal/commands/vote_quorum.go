package commands

import (
	"math"

	"github.com/bwmarrin/discordgo"
)

type memberResolution int

const (
	resolveFromCache memberResolution = iota
	resolveWithFetch
)

func resolveVoiceMember(s *discordgo.Session, guildID, userID string, attached *discordgo.Member, mode memberResolution) *discordgo.Member {
	if attached != nil && attached.User != nil {
		return attached
	}

	if member, err := s.State.Member(guildID, userID); err == nil && member.User != nil {
		return member
	}

	if mode != resolveWithFetch || s.Client == nil {
		return nil
	}

	if member, err := s.GuildMember(guildID, userID); err == nil && member.User != nil {
		_ = s.State.MemberAdd(member)
		return member
	}

	return nil
}

func requiredVotesInChannel(s *discordgo.Session, guildID, voiceChannelID string, mode memberResolution) (int, error) {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return 0, err
	}

	botID := ""
	if s.State.User != nil {
		botID = s.State.User.ID
	}

	humansPresent := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID != voiceChannelID || vs.UserID == botID {
			continue
		}

		member := resolveVoiceMember(s, guildID, vs.UserID, vs.Member, mode)
		if member == nil {
			if mode == resolveFromCache {
				humansPresent++
			}
			continue
		}

		if !member.User.Bot {
			humansPresent++
		}
	}

	required := int(math.Ceil(float64(humansPresent) * 0.5))
	if required < 1 {
		return 1, nil
	}
	return required, nil
}

func classifyVoter(s *discordgo.Session, guildID, voiceChannelID, userID string, adders []string) (voteBallot, bool) {
	votesAsAdder := isAdder(adders, userID)

	voiceState, err := s.State.VoiceState(guildID, userID)
	if err != nil || voiceState.ChannelID != voiceChannelID {
		if !votesAsAdder || isKnownBot(s, guildID, userID) {
			return voteBallot{}, false
		}
		return voteBallot{userID: userID, isAdder: true}, true
	}

	member := resolveVoiceMember(s, guildID, userID, voiceState.Member, resolveFromCache)
	if member == nil || member.User.Bot {
		return voteBallot{}, false
	}

	return voteBallot{userID: userID, countsFor: true, isAdder: votesAsAdder}, true
}

func isKnownBot(s *discordgo.Session, guildID, userID string) bool {
	if s.State != nil && s.State.User != nil && s.State.User.ID == userID {
		return true
	}

	member := resolveVoiceMember(s, guildID, userID, nil, resolveFromCache)
	return member != nil && member.User.Bot
}
