package queue

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/automix"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

const (
	queuePageRoute = "queue_page"
	queueMixRoute  = "queue_mix"

	songsPerPage = 10
)

func renderQueuePage(guildID string, songs []*queue.Song, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	totalPages := discord.PageCount(len(songs), songsPerPage)
	page = discord.ClampPage(page, totalPages)
	return createQueueEmbed(guildID, songs, page, totalPages), createQueueButtons(guildID, page, totalPages)
}

func createQueueEmbed(guildID string, songs []*queue.Song, page, totalPages int) *discordgo.MessageEmbed {
	t := messages.T(guildID)
	start, end := discord.PageBounds(page, songsPerPage, len(songs))

	var description strings.Builder
	for index := start; index < end; index++ {
		song := songs[index]

		duration := song.Duration
		if song.IsLive {
			duration = t.Queue.LiveBadge
		}

		marker := "▶️"
		if index > 0 {
			marker = strconv.Itoa(index+1) + "."
		}

		fmt.Fprintf(&description, "%s %s\n   %s: %s | %s: %s | %s: %s\n\n",
			marker,
			messages.FormatBoldMaskedLink(song.Title, song.URL),
			t.Fields.Uploader, messages.EscapeMarkdown(song.Uploader),
			t.Fields.Duration, duration,
			t.Fields.Requester, messages.EscapeMarkdown(song.RequestedByTag),
		)
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
	t := messages.T(guildID)
	mixButton := discordgo.Button{
		Label:    t.AutoMixPanel.MixButton,
		Style:    discordgo.SecondaryButton,
		CustomID: queueMixRoute,
	}
	return []discordgo.MessageComponent{
		discord.PageButtonRow(queuePageRoute, page, totalPages, t.Buttons.Previous, t.Queue.QueueNextButton, nil, mixButton),
	}
}

func emptyQueueEmbed(guildID string) *discordgo.MessageEmbed {
	t := messages.T(guildID)
	return messages.CreateErrorEmbed(t.Titles.EmptyQueue, t.Descriptions.EmptyQueue)
}

func turnQueuePage(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	page, hasPage := discord.PageArgument(ic, arguments, 1)
	if !hasPage {
		return
	}

	q, err := queue.GetQueue(ic.GuildID, false)
	if err != nil {
		logger.Errorf("Failed to load the queue for a page turn: %v", err)
		t := messages.T(ic.GuildID)
		respondQueueNotice(s, ic, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Errors.CommandExecutionError, err)))
		return
	}
	if q == nil || len(q.Songs) == 0 {
		respondQueueNotice(s, ic, emptyQueueEmbed(ic.GuildID))
		return
	}

	embed, components := renderQueuePage(ic.GuildID, q.Songs, page)
	if err := discord.UpdateComponentMessage(s, ic, embed, components); err != nil {
		logger.Errorf("Failed to turn the queue page: %v", err)
	}
}

func respondQueueNotice(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if err := discord.RespondEphemeralEmbed(s, ic, embed); err != nil {
		logger.Errorf("Failed to answer a queue page turn: %v", err)
	}
}

func openMixPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, _ []string) {
	if ic.Type == discordgo.InteractionMessageComponent {
		automix.OpenPanelFromComponent(s, ic)
	}
}
