package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

// parseSeekPosition parses "ss", "mm:ss", or "hh:mm:ss" into milliseconds; enforces sub-60 when a higher unit is present.
func parseSeekPosition(input string) (int, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, fmt.Errorf("empty position")
	}
	parts := strings.Split(input, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid format")
	}
	values := make([]int, len(parts))
	for idx, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid number %q", p)
		}
		if len(parts) > 1 && idx > 0 && n >= 60 {
			return 0, fmt.Errorf("component out of range %q", p)
		}
		values[idx] = n
	}
	var totalSeconds int
	switch len(values) {
	case 1:
		totalSeconds = values[0]
	case 2:
		totalSeconds = values[0]*60 + values[1]
	case 3:
		totalSeconds = values[0]*3600 + values[1]*60 + values[2]
	}
	return totalSeconds * 1000, nil
}

func HandleSeek(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if _, errEmbed := checkUserInBotVoiceChannel(s, i); errEmbed != nil {
		RespondEmbed(s, i, errEmbed)
		return nil
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.SeekInvalidFormat))
		return nil
	}
	posStr, ok := options[0].Value.(string)
	if !ok || posStr == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.SeekInvalidFormat))
		return nil
	}
	posMs, err := parseSeekPosition(posStr)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.SeekInvalidFormat))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}
	currentSong := q.Songs[0]
	if currentSong.IsLive {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.SeekLiveStream))
		return nil
	}
	durationMs := youtube.ParseDurationToSeconds(currentSong.Duration) * 1000
	if durationMs > 0 && posMs > durationMs {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.SeekOutOfBounds))
		return nil
	}

	if err := player.Seek(i.GuildID, posMs); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.SeekFailed, err)))
		return err
	}

	embed := messages.CreateSuccessEmbed(
		messages.T(i.GuildID).Music.SeekedTitle,
		fmt.Sprintf(messages.T(i.GuildID).Music.SeekedDesc,
			messages.FormatMaskedLink(currentSong.Title, currentSong.URL),
			player.FormatDuration(posMs),
			currentSong.Duration),
	)
	RespondEmbed(s, i, embed)
	return nil
}
