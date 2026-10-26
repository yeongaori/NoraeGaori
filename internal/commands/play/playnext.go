package play

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
)

func HandlePlayNext(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	query, voiceChannelID, ok := readPlayRequest(s, i)
	if !ok {
		return nil
	}

	song, err := searchAndEnqueue(s, i, query, voiceChannelID, 1)
	if song == nil {
		return err
	}

	embed := youtubeSongEmbed(i.GuildID, messages.ColorSuccess, messages.T(i.GuildID).Music.AddedAsNext, "", song)

	_ = discord.UpdateResponseEmbed(s, i, embed)

	player.ResumeOrStart(s, i.GuildID)

	return nil
}
