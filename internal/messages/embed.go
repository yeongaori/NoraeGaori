package messages

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func CreateEmbed(color int, title, description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       color,
	}
}

func CreateSongEmbed(guildID string, color int, title, description, songTitle, songURL, uploader, duration, requester, thumbnailURL string) *discordgo.MessageEmbed {
	t := T(guildID)

	uploaderValue := EscapeMarkdown(uploader)
	if uploaderValue == "" {
		uploaderValue = "-"
	}
	durationValue := duration
	if durationValue == "" {
		durationValue = "-"
	}
	requesterValue := EscapeMarkdown(requester)
	if requesterValue == "" {
		requesterValue = "-"
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: FormatBoldMaskedLink(songTitle, songURL),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   t.Fields.Uploader,
				Value:  uploaderValue,
				Inline: true,
			},
			{
				Name:   t.Fields.Duration,
				Value:  durationValue,
				Inline: true,
			},
			{
				Name:   t.Fields.Requester,
				Value:  requesterValue,
				Inline: true,
			},
		},
	}

	if thumbnailURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: thumbnailURL}
	}

	if description != "" {
		embed.Description = description + "\n\n" + embed.Description
	}

	return embed
}

func CreateErrorEmbed(title, description string) *discordgo.MessageEmbed {
	return CreateEmbed(ColorError, title, description)
}

func CreateSuccessEmbed(title, description string) *discordgo.MessageEmbed {
	return CreateEmbed(ColorSuccess, title, description)
}

func CreateWarningEmbed(title, description string) *discordgo.MessageEmbed {
	return CreateEmbed(ColorWarning, title, description)
}

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
	text = strings.ReplaceAll(text, `[`, `［`)
	text = strings.ReplaceAll(text, `]`, `］`)
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

func SetFooter(embed *discordgo.MessageEmbed, text string) {
	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: text,
	}
}

func SetThumbnail(embed *discordgo.MessageEmbed, url string) {
	embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
		URL: url,
	}
}
