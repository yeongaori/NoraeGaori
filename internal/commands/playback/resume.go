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
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.NoSongsToResume))
		return nil
	}

	if q.Playing || q.Loading {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.AlreadyPlaying))
		return nil
	}

	if err := queue.SetPaused(i.GuildID, false); err != nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.PlaybackStartError))
		return err
	}

	if err := queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID); err != nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.PlaybackStartError))
		return err
	}

	q, err = queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueNotFound))
		return nil
	}

	currentSong := q.Songs[0]

	if currentSong.IsLive {
		logger.Debugf("Current song is a live stream, checking if it's still live")

		discord.DeferResponse(s, i)

		checkingEmbed := &discordgo.MessageEmbed{
			Color: messages.ColorWarning,
			Title: messages.T(i.GuildID).Music.LiveCheckingTitle,
			Description: fmt.Sprintf(messages.T(i.GuildID).Music.LiveCheckingDesc,
				messages.FormatBoldMaskedLink(currentSong.Title, currentSong.URL)),
			Thumbnail: &discordgo.MessageEmbedThumbnail{URL: currentSong.Thumbnail},
		}
		discord.UpdateResponseEmbed(s, i, checkingEmbed)

		isStillLive, err := youtube.CheckIfLive(currentSong.URL)
		if err != nil {
			logger.Warnf("Error checking live stream status: %v", err)

		} else if !isStillLive {
			logger.Infof("Live stream has ended, skipping to next song")

			if err := queue.RemoveSong(i.GuildID, 0); err != nil {
				discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.PlaybackStartError))
				return err
			}

			q, err = queue.GetQueue(i.GuildID, true)
			if err != nil || q == nil || len(q.Songs) == 0 {
				embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Music.LiveEndedTitle,
					messages.T(i.GuildID).Music.LiveEndedNoQueue)
				discord.UpdateResponseEmbed(s, i, embed)
				return nil
			}

			skipEmbed := &discordgo.MessageEmbed{
				Color: messages.ColorWarning,
				Title: messages.T(i.GuildID).Music.LiveEndedTitle,
				Description: fmt.Sprintf(messages.T(i.GuildID).Music.LiveEndedSkip,
					messages.FormatBoldMaskedLink(currentSong.Title, currentSong.URL)),
			}
			discord.UpdateResponseEmbed(s, i, skipEmbed)

			go player.Play(s, i.GuildID)
			return nil
		}

		logger.Debugf("Live stream is still live, proceeding with resume")

		successEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.LiveStartTitle, messages.T(i.GuildID).Music.LiveStartDesc)
		discord.UpdateResponseEmbed(s, i, successEmbed)

		go player.Play(s, i.GuildID)
		return nil
	}

	const seekLoadingThreshold = 120000
	if currentSong.SeekTime > seekLoadingThreshold {
		discord.DeferResponse(s, i)

		loadingEmbed := &discordgo.MessageEmbed{
			Color: messages.ColorWarning,
			Title: messages.T(i.GuildID).Titles.Loading,
			Description: fmt.Sprintf("%s\n\n%s",
				messages.FormatBoldMaskedLink(currentSong.Title, currentSong.URL), messages.T(i.GuildID).Descriptions.Loading),
			Thumbnail: &discordgo.MessageEmbedThumbnail{URL: currentSong.Thumbnail},
		}
		discord.UpdateResponseEmbed(s, i, loadingEmbed)

		msg, err := discord.GetResponseMessage(s, i)
		if err == nil {
			player.SetLoadingMessage(i.GuildID, msg)
		}

		go player.Play(s, i.GuildID)
		return nil
	}

	go player.Play(s, i.GuildID)

	successEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.ResumeStartTitle, messages.T(i.GuildID).Music.ResumeStartDesc)
	discord.RespondEmbed(s, i, successEmbed)
	return nil
}
