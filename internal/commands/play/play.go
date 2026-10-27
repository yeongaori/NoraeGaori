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

func HandlePlay(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	query, voiceChannelID, ok := readPlayRequest(s, i)
	if !ok {
		return nil
	}

	if youtube.IsYouTubeURL(query) {
		analysis := youtube.AnalyzeYouTubeURL(query)
		logger.Debugf("URL analysis: type=%s, videoID=%s, playlistID=%s", analysis.Type, analysis.VideoID, analysis.PlaylistID)

		if analysis.Type == youtube.URLTypePurePlaylist {
			return handlePurePlaylist(s, i, query, voiceChannelID)
		}

		if analysis.Type == youtube.URLTypeVideoWithPlaylist {
			return handleVideoWithPlaylist(s, i, query, analysis, voiceChannelID)
		}
	}

	song, err := searchAndEnqueue(s, i, query, voiceChannelID, -1)
	if song == nil {
		return err
	}

	q, _ := queue.GetQueue(i.GuildID, true)

	isFirstSong := len(q.Songs) == 1

	var embed *discordgo.MessageEmbed
	if isFirstSong {
		embed = loadingSongEmbed(i.GuildID, song, i.Member.User.Username)
	} else {
		embed = youtubeSongEmbed(i.GuildID, messages.ColorSuccess, messages.T(i.GuildID).Titles.Added, "", song)
	}

	_ = discord.UpdateResponseEmbed(s, i, embed)

	if isFirstSong {
		msg, err := discord.GetResponseMessage(s, i)
		if err == nil {
			player.SetLoadingMessage(i.GuildID, msg)
		}
	}

	player.ResumeOrStart(s, i.GuildID)

	return nil
}

func loadingSongEmbed(guildID string, song *youtube.Song, requester string) *discordgo.MessageEmbed {
	uploaderValue := song.Uploader
	if uploaderValue == "" {
		uploaderValue = "-"
	}
	durationValue := song.Duration
	if durationValue == "" {
		durationValue = "-"
	}
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       messages.T(guildID).Titles.Loading,
		Description: fmt.Sprintf("%s\n\n%s", messages.FormatBoldMaskedLink(song.Title, song.URL), messages.T(guildID).Descriptions.Loading),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(guildID).Fields.Uploader, Value: uploaderValue, Inline: true},
			{Name: messages.T(guildID).Fields.Duration, Value: durationValue, Inline: true},
			{Name: messages.T(guildID).Fields.Requester, Value: requester, Inline: true},
		},
	}
	if song.Thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail}
	}
	return embed
}
