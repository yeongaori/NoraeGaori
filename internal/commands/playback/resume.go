package playback

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

func HandleResume(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	voiceChannelID, ok := discord.CallerVoiceChannel(s, i)
	if !ok {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Errors.NotInVoiceChannel)
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.NoSongsToResume)
		return nil
	}

	if q.Playing || q.Loading {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.AlreadyPlaying)
		return nil
	}

	if err = queue.SetPaused(i.GuildID, false); err != nil {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.PlaybackStartError)
		return err
	}

	if err = queue.UpdateVoiceChannel(i.GuildID, voiceChannelID); err != nil {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.PlaybackStartError)
		return err
	}

	q, err = queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.QueueNotFound)
		return nil
	}

	currentSong := q.Songs[0]

	if currentSong.IsLive {
		return resumeLiveSong(s, i, currentSong)
	}

	const seekLoadingThreshold = 120000
	if currentSong.SeekTime > seekLoadingThreshold {
		resumeFromLongSeek(s, i, currentSong)
		return nil
	}

	go player.Play(s, i.GuildID)

	successEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.ResumeStartTitle, messages.T(i.GuildID).Music.ResumeStartDesc)
	discord.RespondEmbed(s, i, successEmbed)
	return nil
}

func resumeLiveSong(s *discordgo.Session, i *discordgo.InteractionCreate, currentSong *queue.Song) error {
	logger.Debugf("Current song is a live stream, checking if it's still live")

	discord.DeferResponse(s, i)

	checkingEmbed := &discordgo.MessageEmbed{
		Color: messages.ColorWarning,
		Title: messages.T(i.GuildID).Music.LiveCheckingTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Music.LiveCheckingDesc,
			messages.FormatBoldMaskedLink(currentSong.Title, currentSong.URL)),
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: currentSong.Thumbnail},
	}
	_ = discord.UpdateResponseEmbed(s, i, checkingEmbed)

	isStillLive, err := youtube.CheckIfLive(currentSong.URL)
	if err != nil {
		logger.Warnf("Error checking live stream status: %v", err)

	} else if !isStillLive {
		logger.Infof("Live stream has ended, skipping to next song")

		if err := queue.RemoveSong(i.GuildID, 0); err != nil {
			discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Music.PlaybackStartError)
			return err
		}

		q, err := queue.GetQueue(i.GuildID, true)
		if err != nil || q == nil || len(q.Songs) == 0 {
			embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Music.LiveEndedTitle,
				messages.T(i.GuildID).Music.LiveEndedNoQueue)
			_ = discord.UpdateResponseEmbed(s, i, embed)
			return nil
		}

		skipEmbed := &discordgo.MessageEmbed{
			Color: messages.ColorWarning,
			Title: messages.T(i.GuildID).Music.LiveEndedTitle,
			Description: fmt.Sprintf(messages.T(i.GuildID).Music.LiveEndedSkip,
				messages.FormatBoldMaskedLink(currentSong.Title, currentSong.URL)),
		}
		_ = discord.UpdateResponseEmbed(s, i, skipEmbed)

		go player.Play(s, i.GuildID)
		return nil
	}

	logger.Debugf("Live stream is still live, proceeding with resume")

	successEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.LiveStartTitle, messages.T(i.GuildID).Music.LiveStartDesc)
	_ = discord.UpdateResponseEmbed(s, i, successEmbed)

	go player.Play(s, i.GuildID)
	return nil
}

func resumeFromLongSeek(s *discordgo.Session, i *discordgo.InteractionCreate, currentSong *queue.Song) {
	discord.DeferResponse(s, i)

	loadingEmbed := &discordgo.MessageEmbed{
		Color: messages.ColorWarning,
		Title: messages.T(i.GuildID).Titles.Loading,
		Description: fmt.Sprintf("%s\n\n%s",
			messages.FormatBoldMaskedLink(currentSong.Title, currentSong.URL), messages.T(i.GuildID).Descriptions.Loading),
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: currentSong.Thumbnail},
	}
	_ = discord.UpdateResponseEmbed(s, i, loadingEmbed)

	msg, err := discord.GetResponseMessage(s, i)
	if err == nil {
		player.SetLoadingMessage(i.GuildID, msg)
	}

	go player.Play(s, i.GuildID)
}
