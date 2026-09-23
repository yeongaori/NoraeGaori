package automix

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

const (
	transitionPickRoute  = "automix_pick"
	transitionPageRoute  = "automix_page"
	transitionStyleRoute = "automix_style"
)

type panelLocation struct {
	messageID string
	page      int
}

func registerPanelRoutes() {
	discord.RegisterComponentRoute(transitionPageRoute, turnTransitionPage)
	discord.RegisterComponentRoute(transitionPickRoute, pickTransition)
	discord.RegisterComponentRoute(transitionStyleRoute, chooseTransitionStyle)
}

func HandleAutoMixPanel(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	page := 1
	if options := i.ApplicationCommandData().Options; len(options) > 0 {
		page = int(options[0].IntValue())
	}

	embed, components, hasTransitions := prepareTransitionPanel(s, i.GuildID, page)
	if !hasTransitions {
		discord.RespondEmbed(s, i, emptyPanelEmbed(i.GuildID))
		return nil
	}

	if _, err := discord.SendEmbedWithComponents(s, i, embed, components); err != nil {
		logger.Errorf("Failed to send panel: %v", err)
		return err
	}
	return nil
}

func OpenPanelFromComponent(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	embed, components, hasTransitions := prepareTransitionPanel(s, ic.GuildID, 1)
	if !hasTransitions {
		respondPanelNotice(s, ic, emptyPanelEmbed(ic.GuildID))
		return
	}

	if _, err := discord.SendEmbedWithComponents(s, ic, embed, components); err != nil {
		logger.Errorf("Failed to open panel from component: %v", err)
	}
}

func prepareTransitionPanel(s *discordgo.Session, guildID string, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent, bool) {
	state, isLoaded := loadPanelState(guildID)
	if !isLoaded || len(state.pairs) == 0 {
		return nil, nil, false
	}

	go player.StartAnalysisBackfill(guildID, voiceChannelBitrate(s, guildID))

	embed, components := renderTransitionPage(guildID, &state, page)
	return embed, components, true
}

func renderTransitionPanel(guildID string, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	state, isLoaded := loadPanelState(guildID)
	if !isLoaded {
		return emptyPanelEmbed(guildID), nil
	}
	return renderTransitionPage(guildID, &state, page)
}

func renderTransitionPage(guildID string, state *panelState, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	totalPages := transitionPageCount(state.pairs)
	page = discord.ClampPage(page, totalPages)
	rows := hydrateTransitionRows(guildID, state, transitionPageSlice(state.pairs, page))
	return createTransitionPanelEmbed(guildID, state, rows, page, totalPages),
		createTransitionPanelComponents(guildID, rows, page, totalPages)
}

func emptyPanelEmbed(guildID string) *discordgo.MessageEmbed {
	panel := &messages.T(guildID).AutoMixPanel
	return messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc)
}

func respondPanelNotice(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if err := discord.RespondEphemeralEmbed(s, ic, embed); err != nil {
		logger.Errorf("Failed to report a transition panel notice: %v", err)
	}
}

func turnTransitionPage(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	page, hasPage := discord.PageArgument(ic, arguments, 1)
	if !hasPage {
		return
	}

	embed, components := renderTransitionPanel(ic.GuildID, page)
	if err := discord.UpdateComponentMessage(s, ic, embed, components); err != nil {
		logger.Errorf("Failed to turn the transition panel page: %v", err)
	}
}

func pickTransition(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	page, hasPage := discord.PageArgument(ic, arguments, 1)
	selected, isSelected := discord.SelectedValue(ic)
	if !hasPage || !isSelected || ic.Message == nil {
		return
	}
	songID, err := strconv.Atoi(selected)
	if err != nil {
		return
	}

	location := &panelLocation{messageID: ic.Message.ID, page: page}
	openTransitionEditor(s, ic, songID, location)
	refreshTransitionPanel(s, ic, location)
}

func chooseTransitionStyle(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	page, hasPage := discord.PageArgument(ic, arguments, 4)
	style, isSelected := discord.SelectedValue(ic)
	if !hasPage || !isSelected {
		return
	}
	songID, err := strconv.Atoi(arguments[1])
	if err != nil {
		return
	}

	category := arguments[0]
	location := &panelLocation{messageID: arguments[2], page: page}
	panel := &messages.T(ic.GuildID).AutoMixPanel

	if !transition.ValidStyle(category, style) {
		redrawTransitionEditor(s, ic, songID, location, fmt.Sprintf(panel.UpdateFailed, style))
		return
	}

	if err := queue.SetSongAutoMixStyle(ic.GuildID, songID, category, style); err != nil {
		if errors.Is(err, queue.ErrSongNotInQueue) {
			closeTransitionEditor(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone))
			return
		}
		redrawTransitionEditor(s, ic, songID, location, fmt.Sprintf(panel.UpdateFailed, err))
		return
	}

	if redrawTransitionEditor(s, ic, songID, location, "") {
		refreshTransitionPanel(s, ic, location)
	}
}

func openTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, songID int, location *panelLocation) {
	state, row, notice := loadTransitionRow(ic.GuildID, songID)
	if notice != nil {
		respondPanelNotice(s, ic, notice)
		return
	}

	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{createTransitionEditorEmbed(ic.GuildID, &state, row, "")},
			Components: createTransitionEditorComponents(ic.GuildID, &state, row, location),
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		logger.Errorf("Failed to open editor: %v", err)
	}
}

func redrawTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, songID int, location *panelLocation, errorMessage string) bool {
	state, row, notice := loadTransitionRow(ic.GuildID, songID)
	if notice != nil {
		closeTransitionEditor(s, ic, notice)
		return false
	}

	embed := createTransitionEditorEmbed(ic.GuildID, &state, row, errorMessage)
	if err := discord.UpdateComponentMessage(s, ic, embed, createTransitionEditorComponents(ic.GuildID, &state, row, location)); err != nil {
		logger.Errorf("Failed to redraw the transition editor: %v", err)
	}
	return true
}

func loadTransitionRow(guildID string, songID int) (panelState, transitionRow, *discordgo.MessageEmbed) {
	panel := &messages.T(guildID).AutoMixPanel

	state, isLoaded := loadPanelState(guildID)
	if !isLoaded {
		return panelState{}, transitionRow{}, messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc)
	}
	pair, found := findTransitionPair(state.pairs, songID)
	if !found {
		return panelState{}, transitionRow{}, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone)
	}
	return state, hydrateTransitionRow(guildID, &state, pair), nil
}

func closeTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if err := discord.UpdateComponentMessage(s, ic, embed, nil); err != nil {
		logger.Errorf("Failed to close the transition editor: %v", err)
	}
}

func refreshTransitionPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, location *panelLocation) {
	embed, components := renderTransitionPanel(ic.GuildID, location.page)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         location.messageID,
		Channel:    ic.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	}); err != nil {
		logger.Errorf("Failed to refresh the transition panel: %v", err)
	}
}
