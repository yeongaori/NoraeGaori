package queue

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/automix"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

const (
	queuePanelExpiry = 5 * time.Minute

	queuePrevPrefix    = "queue_prev_"
	queueNextPrefix    = "queue_next_"
	queueOpenMixPrefix = "queue_open_mix_"
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

func createQueueButtons(guildID string, page, totalPages int, token string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    messages.T(guildID).Buttons.Previous,
					Style:    discordgo.PrimaryButton,
					CustomID: queuePrevPrefix + token,
					Disabled: page == 1,
				},
				discordgo.Button{
					Label:    messages.T(guildID).Queue.QueueNextButton,
					Style:    discordgo.PrimaryButton,
					CustomID: queueNextPrefix + token,
					Disabled: page == totalPages,
				},
				discordgo.Button{
					Label:    messages.T(guildID).AutoMixPanel.MixButton,
					Style:    discordgo.SecondaryButton,
					CustomID: queueOpenMixPrefix + token,
				},
			},
		},
	}
}

type queuePanel struct {
	guildID string
	token   string
	perPage int

	pageMu sync.Mutex
	page   int
}

func (panel *queuePanel) pageCount(songs int) int {
	if songs <= panel.perPage {
		return 1
	}
	return (songs + panel.perPage - 1) / panel.perPage
}

func (panel *queuePanel) isPageButton(customID string) bool {
	return customID == queuePrevPrefix+panel.token || customID == queueNextPrefix+panel.token
}

func (panel *queuePanel) turnPage(customID string, totalPages int) int {
	panel.pageMu.Lock()
	defer panel.pageMu.Unlock()

	if customID == queuePrevPrefix+panel.token && panel.page > 1 {
		panel.page--
	}
	if customID == queueNextPrefix+panel.token && panel.page < totalPages {
		panel.page++
	}
	return panel.clampedPage(totalPages)
}

func (panel *queuePanel) currentPage(totalPages int) int {
	panel.pageMu.Lock()
	defer panel.pageMu.Unlock()
	return panel.clampedPage(totalPages)
}

func (panel *queuePanel) clampedPage(totalPages int) int {
	if panel.page > totalPages {
		panel.page = totalPages
	}
	if panel.page < 1 {
		panel.page = 1
	}
	return panel.page
}

func (panel *queuePanel) handleInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if ic.Type != discordgo.InteractionMessageComponent || ic.GuildID != panel.guildID {
		return
	}

	data := ic.MessageComponentData()
	if data.CustomID == queueOpenMixPrefix+panel.token {
		automix.OpenPanelFromComponent(s, ic)
		return
	}
	if !panel.isPageButton(data.CustomID) {
		return
	}

	q, err := queue.GetQueue(panel.guildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return
	}

	totalPages := panel.pageCount(len(q.Songs))
	page := panel.turnPage(data.CustomID, totalPages)

	embed := createQueueEmbed(panel.guildID, q.Songs, page, totalPages, panel.perPage)
	components := createQueueButtons(panel.guildID, page, totalPages, panel.token)

	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	}); err != nil {
		logger.Errorf("Failed to turn the queue page: %v", err)
	}
}

func expireQueuePanel(s *discordgo.Session, i *discordgo.InteractionCreate, panelMsg *discordgo.Message, panel *queuePanel, removeHandler func()) {
	defer removeHandler()

	<-time.After(queuePanelExpiry)

	panelMsg = discord.ResolvePanelMessage(s, i, panelMsg)
	if panelMsg == nil {
		return
	}

	q, err := queue.GetQueue(panel.guildID, false)
	if err != nil || q == nil {
		return
	}

	totalPages := panel.pageCount(len(q.Songs))
	embed := createQueueEmbed(panel.guildID, q.Songs, panel.currentPage(totalPages), totalPages, panel.perPage)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         panelMsg.ID,
		Channel:    panelMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	}); err != nil {
		logger.Errorf("Failed to close the queue panel: %v", err)
	}
}
