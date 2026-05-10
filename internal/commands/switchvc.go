package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleSwitchVC(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var targetChannelID string
	if len(options) > 0 {
		targetChannelID = options[0].ChannelValue(s).ID
	} else {
		voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
		if err != nil || voiceState.ChannelID == "" {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Voice.EnterVoiceOrSpecify))
			return nil
		}
		targetChannelID = voiceState.ChannelID
	}

	p := player.GetPlayer(i.GuildID)
	wasPlaying := p != nil && p.Playing

	q, _ := queue.GetQueue(i.GuildID, false)
	hasSongs := q != nil && len(q.Songs) > 0

	player.LeaveVoice(i.GuildID)

	_, err := player.JoinVoice(s, i.GuildID, targetChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.SwitchFailedTitle, messages.T(i.GuildID).Voice.SwitchFailedChannel))
		return err
	}

	if hasSongs {
		if err := queue.UpdateVoiceChannel(i.GuildID, targetChannelID); err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.SwitchFailedTitle, messages.T(i.GuildID).Voice.SwitchFailedQueue))
			return err
		}
	}

	if wasPlaying && hasSongs {
		go player.Play(s, i.GuildID)
	}

	channel, err := s.Channel(targetChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.SwitchSuccessTitle, messages.T(i.GuildID).Voice.SwitchSuccessDesc))
		return nil
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.SwitchSuccessTitle, fmt.Sprintf(messages.T(i.GuildID).Voice.SwitchSuccessChannel, messages.EscapeMarkdown(channel.Name))))
	return nil
}
