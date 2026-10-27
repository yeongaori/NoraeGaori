package admin

import (
	"fmt"
	"noraegaori/internal/discord"
	"regexp"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

var (
	digitsRegex      = regexp.MustCompile(`^\d+$`)
	userMentionRegex = regexp.MustCompile(`^<@!?(\d+)>$`)
)

func HandleForceRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.MentionTarget)
		return nil
	}
	target := options[0].StringValue()

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.NoSongsToDelete)
		return nil
	}

	if digitsRegex.MatchString(target) {
		if len(target) >= 17 {
			return forceRemoveByUser(s, i, q, target)
		}
		position, _ := strconv.Atoi(target)
		return forceRemoveByPosition(s, i, q, position)
	}

	matches := userMentionRegex.FindStringSubmatch(target)
	if len(matches) < 2 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.InvalidMention)
		return nil
	}

	return forceRemoveByUser(s, i, q, matches[1])
}

func forceRemoveByUser(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, targetUserID string) error {
	if len(q.Songs) < 2 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.NoSongsToDelete)
		return nil
	}

	targetUser, err := s.User(targetUserID)
	if err != nil {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.UserNotFound)
		return nil
	}

	userSongs, startIdx := songsRequestedBy(q, targetUserID)

	if len(userSongs) == 0 {
		description := fmt.Sprintf(messages.T(i.GuildID).Admin.UserNoSongs, messages.EscapeMarkdown(targetUser.Username))
		if startIdx == 1 {
			description += messages.T(i.GuildID).Admin.ExcludingCurrent
		}
		discord.RespondErrorEmbed(s, i, description)
		return nil
	}

	songIDs := make([]int, len(userSongs))
	for i, song := range userSongs {
		songIDs[i] = song.ID
	}

	if err := queue.RemoveSongsByIDs(i.GuildID, songIDs); err != nil {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Admin.RemoveFailed, err))
		return err
	}

	player.InvalidatePreCaches(i.GuildID, songIDs)

	respondRemovedSongs(s, i, targetUser.Username, userSongs)
	return nil
}

func songsRequestedBy(q *queue.Queue, targetUserID string) ([]*queue.Song, int) {
	var userSongs []*queue.Song
	startIdx := 1
	if !q.Playing && !q.Loading {
		startIdx = 0
	}

	for i := startIdx; i < len(q.Songs); i++ {
		if q.Songs[i].RequestedByID == targetUserID {
			userSongs = append(userSongs, q.Songs[i])
		}
	}
	return userSongs, startIdx
}

func respondRemovedSongs(s *discordgo.Session, i *discordgo.InteractionCreate, username string, userSongs []*queue.Song) {
	songLines := make([]string, 0, len(userSongs))
	for _, song := range userSongs {
		var titlePart string
		if song.URL != "" {
			titlePart = messages.FormatBoldMaskedLink(song.Title, song.URL)
		} else {
			titlePart = "**" + messages.EscapeMarkdown(song.Title) + "**"
		}
		songLines = append(songLines, "• "+titlePart)
	}
	summary := fmt.Sprintf(messages.T(i.GuildID).Admin.DeleteCompleteDesc, messages.EscapeMarkdown(username), len(userSongs))
	chunks := discord.SplitLinesIntoChunks(songLines, 3900)
	discord.RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Admin.DeleteCompleteTitle, summary+"\n\n"+chunks[0]))
	for _, chunk := range chunks[1:] {
		discord.FollowUpEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       messages.T(i.GuildID).Admin.DeleteCompleteTitle,
			Description: chunk,
		})
	}
}

func forceRemoveByPosition(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, position int) error {
	if position < 1 || position > len(q.Songs) {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Admin.EnterValidRange, len(q.Songs)))
		return nil
	}

	if position == 1 && (q.Playing || q.Loading) {
		return forceRemoveCurrent(s, i, q)
	}

	songToRemove := q.Songs[position-1]

	if err := queue.RemoveSong(i.GuildID, position-1); err != nil {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Admin.RemoveFailed, err))
		return err
	}

	player.InvalidatePreCaches(i.GuildID, []int{songToRemove.ID})

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Queue.SongRemoved, messages.EscapeMarkdown(songToRemove.Title)))
	discord.RespondEmbed(s, i, embed)
	return nil
}

func forceRemoveCurrent(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue) error {
	discord.DeferResponse(s, i)

	songTitle := q.Songs[0].Title

	err := player.Skip(s, i.GuildID)
	if err != nil && err != player.ErrQueueEmpty {
		discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Admin.RemoveFailed, err))
		return nil
	}

	if err == player.ErrQueueEmpty {
		if stopErr := player.Stop(i.GuildID); stopErr != nil {
			logger.Errorf("Failed to cleanup after queue empty: %v", stopErr)
		}
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Queue.SongRemoved, messages.EscapeMarkdown(songTitle)))
	_ = discord.UpdateResponseEmbed(s, i, embed)
	return nil
}
