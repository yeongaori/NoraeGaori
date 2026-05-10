package commands

import (
	"fmt"
	"math"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

// HandleSkip skips the current song via majority vote; with 1-2 members skips immediately.
func HandleSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterVoiceChannel))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	voiceMembers := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == voiceState.ChannelID {
			member, err := s.State.Member(i.GuildID, vs.UserID)
			if err == nil && !member.User.Bot {
				voiceMembers++
			}
		}
	}

	requiredVotes := int(math.Ceil(float64(voiceMembers) * 0.5))

	// Solo or duo: skip without a vote.
	if requiredVotes == 1 {
		songTitle := q.Songs[0].Title
		songURL := q.Songs[0].URL
		songThumbnail := q.Songs[0].Thumbnail

		err := player.Skip(s, i.GuildID)
		if err != nil && err != player.ErrQueueEmpty {
			logger.Errorf("[Skip] Failed to skip: %v", err)
			embed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle,
				fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err))
			UpdateResponseEmbed(s, i, embed)
			return nil
		}

		if err == player.ErrQueueEmpty {
			embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.PlaybackEndedTitle,
				fmt.Sprintf(messages.T(i.GuildID).Music.PlaybackEndedSkip, messages.FormatMaskedLink(songTitle, songURL)))
			messages.SetThumbnail(embed, songThumbnail)
			UpdateResponseEmbed(s, i, embed)

			if stopErr := player.Stop(i.GuildID); stopErr != nil {
				logger.Errorf("[Skip] Failed to cleanup after queue empty: %v", stopErr)
			}
			return nil
		}

		embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Skipped,
			fmt.Sprintf(messages.T(i.GuildID).Descriptions.Skipped, messages.FormatMaskedLink(songTitle, songURL)))
		messages.SetThumbnail(embed, songThumbnail)
		UpdateResponseEmbed(s, i, embed)

		return nil
	}

	songTitle := q.Songs[0].Title
	songURL := q.Songs[0].URL
	songThumbnail := q.Songs[0].Thumbnail

	isNewSession := false
	skipVotesMutex.Lock()

	session := skipVotes[i.GuildID]
	if session == nil {
		session = &voteSession{
			votes:          make(map[string]bool),
			requiredVotes:  requiredVotes,
			startTime:      time.Now(),
			cancelTimer:    make(chan bool, 1),
			voiceChannelID: voiceState.ChannelID,
		}
		skipVotes[i.GuildID] = session
		isNewSession = true
	}
	skipVotesMutex.Unlock()

	skipVotesMutex.Lock()
	if session.votes[i.Member.User.ID] {
		skipVotesMutex.Unlock()
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.AlreadyVoted, messages.T(i.GuildID).Errors.AlreadyVoted))
		return nil
	}

	session.votes[i.Member.User.ID] = true
	currentVotes := len(session.votes)
	skipVotesMutex.Unlock()

	if currentVotes >= requiredVotes {
		select {
		case session.cancelTimer <- true:
		default:
		}

		skipVotesMutex.Lock()
		delete(skipVotes, i.GuildID)
		skipVotesMutex.Unlock()

		err := player.Skip(s, i.GuildID)
		if err != nil && err != player.ErrQueueEmpty {
			logger.Errorf("[Skip] Failed to skip: %v", err)
			embed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle,
				fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err))
			messages.AddField(embed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			UpdateResponseEmbed(s, i, embed)
			return nil
		}

		if err == player.ErrQueueEmpty {
			embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.PlaybackEndedTitle,
				fmt.Sprintf(messages.T(i.GuildID).Music.PlaybackEndedSkip, messages.FormatMaskedLink(songTitle, songURL)))
			messages.SetThumbnail(embed, songThumbnail)
			messages.AddField(embed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			UpdateResponseEmbed(s, i, embed)

			if stopErr := player.Stop(i.GuildID); stopErr != nil {
				logger.Errorf("[Skip] Failed to cleanup after queue empty: %v", stopErr)
			}
			return nil
		}

		embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Skipped,
			fmt.Sprintf(messages.T(i.GuildID).Descriptions.Skipped, messages.FormatMaskedLink(songTitle, songURL)))
		messages.SetThumbnail(embed, songThumbnail)
		messages.AddField(embed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		UpdateResponseEmbed(s, i, embed)
	} else {
		embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.SkipVote, "")
		messages.AddField(embed, messages.T(i.GuildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		messages.SetFooter(embed, fmt.Sprintf(messages.T(i.GuildID).Footers.VoteReaction, "⏭", int(voteExpirationTime.Seconds())))
		UpdateResponseEmbed(s, i, embed)

		if isNewSession {
			msg, msgErr := GetResponseMessage(s, i)
			if msgErr == nil && msg != nil {
				session.messageID = msg.ID
				session.channelID = msg.ChannelID

				go startVoteWithReaction(s, i.GuildID, messages.T(i.GuildID).Titles.SkipVote, "⏭", session, skipVotes, &skipVotesMutex, func(votes int) {
					skipErr := player.Skip(s, i.GuildID)
					if skipErr != nil && skipErr != player.ErrQueueEmpty {
						errEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, skipErr))
						s.ChannelMessageEditEmbed(session.channelID, session.messageID, errEmbed)
						return
					}
					if skipErr == player.ErrQueueEmpty {
						doneEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.PlaybackEndedTitle,
							fmt.Sprintf(messages.T(i.GuildID).Music.PlaybackEndedSkip, messages.FormatMaskedLink(songTitle, songURL)))
						messages.SetThumbnail(doneEmbed, songThumbnail)
						messages.AddField(doneEmbed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", votes, requiredVotes), true)
						s.ChannelMessageEditEmbed(session.channelID, session.messageID, doneEmbed)
						player.Stop(i.GuildID)
						return
					}
					skipEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Skipped,
						fmt.Sprintf(messages.T(i.GuildID).Descriptions.Skipped, messages.FormatMaskedLink(songTitle, songURL)))
					messages.SetThumbnail(skipEmbed, songThumbnail)
					messages.AddField(skipEmbed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", votes, requiredVotes), true)
					s.ChannelMessageEditEmbed(session.channelID, session.messageID, skipEmbed)
				})
			}
		}
	}

	return nil
}
