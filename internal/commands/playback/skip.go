package playback

import (
	"fmt"
	"noraegaori/internal/discord"
	"noraegaori/internal/vote"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func skipResultEmbed(guildID string, song *queue.Song, queueEnded, isStaying bool) *discordgo.MessageEmbed {
	title := messages.T(guildID).Titles.Skipped
	template := messages.T(guildID).Descriptions.Skipped
	if queueEnded {
		title = messages.T(guildID).Music.PlaybackEndedTitle
		template = messages.T(guildID).Music.PlaybackEndedSkip
		if isStaying {
			template = messages.T(guildID).Music.PlaybackEndedSkipStay
		}
	}

	embed := messages.CreateSuccessEmbed(title, fmt.Sprintf(template, messages.FormatMaskedLink(song.Title, song.URL)))
	messages.SetThumbnail(embed, song.Thumbnail)
	return embed
}

func isStayingAfterSkip(guildID string, queueEnded bool) bool {
	return queueEnded && !player.ShouldLeaveVoice(guildID)
}

func stopAfterEmptyQueue(guildID string) {
	if err := player.Stop(guildID); err != nil {
		logger.Errorf("Failed to cleanup after queue empty: %v", err)
	}
}

func skipCurrentSong(s *discordgo.Session, guildID string, skipped *queue.Song) (*discordgo.MessageEmbed, bool, error) {
	err := player.Skip(s, guildID)
	if err != nil && err != player.ErrQueueEmpty {
		return nil, false, err
	}

	queueEnded := err == player.ErrQueueEmpty
	vote.CancelSuperseded(guildID, vote.KindSkip, queueEnded)
	return skipResultEmbed(guildID, skipped, queueEnded, isStayingAfterSkip(guildID, queueEnded)), queueEnded, nil
}

func applySkipVote(guildID string, skipped *queue.Song) func(*discordgo.Session, *vote.Session, vote.Tally) {
	return func(s *discordgo.Session, session *vote.Session, tally vote.Tally) {
		embed, queueEnded, skipErr := skipCurrentSong(s, guildID, skipped)
		if skipErr != nil {
			vote.RenderFailure(s, session, messages.T(guildID).Music.SkipFailedTitle, fmt.Sprintf(messages.T(guildID).Music.SkipFailedDesc, skipErr))
			return
		}

		vote.RenderResult(s, session, embed, tally)

		if queueEnded {
			stopAfterEmptyQueue(guildID)
		}
	}
}

func HandleSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	discord.DeferResponse(s, i)

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterVoiceChannel))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	requiredVotes, err := vote.RequiredInChannel(s, i.GuildID, voiceState.ChannelID, vote.ResolveWithFetch)
	if err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	skipped := q.Songs[0]

	if requiredVotes == 1 {
		return skipImmediately(s, i, skipped)
	}

	return vote.Start(s, i, vote.Request{
		Kind:           vote.KindSkip,
		Title:          messages.T(i.GuildID).Titles.SkipVote,
		Emoji:          "⏭",
		VoiceChannelID: voiceState.ChannelID,
		RequiredVotes:  requiredVotes,
		Target:         vote.Target{Scope: vote.ScopeCurrentSong},
		OnPassed:       applySkipVote(i.GuildID, skipped),
	})
}

func skipImmediately(s *discordgo.Session, i *discordgo.InteractionCreate, skipped *queue.Song) error {
	embed, queueEnded, err := skipCurrentSong(s, i.GuildID, skipped)
	if err != nil {
		logger.Errorf("Failed to skip: %v", err)
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err)))
		return nil
	}

	discord.UpdateResponseEmbed(s, i, embed)

	if queueEnded {
		stopAfterEmptyQueue(i.GuildID)
	}
	return nil
}
