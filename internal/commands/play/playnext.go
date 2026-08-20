package play

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

func HandlePlayNext(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterQuery))
		return nil
	}
	query := options[0].StringValue()
	query = messages.StripMarkdown(query)

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel))
		return nil
	}

	discord.DeferResponse(s, i)

	searchEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Searching, fmt.Sprintf(messages.T(i.GuildID).Descriptions.Searching, query))
	discord.UpdateResponseEmbed(s, i, searchEmbed)

	logger.Debugf("Searching for: %s", query)
	song, err := youtube.Search(i.GuildID, query, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.SongNotFound))
		return err
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if err := ensureQueueForVoice(s, i, q, voiceState.ChannelID); err != nil {
		return err
	}

	queueSong := queueSongFrom(song)

	if err := queue.AddSong(i.GuildID, queueSong, 1); err != nil {
		if err.Error() == "song already in queue: "+song.Title {
			discord.UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Duplicate, messages.T(i.GuildID).Errors.DuplicateSong))
		} else {
			discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err)))
		}
		return err
	}

	embed := messages.CreateSongEmbed(
		i.GuildID,
		messages.ColorSuccess,
		messages.T(i.GuildID).Music.AddedAsNext,
		"",
		song.Title, song.URL, song.Uploader,
		song.Duration, i.Member.User.Username,
		song.Thumbnail,
	)

	discord.UpdateResponseEmbed(s, i, embed)

	player.ResumeOrStart(s, i.GuildID)

	return nil
}
