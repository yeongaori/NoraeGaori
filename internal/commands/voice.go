package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

// HandleJoin handles the join command
func HandleJoin(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Check if user is in a voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Voice.EnterVoiceChannel))
		return nil
	}

	// Join voice channel
	_, err = player.JoinVoice(s, i.GuildID, voiceState.ChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Voice.JoinFailedTitle, messages.T().Voice.JoinFailedDesc))
		return err
	}

	// Get channel name
	channel, err := s.Channel(voiceState.ChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Voice.JoinSuccessTitle, messages.T().Voice.JoinSuccessDesc))
		return nil
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Voice.JoinSuccessTitle, fmt.Sprintf(messages.T().Voice.JoinSuccessChannel, messages.EscapeMarkdown(channel.Name))))
	return nil
}

// HandleLeave handles the leave command
func HandleLeave(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if err := player.LeaveVoice(i.GuildID); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Voice.LeaveFailedTitle, messages.T().Voice.LeaveFailedDesc))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Voice.LeaveSuccessTitle, messages.T().Voice.LeaveSuccessDesc))
	return nil
}

// HandleSwitchVC handles the switchvc command
func HandleSwitchVC(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	// Get target channel ID
	var targetChannelID string
	if len(options) > 0 {
		targetChannelID = options[0].ChannelValue(s).ID
	} else {
		// If no channel specified, use user's current voice channel
		voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
		if err != nil || voiceState.ChannelID == "" {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Voice.EnterVoiceOrSpecify))
			return nil
		}
		targetChannelID = voiceState.ChannelID
	}

	// Get current player state
	p := player.GetPlayer(i.GuildID)
	wasPlaying := p != nil && p.Playing

	// Get current queue to check if there are songs
	q, _ := queue.GetQueue(i.GuildID, false)
	hasSongs := q != nil && len(q.Songs) > 0

	// Leave current channel
	player.LeaveVoice(i.GuildID)

	// Join new channel
	_, err := player.JoinVoice(s, i.GuildID, targetChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Voice.SwitchFailedTitle, messages.T().Voice.SwitchFailedChannel))
		return err
	}

	// Update queue voice channel
	if hasSongs {
		if err := queue.UpdateVoiceChannel(i.GuildID, targetChannelID); err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Voice.SwitchFailedTitle, messages.T().Voice.SwitchFailedQueue))
			return err
		}
	}

	// Resume playback if was playing
	if wasPlaying && hasSongs {
		go player.Play(s, i.GuildID)
	}

	// Get channel name
	channel, err := s.Channel(targetChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Voice.SwitchSuccessTitle, messages.T().Voice.SwitchSuccessDesc))
		return nil
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Voice.SwitchSuccessTitle, fmt.Sprintf(messages.T().Voice.SwitchSuccessChannel, messages.EscapeMarkdown(channel.Name))))
	return nil
}
