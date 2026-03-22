package commands

import (
	"fmt"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

// HandleForceRemove handles the forceremove command (admin only - bulk delete by user)
func HandleForceRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Admin.MentionTarget))
		return nil
	}
	target := options[0].StringValue()

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Admin.NoSongsToDelete))
		return nil
	}

	// Parse user mention (format: <@!?userID>)
	userMentionRegex := regexp.MustCompile(`^<@!?(\d+)>$`)
	matches := userMentionRegex.FindStringSubmatch(target)

	if len(matches) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Admin.InvalidMention))
		return nil
	}

	targetUserID := matches[1]

	// Get target user info
	targetUser, err := s.User(targetUserID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Admin.UserNotFound))
		return nil
	}

	// Find all songs by target user (excluding currently playing song at position 0)
	var userSongs []*queue.Song
	startIdx := 1 // Skip currently playing song
	if !q.Playing && !q.Loading {
		startIdx = 0 // If nothing playing, can remove from position 0
	}

	for idx := startIdx; idx < len(q.Songs); idx++ {
		if q.Songs[idx].RequestedByID == targetUserID {
			userSongs = append(userSongs, q.Songs[idx])
		}
	}

	if len(userSongs) == 0 {
		description := fmt.Sprintf(messages.T().Admin.UserNoSongs, messages.EscapeMarkdown(targetUser.Username))
		if startIdx == 1 {
			description += messages.T().Admin.ExcludingCurrent
		}
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, description))
		return nil
	}

	// Check if next song (position 1) is being removed
	isNextSongRemoved := false
	if len(q.Songs) > 1 {
		nextSong := q.Songs[1]
		for _, song := range userSongs {
			if song.ID == nextSong.ID {
				isNextSongRemoved = true
				break
			}
		}
	}

	// Get song IDs to remove
	songIDs := make([]int, len(userSongs))
	for i, song := range userSongs {
		songIDs[i] = song.ID
	}

	// Remove songs
	if err := queue.RemoveSongsByIDs(i.GuildID, songIDs); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Admin.RemoveFailed, err)))
		return err
	}

	// Cleanup pre-cache worker if next song was removed
	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T().Admin.DeleteCompleteTitle,
		fmt.Sprintf(messages.T().Admin.DeleteCompleteDesc, messages.EscapeMarkdown(targetUser.Username), len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}

// HandleMoveTrack handles the movetrack command (admin only - move song position)
func HandleMoveTrack(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Admin.EnterPositions))
		return nil
	}
	fromPos := int(options[0].IntValue()) - 1 // Convert to 0-based
	toPos := int(options[1].IntValue()) - 1

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleEmptyQueue, messages.DescEmptyQueue))
		return nil
	}

	// Validate positions
	if fromPos < 0 || toPos < 0 || fromPos >= len(q.Songs) || toPos >= len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Admin.EnterValidRange, len(q.Songs))))
		return nil
	}

	if fromPos == toPos {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Admin.SamePosition))
		return nil
	}

	// Prevent moving currently playing song
	if (fromPos == 0 || toPos == 0) && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Admin.CannotMovePlaying))
		return nil
	}

	// Get song title for confirmation message
	songTitle := q.Songs[fromPos].Title

	// Move song
	if err := queue.MoveSong(i.GuildID, fromPos, toPos); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Admin.MoveFailed, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T().Admin.MoveCompleteTitle,
		Description: fmt.Sprintf(messages.T().Admin.MoveCompleteDesc,
			messages.EscapeMarkdown(songTitle), fromPos+1, toPos+1),
	}
	RespondEmbed(s, i, embed)
	return nil
}

// HandleForceStop handles the forcestop command (admin only)
func HandleForceStop(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Admin.NoSongsTitle, messages.T().Admin.NoSongsDesc))
		return nil
	}

	// Force stop without voting
	if err := player.Stop(i.GuildID); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Admin.StopFailed, err)))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Admin.ForceStopTitle, messages.T().Admin.ForceStopDesc))
	return nil
}
