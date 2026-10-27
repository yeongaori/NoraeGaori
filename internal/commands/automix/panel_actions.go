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
	discord.RegisterRoute(transitionPageRoute, turnTransitionPage)
	discord.RegisterRoute(transitionPickRoute, pickTransition)
	discord.RegisterRoute(transitionStyleRoute, chooseStyle)
	discord.RegisterRoute(transitionTabRoute, turnEditorTab)
	discord.RegisterRoute(mixingSettingsRoute, openMixingSettings)
	discord.RegisterRoute(panelOpenRoute, openPanel)
	discord.AttachDropdown("automix", panelOpenButtons)
}

func panelOpenButtons(guildID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.Button{
		Label:    messages.T(guildID).AutoMixPanel.OpenButton,
		Style:    discordgo.SecondaryButton,
		CustomID: panelOpenRoute,
	}}
}

func openPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, _ []string) {
	OpenPanelOnClick(s, ic)
}

func openMixingSettings(s *discordgo.Session, ic *discordgo.InteractionCreate, _ []string) {
	settings.OpenMixingPanel(s, ic)
}

func HandleAutoMixPanel(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	page := 1
	if options := i.ApplicationCommandData().Options; len(options) > 0 {
		page = int(options[0].IntValue())
	}

	embed, components, hasTransitions := preparePanel(s, i.GuildID, page)
	if !hasTransitions {
		discord.RespondEmbed(s, i, emptyPanelEmbed(i.GuildID))
		return nil
	}

	if _, err := discord.SendPanel(s, i, embed, components); err != nil {
		logger.Errorf("Failed to send panel: %v", err)
		return err
	}
	return nil
}

func OpenPanelOnClick(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	embed, components, hasTransitions := preparePanel(s, ic.GuildID, 1)
	if !hasTransitions {
		respondPanelNotice(s, ic, emptyPanelEmbed(ic.GuildID))
		return
	}

	if _, err := discord.SendPanel(s, ic, embed, components); err != nil {
		logger.Errorf("Failed to open panel from component: %v", err)
	}
}

func preparePanel(s *discordgo.Session, guildID string, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent, bool) {
	state, isLoaded := loadPanelState(guildID)
	if !isLoaded || len(state.pairs) == 0 {
		return nil, nil, false
	}

	if state.autoSelect {
		go player.StartBackfill(guildID, voiceChannelBitrate(s, guildID))
	}

	embed, components := renderTransitionPage(guildID, state, page)
	return embed, components, true
}

func renderPanel(guildID string, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	state, isLoaded := loadPanelState(guildID)
	if !isLoaded {
		return emptyPanelEmbed(guildID), nil
	}
	return renderTransitionPage(guildID, state, page)
}

func renderTransitionPage(guildID string, state *panelState, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	totalPages := transitionPageCount(state.pairs)
	page = discord.ClampPage(page, totalPages)
	rows := hydrateRows(guildID, state, transitionPageSlice(state.pairs, page))
	return createPanelEmbed(guildID, state, rows, page, totalPages),
		createPanelControls(guildID, rows, page, totalPages)
}

func emptyPanelEmbed(guildID string) *discordgo.MessageEmbed {
	panel := &messages.T(guildID).AutoMixPanel
	return messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc)
}

func respondPanelNotice(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if err := discord.RespondEphemeral(s, ic, embed); err != nil {
		logger.Errorf("Failed to report a transition panel notice: %v", err)
	}
}

func turnTransitionPage(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	page, hasPage := discord.PageArgument(ic, arguments, 1)
	if !hasPage {
		return
	}

	embed, components := renderPanel(ic.GuildID, page)
	if err := discord.UpdatePanel(s, ic, embed, components); err != nil {
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
	refreshPanel(s, ic, location)
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
	redrawEditor(s, ic, songID, location, tab, "")
}

func chooseStyle(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
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
		redrawEditor(s, ic, songID, location, tab, fmt.Sprintf(panel.UpdateFailed, style))
		return
	}

	if err := queue.SetSongOverrides(ic.GuildID, songID, category.Override(style)); err != nil {
		if errors.Is(err, queue.ErrSongNotInQueue) {
			closeEditor(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone))
			return
		}
		redrawEditor(s, ic, songID, location, tab, fmt.Sprintf(panel.UpdateFailed, err))
		return
	}

	if redrawEditor(s, ic, songID, location, tab, "") {
		refreshPanel(s, ic, location)
	}
}

func openTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, songID int, location *panelLocation) {
	row, notice := loadTransitionRow(ic.GuildID, songID)
	if notice != nil {
		respondPanelNotice(s, ic, notice)
		return
	}

	tab := &editorTabs[0]
	embed := createEditorEmbed(ic.GuildID, row, tab, "")
	if err := discord.RespondEphemeral(s, ic, embed, createEditorControls(ic.GuildID, row, tab, location)...); err != nil {
		logger.Errorf("Failed to open editor: %v", err)
	}
}

func redrawEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, songID int, location *panelLocation, tab *editorTab, errorMessage string) bool {
	row, notice := loadTransitionRow(ic.GuildID, songID)
	if notice != nil {
		closeEditor(s, ic, notice)
		return false
	}

	embed := createEditorEmbed(ic.GuildID, row, tab, errorMessage)
	if err := discord.UpdatePanel(s, ic, embed, createEditorControls(ic.GuildID, row, tab, location)); err != nil {
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

func closeEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if err := discord.UpdatePanel(s, ic, embed, nil); err != nil {
		logger.Errorf("Failed to close the transition editor: %v", err)
	}
}

func refreshPanel(s *discordgo.Session, ic *discordgo.InteractionCreate, location *panelLocation) {
	embed, components := renderPanel(ic.GuildID, location.page)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         location.messageID,
		Channel:    ic.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	}); err != nil {
		logger.Errorf("Failed to refresh the transition panel: %v", err)
	}
}
