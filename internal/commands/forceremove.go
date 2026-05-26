package commands

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func HandleForceRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.MentionTarget))
		return nil
	}
	target := options[0].StringValue()

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.NoSongsToDelete))
		return nil
	}

	if regexp.MustCompile(`^\d+$`).MatchString(target) {
		if len(target) >= 17 {
			return forceRemoveByUser(s, i, q, target)
		}
		position, _ := strconv.Atoi(target)
		return forceRemoveByPosition(s, i, q, position)
	}

	userMentionRegex := regexp.MustCompile(`^<@!?(\d+)>$`)
	matches := userMentionRegex.FindStringSubmatch(target)
	if len(matches) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Admin.InvalidMention))
		return nil
	}

	return forceRemoveByUser(s, i, q, matches[1])
}

func forceRemoveByUser(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, targetUserID string) error {
	if len(q.Songs) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.NoSongsToDelete))
		return nil
	}

	targetUser, err := s.User(targetUserID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.UserNotFound))
		return nil
	}

	var userSongs []*queue.Song
	startIdx := 1
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

	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Admin.DeleteCompleteTitle,
		fmt.Sprintf(messages.T(i.GuildID).Admin.DeleteCompleteDesc, messages.EscapeMarkdown(targetUser.Username), len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}

func forceRemoveByPosition(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, position int) error {
	if position < 1 || position > len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Admin.EnterValidRange, len(q.Songs))))
		return nil
	}

	if position == 1 && (q.Playing || q.Loading) {
		return forceRemoveCurrent(s, i, q)
	}

	songToRemove := q.Songs[position-1]
	isNextSongRemoved := len(q.Songs) > 1 && q.Songs[1].ID == songToRemove.ID

	if err := queue.RemoveSong(i.GuildID, position-1); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Admin.RemoveFailed, err)))
		return err
	}

	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Queue.SongRemoved, messages.EscapeMarkdown(songToRemove.Title)))
	RespondEmbed(s, i, embed)
	return nil
}

func forceRemoveCurrent(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue) error {
	DeferResponse(s, i)

	songTitle := q.Songs[0].Title

	err := player.Skip(s, i.GuildID)
	if err != nil && err != player.ErrQueueEmpty {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Admin.RemoveFailed, err)))
		return nil
	}

	if err == player.ErrQueueEmpty {
		if stopErr := player.Stop(i.GuildID); stopErr != nil {
			logger.Errorf("[ForceRemove] Failed to cleanup after queue empty: %v", stopErr)
		}
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Queue.SongRemoved, messages.EscapeMarkdown(songTitle)))
	UpdateResponseEmbed(s, i, embed)
	return nil
}
