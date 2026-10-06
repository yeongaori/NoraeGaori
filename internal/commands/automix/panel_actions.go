package automix

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/commands/settings"
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
	transitionTabRoute   = "automix_tab"
	mixingSettingsRoute  = "automix_mixing"
	panelOpenRoute       = "automix_open"
)

type panelLocation struct {
	messageID string
	page      int
}

func registerPanelRoutes() {
	discord.RegisterComponentRoute(transitionPageRoute, turnTransitionPage)
	discord.RegisterComponentRoute(transitionPickRoute, pickTransition)
	discord.RegisterComponentRoute(transitionStyleRoute, chooseTransitionStyle)
	discord.RegisterComponentRoute(transitionTabRoute, turnEditorTab)
	discord.RegisterComponentRoute(mixingSettingsRoute, openMixingSettings)
	discord.RegisterComponentRoute(panelOpenRoute, openPanel)
	discord.AttachDropdownButtons("automix", panelOpenButtons)
}

func panelOpenButtons(guildID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.Button{
		Label:    messages.T(guildID).AutoMixPanel.OpenButton,
		Style:    discordgo.SecondaryButton,
		CustomID: panelOpenRoute,
	}}
}

func openPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, _ []string) {
	OpenPanelFromComponent(s, ic)
}

func openMixingSettings(s *discordgo.Session, ic *discordgo.InteractionCreate, _ []string) {
	settings.OpenMixingPanel(s, ic)
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

	if state.autoSelect {
		go player.StartAnalysisBackfill(guildID, voiceChannelBitrate(s, guildID))
	}

	embed, components := renderTransitionPage(guildID, state, page)
	return embed, components, true
}

func renderTransitionPanel(guildID string, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	state, isLoaded := loadPanelState(guildID)
	if !isLoaded {
		return emptyPanelEmbed(guildID), nil
	}
	return renderTransitionPage(guildID, state, page)
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

func parseEditorArguments(ic *discordgo.InteractionCreate, arguments []string) (int, *panelLocation, bool) {
	page, hasPage := discord.PageArgument(ic, arguments, 4)
	if !hasPage {
		return 0, nil, false
	}
	songID, err := strconv.Atoi(arguments[1])
	if err != nil {
		return 0, nil, false
	}
	return songID, &panelLocation{messageID: arguments[2], page: page}, true
}

func turnEditorTab(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	songID, location, isValid := parseEditorArguments(ic, arguments)
	if !isValid {
		return
	}
	tab := findTab(arguments[0])
	if tab == nil {
		logger.Errorf("AutoMix panel received the unknown tab %q", arguments[0])
		return
	}
	redrawTransitionEditor(s, ic, songID, location, tab, "")
}

func chooseTransitionStyle(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	songID, location, isValid := parseEditorArguments(ic, arguments)
	style, isSelected := discord.SelectedValue(ic)
	if !isValid || !isSelected {
		return
	}

	panel := &messages.T(ic.GuildID).AutoMixPanel
	category, isKnown := transition.ParseCategory(arguments[0])
	tab := tabOfCategory(category)
	if !isKnown || tab == nil {
		logger.Errorf("AutoMix panel received the unknown category %q", arguments[0])
		return
	}

	if !transition.ValidStyle(category, style) {
		logger.Errorf("AutoMix panel received the unknown %s style %q", category, style)
		redrawTransitionEditor(s, ic, songID, location, tab, fmt.Sprintf(panel.UpdateFailed, style))
		return
	}

	if err := queue.SetSongAutoMixOverrides(ic.GuildID, songID, category.Override(style)); err != nil {
		if errors.Is(err, queue.ErrSongNotInQueue) {
			closeTransitionEditor(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone))
			return
		}
		redrawTransitionEditor(s, ic, songID, location, tab, fmt.Sprintf(panel.UpdateFailed, err))
		return
	}

	if redrawTransitionEditor(s, ic, songID, location, tab, "") {
		refreshTransitionPanel(s, ic, location)
	}
}

func openTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, songID int, location *panelLocation) {
	row, notice := loadTransitionRow(ic.GuildID, songID)
	if notice != nil {
		respondPanelNotice(s, ic, notice)
		return
	}

	tab := &editorTabs[0]
	embed := createTransitionEditorEmbed(ic.GuildID, row, tab, "")
	if err := discord.RespondEphemeralEmbed(s, ic, embed, createTransitionEditorComponents(ic.GuildID, row, tab, location)...); err != nil {
		logger.Errorf("Failed to open editor: %v", err)
	}
}

func redrawTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, songID int, location *panelLocation, tab *editorTab, errorMessage string) bool {
	row, notice := loadTransitionRow(ic.GuildID, songID)
	if notice != nil {
		closeTransitionEditor(s, ic, notice)
		return false
	}

	embed := createTransitionEditorEmbed(ic.GuildID, row, tab, errorMessage)
	if err := discord.UpdateComponentMessage(s, ic, embed, createTransitionEditorComponents(ic.GuildID, row, tab, location)); err != nil {
		logger.Errorf("Failed to redraw the transition editor: %v", err)
	}
	return true
}

func loadTransitionRow(guildID string, songID int) (*transitionRow, *discordgo.MessageEmbed) {
	panel := &messages.T(guildID).AutoMixPanel

	state, isLoaded := loadPanelState(guildID)
	if !isLoaded {
		return nil, messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc)
	}
	pair := findTransitionPair(state.pairs, songID)
	if pair == nil {
		return nil, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone)
	}
	return hydrateTransitionRow(guildID, state, pair), nil
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
