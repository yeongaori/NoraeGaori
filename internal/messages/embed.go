package messages

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// CreateEmbed creates a basic embed with title, description, and color
func CreateEmbed(color int, title, description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       color,
	}
}

// CreateSongEmbed creates an embed for a song with thumbnail and fields
func CreateSongEmbed(color int, title, description, songTitle, songURL, uploader, duration, requester, thumbnailURL string) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: FormatBoldMaskedLink(songTitle, songURL),
		Color:       color,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: thumbnailURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   FieldUploader,
				Value:  EscapeMarkdown(uploader),
				Inline: true,
			},
			{
				Name:   FieldDuration,
				Value:  duration,
				Inline: true,
			},
			{
				Name:   FieldRequester,
				Value:  EscapeMarkdown(requester),
				Inline: true,
			},
		},
	}

	if description != "" {
		embed.Description = description + "\n\n" + embed.Description
	}

	return embed
}

// CreateQueueEmbed creates an embed for queue display
func CreateQueueEmbed(songs []QueueSong, page, totalPages, totalSongs int) *discordgo.MessageEmbed {
	var desc strings.Builder

	for i, song := range songs {
		if i == 0 {
			desc.WriteString(fmt.Sprintf("▶️ %s\n   %s: %s | %s: %s | %s: %s\n\n",
				FormatBoldMaskedLink(song.Title, song.URL),
				FieldUploader, EscapeMarkdown(song.Uploader),
				FieldDuration, song.Duration,
				FieldRequester, EscapeMarkdown(song.Requester),
			))
		} else {
			desc.WriteString(fmt.Sprintf("%d. %s\n   %s: %s | %s: %s | %s: %s\n\n",
				song.Position,
				FormatBoldMaskedLink(song.Title, song.URL),
				FieldUploader, EscapeMarkdown(song.Uploader),
				FieldDuration, song.Duration,
				FieldRequester, EscapeMarkdown(song.Requester),
			))
		}
	}

	return &discordgo.MessageEmbed{
		Title:       TitleQueue,
		Description: desc.String(),
		Color:       ColorInfo,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(FooterPagination, page, totalPages, totalSongs),
		},
	}
}

// QueueSong represents a song in the queue display
type QueueSong struct {
	Position  int
	Title     string
	URL       string
	Uploader  string
	Duration  string
	Requester string
}

// CreateErrorEmbed creates a simple error embed
func CreateErrorEmbed(title, description string) *discordgo.MessageEmbed {
	if title == "" {
		title = TitleError
	}
	return CreateEmbed(ColorError, title, description)
}

// CreateSuccessEmbed creates a simple success embed
func CreateSuccessEmbed(title, description string) *discordgo.MessageEmbed {
	return CreateEmbed(ColorSuccess, title, description)
}

// CreateWarningEmbed creates a simple warning embed
func CreateWarningEmbed(title, description string) *discordgo.MessageEmbed {
	return CreateEmbed(ColorWarning, title, description)
}

// CreateInfoEmbed creates a simple info embed
func CreateInfoEmbed(title, description string) *discordgo.MessageEmbed {
	return CreateEmbed(ColorInfo, title, description)
}

func EscapeInline(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, `*`, `\*`)
	text = strings.ReplaceAll(text, `_`, `\_`)
	text = strings.ReplaceAll(text, `~`, `\~`)
	text = strings.ReplaceAll(text, "`", "\\`")
	text = strings.ReplaceAll(text, `|`, `\|`)
	return text
}

func EscapeMarkdown(text string) string {
	text = EscapeInline(text)
	text = strings.ReplaceAll(text, `[`, `\[`)
	text = strings.ReplaceAll(text, `]`, `\]`)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ">") ||
			strings.HasPrefix(line, "- ") || line == "-" {
			lines[i] = `\` + line
		}
	}
	return strings.Join(lines, "\n")
}

func EscapeMessageContent(text string) string {
	text = EscapeInline(text)
	text = strings.ReplaceAll(text, `[`, `\[`)
	text = strings.ReplaceAll(text, `]`, `\]`)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "- ") || line == "-" {
			lines[i] = `\` + line
		}
	}
	return strings.Join(lines, "\n")
}

func EscapeLinkText(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, `*`, `\*`)
	text = strings.ReplaceAll(text, `~`, `\~`)
	text = strings.ReplaceAll(text, `|`, `\|`)
	text = strings.ReplaceAll(text, "`", "\\`")
	return text
}

func EscapeURL(rawURL string) string {
	return strings.ReplaceAll(rawURL, `)`, `%29`)
}

func FormatMaskedLink(title, url string) string {
	return fmt.Sprintf("[%s](%s)", EscapeLinkText(title), EscapeURL(url))
}

func FormatBoldMaskedLink(title, url string) string {
	return fmt.Sprintf("**%s**", FormatMaskedLink(title, url))
}

func StripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	markers := []string{"***", "**", "__", "~~", "||", "*", "_", "`"}
	for _, marker := range markers {
		if len(s) > 2*len(marker) && strings.HasPrefix(s, marker) && strings.HasSuffix(s, marker) {
			s = s[len(marker) : len(s)-len(marker)]
			break
		}
	}
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) > 2 {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// CreateNavigationButtons creates Previous/Next buttons for pagination
func CreateNavigationButtons(currentPage, totalPages int, commandPrefix string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    ButtonPrevious,
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("%s_prev_%d", commandPrefix, currentPage-1),
					Disabled: currentPage <= 1,
				},
				discordgo.Button{
					Label:    ButtonNext,
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("%s_next_%d", commandPrefix, currentPage+1),
					Disabled: currentPage >= totalPages,
				},
			},
		},
	}
}

// AddField adds a field to an embed
func AddField(embed *discordgo.MessageEmbed, name, value string, inline bool) {
	if embed.Fields == nil {
		embed.Fields = []*discordgo.MessageEmbedField{}
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   name,
		Value:  value,
		Inline: inline,
	})
}

// SetFooter sets the footer of an embed
func SetFooter(embed *discordgo.MessageEmbed, text string) {
	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: text,
	}
}

// SetThumbnail sets the thumbnail of an embed
func SetThumbnail(embed *discordgo.MessageEmbed, url string) {
	embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
		URL: url,
	}
}
