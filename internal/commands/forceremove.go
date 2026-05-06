package commands

import (
	"fmt"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

// HandleForceRemove bulk-deletes all queued songs requested by a target user.
func HandleForceRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.MentionTarget))
		return nil
	}
	target := options[0].StringValue()

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.NoSongsToDelete))
		return nil
	}

	userMentionRegex := regexp.MustCompile(`^<@!?(\d+)>$`)
	matches := userMentionRegex.FindStringSubmatch(target)

	if len(matches) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Admin.InvalidMention))
		return nil
	}

	targetUserID := matches[1]

	targetUser, err := s.User(targetUserID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.UserNotFound))
		return nil
	}

	var userSongs []*queue.Song
	startIdx := 1 // skip currently playing song
	if !q.Playing && !q.Loading {
		startIdx = 0
	}

	for idx := startIdx; idx < len(q.Songs); idx++ {
		if q.Songs[idx].RequestedByID == targetUserID {
			userSongs = append(userSongs, q.Songs[idx])
		}
	}

	if len(userSongs) == 0 {
		description := fmt.Sprintf(messages.T(i.GuildID).Admin.UserNoSongs, messages.EscapeMarkdown(targetUser.Username))
		if startIdx == 1 {
			description += messages.T(i.GuildID).Admin.ExcludingCurrent
		}
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, description))
		return nil
	}

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

	songIDs := make([]int, len(userSongs))
	for i, song := range userSongs {
		songIDs[i] = song.ID
	}

	if err := queue.RemoveSongsByIDs(i.GuildID, songIDs); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Admin.RemoveFailed, err)))
		return err
	}

	// Pre-cache worker is keyed to the next song; rebuild it if that song is gone.
	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Admin.DeleteCompleteTitle,
		fmt.Sprintf(messages.T(i.GuildID).Admin.DeleteCompleteDesc, messages.EscapeMarkdown(targetUser.Username), len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}
