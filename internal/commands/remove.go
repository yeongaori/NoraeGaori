package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

// HandleRemove accepts a single index ("3"), a range ("1-5"), or "ALL".
func HandleRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.EnterPosition))
		return nil
	}
	positionStr := options[0].StringValue()

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
		return nil
	}

	if strings.ToUpper(positionStr) == "ALL" {
		return handleRemoveAll(s, i, q)
	}

	if strings.Contains(positionStr, "-") {
		return handleRemoveRange(s, i, q, positionStr)
	}

	return handleRemoveSingle(s, i, q, positionStr)
}

func handleRemoveAll(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue) error {
	userID := i.Member.User.ID

	var userSongs []*queue.Song
	startIdx := 0
	if q.Playing || q.Loading {
		startIdx = 1 // skip currently playing song
	}

	for idx := startIdx; idx < len(q.Songs); idx++ {
		if q.Songs[idx].RequestedByID == userID {
			userSongs = append(userSongs, q.Songs[idx])
		}
	}

	if len(userSongs) == 0 {
		description := messages.T(i.GuildID).Queue.NoUserSongs

		if (q.Playing || q.Loading) && len(q.Songs) > 0 && q.Songs[0].RequestedByID == userID {
			description = messages.T(i.GuildID).Queue.OnlyCurrentSong
		}

		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Queue.NoSongsToRemoveTitle, description))
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
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Queue.RemoveFailed, err)))
		return err
	}

	// Pre-cache worker is keyed to the next song; rebuild it if that song is gone.
	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Queue.SongsRemovedAll, i.Member.User.Username, len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}

func handleRemoveRange(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, rangeStr string) error {
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.InvalidRange))
		return nil
	}

	start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))

	if err1 != nil || err2 != nil || start < 1 || end < start || start > len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.InvalidRange))
		return nil
	}

	if start == 1 && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Queue.RangeIncludesCurrent))
		return nil
	}

	if end > len(q.Songs) {
		end = len(q.Songs)
	}

	songsToRemove := q.Songs[start-1 : end]

	// Caller may only remove their own songs; silently ignore others in the range.
	userID := i.Member.User.ID
	var userSongs []*queue.Song
	for _, song := range songsToRemove {
		if song.RequestedByID == userID {
			userSongs = append(userSongs, song)
		}
	}

	if len(userSongs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission, messages.T(i.GuildID).Queue.NoUserSongsInRange))
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
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Queue.RemoveFailed, err)))
		return err
	}

	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Queue.RangeRemoved, start, end, len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}

func handleRemoveSingle(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, positionStr string) error {
	position, err := strconv.Atoi(strings.TrimSpace(positionStr))
	if err != nil || position < 1 || position > len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	if position == 1 && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Queue.CannotRemoveCurrent))
		return nil
	}

	songToRemove := q.Songs[position-1]

	if songToRemove.RequestedByID != i.Member.User.ID {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission, messages.T(i.GuildID).Queue.OnlyOwnSongs))
		return nil
	}

	isNextSongRemoved := len(q.Songs) > 1 && q.Songs[1].ID == songToRemove.ID

	if err := queue.RemoveSong(i.GuildID, position-1); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Queue.RemoveFailed, err)))
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
