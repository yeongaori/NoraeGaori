package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleSkipTo(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.SkipToEnterPosition))
		return nil
	}
	position := int(options[0].IntValue())

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterVoiceChannel))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
		return nil
	}

	if position < 1 || position > len(q.Songs) {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	if position == 1 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.SkipToCurrent))
		return nil
	}

	targetSong := q.Songs[position-1]

	
	if err := queue.SkipToPosition(i.GuildID, position-1); err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToFailed, err)))
		return err
	}

	p := player.GetPlayer(i.GuildID)
	if p.Playing || p.Loading {
		if err := player.SkipTo(s, i.GuildID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
				fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToFailed, err)))
			return err
		}
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(i.GuildID).Queue.SkipToCompleteTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToCompleteDesc,
			messages.FormatBoldMaskedLink(targetSong.Title, targetSong.URL)),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Queue.SkipToSkippedSongs, Value: fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToSongsCount, position-1), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: messages.EscapeMarkdown(targetSong.RequestedByTag), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: targetSong.Thumbnail},
	}

	UpdateResponseEmbed(s, i, embed)
	return nil
}
