package commands

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func createQueueEmbed(guildID string, songs []*queue.Song, page, totalPages, perPage int) *discordgo.MessageEmbed {
	t := messages.T(guildID)
	start := (page - 1) * perPage
	end := start + perPage
	if end > len(songs) {
		end = len(songs)
	}

	var description strings.Builder

	for idx := start; idx < end; idx++ {
		song := songs[idx]

		duration := song.Duration
		if song.IsLive {
			duration = t.Queue.LiveBadge
		}

		if idx == 0 {
			description.WriteString(fmt.Sprintf("▶️ %s\n   %s: %s | %s: %s | %s: %s\n\n",
				messages.FormatBoldMaskedLink(song.Title, song.URL),
				t.Fields.Uploader, messages.EscapeMarkdown(song.Uploader),
				t.Fields.Duration, duration,
				t.Fields.Requester, messages.EscapeMarkdown(song.RequestedByTag),
			))
		} else {
			description.WriteString(fmt.Sprintf("%d. %s\n   %s: %s | %s: %s | %s: %s\n\n",
				idx+1,
				messages.FormatBoldMaskedLink(song.Title, song.URL),
				t.Fields.Uploader, messages.EscapeMarkdown(song.Uploader),
				t.Fields.Duration, duration,
				t.Fields.Requester, messages.EscapeMarkdown(song.RequestedByTag),
			))
		}
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       t.Titles.Queue,
		Description: description.String(),
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(t.Footers.Pagination, page, totalPages, len(songs)),
		},
	}
}

func createQueueButtons(guildID string, page, totalPages int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    messages.T(guildID).Buttons.Previous,
					Style:    discordgo.PrimaryButton,
					CustomID: "queue_prev",
					Disabled: page == 1,
				},
				discordgo.Button{
					Label:    messages.T(guildID).Queue.QueueNextButton,
					Style:    discordgo.PrimaryButton,
					CustomID: "queue_next",
					Disabled: page == totalPages,
				},
			},
		},
	}
}

func handleQueueButtons(s *discordgo.Session, i *discordgo.InteractionCreate, originalMsg *discordgo.Message, guildID string, totalPages, perPage int) {
	timeout := time.After(5 * time.Minute)
	currentPage := 1
	var pageMu sync.Mutex

	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		currentPage = int(options[0].IntValue())
	}

	originalMsgID := ""
	if originalMsg != nil {
		originalMsgID = originalMsg.ID
	}

	buttonHandler := func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic.Type != discordgo.InteractionMessageComponent {
			return
		}

		
		if originalMsgID != "" && (ic.Message == nil || ic.Message.ID != originalMsgID) {
			return
		}

		data := ic.MessageComponentData()

		if data.CustomID != "queue_prev" && data.CustomID != "queue_next" {
			return
		}

		pageMu.Lock()
		switch data.CustomID {
		case "queue_prev":
			if currentPage > 1 {
				currentPage--
			}
		case "queue_next":
			if currentPage < totalPages {
				currentPage++
			}
		default:
			pageMu.Unlock()
			return
		}
		page := currentPage
		pageMu.Unlock()

		q, err := queue.GetQueue(guildID, false)
		if err != nil || q == nil || len(q.Songs) == 0 {
			return
		}

		embed := createQueueEmbed(guildID, q.Songs, page, totalPages, perPage)
		components := createQueueButtons(guildID, page, totalPages)

		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	}

	removeHandler := s.AddHandler(buttonHandler)
	defer removeHandler()

	<-timeout

	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || originalMsg == nil {
		return
	}

	pageMu.Lock()
	finalPage := currentPage
	pageMu.Unlock()

	embed := createQueueEmbed(guildID, q.Songs, finalPage, totalPages, perPage)

	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         originalMsg.ID,
		Channel:    originalMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	})
}

func formatSeconds(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

func boolToOnOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}
