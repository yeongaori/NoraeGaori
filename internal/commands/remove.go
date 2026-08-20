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

func HandleRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.EnterPosition))
		return nil
	}
	positionStr := options[0].StringValue()

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
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

func songIDsOf(songs []*queue.Song) []int {
	songIDs := make([]int, 0, len(songs))
	for _, song := range songs {
		songIDs = append(songIDs, song.ID)
	}
	return songIDs
}

func ownedBy(songs []*queue.Song, userID string) bool {
	for _, song := range songs {
		if song.RequestedByID != userID {
			return false
		}
	}
	return true
}

func removeSongs(guildID string, songs []*queue.Song) error {
	songIDs := songIDsOf(songs)

	q, err := queue.GetQueue(guildID, false)
	isNextSongRemoved := false
	if err == nil && q != nil && len(q.Songs) > 1 {
		for _, songID := range songIDs {
			if songID == q.Songs[1].ID {
				isNextSongRemoved = true
				break
			}
		}
	}

	if err := queue.RemoveSongsByIDs(guildID, songIDs); err != nil {
		return err
	}

	if isNextSongRemoved {
		player.CleanupPreCacheWorker(guildID)
	}
	return nil
}

func removableFrom(q *queue.Queue, songIDs []int) []*queue.Song {
	if q == nil || len(q.Songs) == 0 {
		return nil
	}

	wanted := make(map[int]struct{}, len(songIDs))
	for _, songID := range songIDs {
		wanted[songID] = struct{}{}
	}

	songs := make([]*queue.Song, 0, len(songIDs))
	for index, song := range q.Songs {
		if _, ok := wanted[song.ID]; !ok {
			continue
		}
		if index == 0 && (q.Playing || q.Loading) {
			continue
		}
		songs = append(songs, song)
	}
	return songs
}

func removableSongs(guildID string, songIDs []int) ([]*queue.Song, error) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil {
		return nil, err
	}
	return removableFrom(q, songIDs), nil
}

func applyRemoveVote(guildID string, songIDs []int, result func(removed int) *discordgo.MessageEmbed) func(*discordgo.Session, *voteSession, voteTally) {
	return func(s *discordgo.Session, session *voteSession, tally voteTally) {
		songs, err := removableSongs(guildID, songIDs)
		if err != nil {
			renderVoteFailure(s, session, messages.T(guildID).Titles.Error, fmt.Sprintf(messages.T(guildID).Queue.RemoveFailed, err))
			return
		}

		if len(songs) == 0 {
			renderVoteFailure(s, session, messages.T(guildID).Queue.NoSongsToRemoveTitle, messages.T(guildID).Queue.RemoveTargetGone)
			return
		}

		if err := removeSongs(guildID, songs); err != nil {
			renderVoteFailure(s, session, messages.T(guildID).Titles.Error, fmt.Sprintf(messages.T(guildID).Queue.RemoveFailed, err))
			return
		}

		renderVoteResult(s, session, result(len(songs)), tally)
	}
}

func startRemoveVote(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*queue.Song, description string, result func(removed int) *discordgo.MessageEmbed) error {
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterVoiceChannel))
		return nil
	}

	requiredVotes, err := requiredVotesInChannel(s, i.GuildID, voiceState.ChannelID, resolveWithFetch)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	songIDs := songIDsOf(songs)

	if requiredVotes == 1 {
		return removeImmediately(s, i, songs, result)
	}

	return startVote(s, i, voteRequest{
		kind:           voteKindRemove,
		title:          messages.T(i.GuildID).Titles.RemoveVote,
		description:    description,
		emoji:          "❌",
		voiceChannelID: voiceState.ChannelID,
		requiredVotes:  requiredVotes,
		target:         voteTarget{scope: voteScopeSongs, songIDs: songIDs},
		onPassed:       applyRemoveVote(i.GuildID, songIDs, result),
	})
}

func removeImmediately(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*queue.Song, result func(removed int) *discordgo.MessageEmbed) error {
	if err := removeSongs(i.GuildID, songs); err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Queue.RemoveFailed, err)))
		return err
	}

	UpdateResponseEmbed(s, i, result(len(songs)))
	return nil
}

func handleRemoveAll(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue) error {
	userID := i.Member.User.ID

	var userSongs []*queue.Song
	startIdx := 0
	if q.Playing || q.Loading {
		startIdx = 1
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

		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Queue.NoSongsToRemoveTitle, description))
		return nil
	}

	return removeImmediately(s, i, userSongs, func(removed int) *discordgo.MessageEmbed {
		return messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Queue.SongsRemovedAll, i.Member.User.Username, removed))
	})
}

func handleRemoveRange(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, rangeStr string) error {
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.InvalidRange))
		return nil
	}

	start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))

	if err1 != nil || err2 != nil || start < 1 || end < start || start > len(q.Songs) {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.InvalidRange))
		return nil
	}

	if start == 1 && (q.Playing || q.Loading) {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Queue.RangeIncludesCurrent))
		return nil
	}

	if end > len(q.Songs) {
		end = len(q.Songs)
	}

	songsToRemove := q.Songs[start-1 : end]

	result := func(removed int) *discordgo.MessageEmbed {
		return messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Queue.RangeRemoved, removed, start, end))
	}

	if ownedBy(songsToRemove, i.Member.User.ID) {
		return removeImmediately(s, i, songsToRemove, result)
	}

	description := fmt.Sprintf(messages.T(i.GuildID).Queue.RemoveRangeVoteDesc, len(songsToRemove), start, end)
	return startRemoveVote(s, i, songsToRemove, description, result)
}

func handleRemoveSingle(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, positionStr string) error {
	position, err := strconv.Atoi(strings.TrimSpace(positionStr))
	if err != nil || position < 1 || position > len(q.Songs) {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	if position == 1 && (q.Playing || q.Loading) {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Queue.CannotRemoveCurrent))
		return nil
	}

	songToRemove := q.Songs[position-1]
	songs := []*queue.Song{songToRemove}

	result := func(removed int) *discordgo.MessageEmbed {
		return messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Queue.SongRemoved, messages.EscapeMarkdown(songToRemove.Title)))
	}

	if songToRemove.RequestedByID == i.Member.User.ID {
		return removeImmediately(s, i, songs, result)
	}

	description := fmt.Sprintf(messages.T(i.GuildID).Queue.RemoveVoteDesc, messages.EscapeMarkdown(songToRemove.Title))
	return startRemoveVote(s, i, songs, description, result)
}
