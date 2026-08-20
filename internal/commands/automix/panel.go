package automix

import (
	"errors"
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

const (
	transitionsPerPage    = 5
	transitionPanelExpiry = 5 * time.Minute
	transitionEditExpiry  = 5 * time.Minute
	discordLabelLimit     = 100
	discordSelectLimit    = 25
)

var transitionCategories = []string{"volume", "eq", "filter", "effect", "loop"}

type transitionPair struct {
	fromIndex int
	toIndex   int
	fromSong  *queue.Song
	toSong    *queue.Song
}

type transitionRow struct {
	transitionPair
	fromAnalysis  *analysis.TrackAnalysis
	toAnalysis    *analysis.TrackAnalysis
	effective     map[string]string
	source        map[string]string
	fromAnalyzing bool
	toAnalyzing   bool
}

type panelState struct {
	pairs          []transitionPair
	guildOverrides transition.StyleOverrides
	autoSelect     bool
	crossfade      bool
	autoMixBeats   int
	crossfadeSec   float64
	repeatSingle   bool
	backfillActive bool
}

func voiceChannelBitrate(s *discordgo.Session, guildID string) int {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.VoiceChannelID == "" {
		return 0
	}
	channel, err := s.Channel(q.VoiceChannelID)
	if err != nil || channel == nil {
		return 0
	}
	return channel.Bitrate
}

func newPanelToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func transitionPairs(songs []*queue.Song) []transitionPair {
	pairs := make([]transitionPair, 0, len(songs))
	for index := 0; index+1 < len(songs); index++ {
		from := songs[index]
		to := songs[index+1]
		if from.IsLive || to.IsLive {
			continue
		}
		pairs = append(pairs, transitionPair{
			fromIndex: index,
			toIndex:   index + 1,
			fromSong:  from,
			toSong:    to,
		})
	}

	if last := len(songs) - 1; last >= 0 && !songs[last].IsLive {
		pairs = append(pairs, transitionPair{
			fromIndex: last,
			toIndex:   -1,
			fromSong:  songs[last],
		})
	}

	return pairs
}

func (pair transitionPair) isOutro() bool {
	return pair.toSong == nil
}

func hydrateTransitionRow(guildID string, state panelState, pair transitionPair) transitionRow {
	fromAnalysis := player.LookupAnalysisForDisplay(guildID, pair.fromSong, analysis.SegmentTail)

	var toAnalysis *analysis.TrackAnalysis
	var recipe transition.Recipe
	var effective, source map[string]string

	if pair.isOutro() {
		recipe, effective, source = transition.ResolveOutroStyles(fromAnalysis, state.autoSelect,
			state.guildOverrides, songStyleOverrides(pair.fromSong))
	} else {
		toAnalysis = player.LookupAnalysisForDisplay(guildID, pair.toSong, analysis.SegmentHead)
		recipe, effective, source = transition.ResolveStyles(fromAnalysis, toAnalysis, state.autoSelect,
			state.guildOverrides, songStyleOverrides(pair.fromSong))
	}

	periodSec := 0.0
	if fromAnalysis != nil {
		periodSec = fromAnalysis.PeriodSec
	}
	crossfadeFrames, _ := transition.CrossfadeFrames(state.autoSelect, state.autoMixBeats, state.crossfadeSec, fromAnalysis)
	if clamped, _ := transition.ClampLoopStyle(recipe.Loop, periodSec, crossfadeFrames); clamped != recipe.Loop {
		effective["loop"] = clamped.String()
	}

	return transitionRow{
		transitionPair: pair,
		fromAnalysis:   fromAnalysis,
		toAnalysis:     toAnalysis,
		effective:      effective,
		source:         source,
		fromAnalyzing:  state.backfillActive && !player.AnalysisFailed(pair.fromSong.URL),
		toAnalyzing:    !pair.isOutro() && state.backfillActive && !player.AnalysisFailed(pair.toSong.URL),
	}
}

func hydrateTransitionRows(guildID string, state panelState, pairs []transitionPair) []transitionRow {
	rows := make([]transitionRow, 0, len(pairs))
	for _, pair := range pairs {
		rows = append(rows, hydrateTransitionRow(guildID, state, pair))
	}
	return rows
}

func transitionPageSlice(pairs []transitionPair, page int) []transitionPair {
	start := (page - 1) * transitionsPerPage
	if start < 0 || start >= len(pairs) {
		return nil
	}
	end := start + transitionsPerPage
	if end > len(pairs) {
		end = len(pairs)
	}
	return pairs[start:end]
}

func findTransitionPair(pairs []transitionPair, songID int) (transitionPair, bool) {
	for _, pair := range pairs {
		if pair.fromSong.ID == songID {
			return pair, true
		}
	}
	return transitionPair{}, false
}

func queueStyleOverrides(q *queue.Queue) transition.StyleOverrides {
	return transition.StyleOverrides{
		Volume: q.AutoMixStyleVolume,
		EQ:     q.AutoMixStyleEQ,
		Filter: q.AutoMixStyleFilter,
		Effect: q.AutoMixStyleEffect,
		Loop:   q.AutoMixStyleLoop,
	}
}

func songStyleOverrides(song *queue.Song) transition.StyleOverrides {
	if song == nil {
		return transition.StyleOverrides{}
	}
	return transition.StyleOverrides{
		Volume: song.AutoMixStyleVolume,
		EQ:     song.AutoMixStyleEQ,
		Filter: song.AutoMixStyleFilter,
		Effect: song.AutoMixStyleEffect,
		Loop:   song.AutoMixStyleLoop,
	}
}

func loadPanelState(guildID string) (panelState, bool) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		return panelState{}, false
	}
	return panelState{
		pairs:          transitionPairs(q.Songs),
		guildOverrides: queueStyleOverrides(q),
		autoSelect:     q.AutoMix,
		crossfade:      q.Crossfade,
		autoMixBeats:   q.AutoMixBeats,
		crossfadeSec:   q.CrossfadeDuration,
		repeatSingle:   q.RepeatMode == queue.RepeatSingle,
		backfillActive: player.AnalysisBackfillActive(guildID),
	}, true
}

func transitionPageCount(pairs []transitionPair) int {
	if len(pairs) == 0 {
		return 1
	}
	return (len(pairs) + transitionsPerPage - 1) / transitionsPerPage
}

func autoStylesFor(state panelState, row transitionRow) map[string]string {
	if !state.autoSelect {
		_, styles, _ := transition.ResolveStyles(nil, nil, false,
			transition.StyleOverrides{}, transition.StyleOverrides{})
		return styles
	}
	if row.isOutro() {
		return transition.AutoOutroStyles(row.fromAnalysis)
	}
	return transition.AutoStyles(row.fromAnalysis, row.toAnalysis)
}

func styleLabel(guildID, category, style string) string {
	panel := messages.T(guildID).AutoMixPanel
	if label, ok := panel.StyleLabels[category+"."+style]; ok && label != "" {
		return label
	}
	return style
}

func categoryLabel(guildID, category string) string {
	panel := messages.T(guildID).AutoMixPanel
	if label, ok := panel.CategoryLabels[category]; ok && label != "" {
		return label
	}
	return category
}

func describeTrack(guildID string, track *analysis.TrackAnalysis, analyzing bool) string {
	panel := messages.T(guildID).AutoMixPanel
	if track == nil {
		if analyzing {
			return panel.Analyzing
		}
		return panel.Unknown
	}

	bpm, key, camelot, hasKey := analysis.Summarize(track)
	if bpm <= 0 {
		if analyzing {
			return panel.Analyzing
		}
		return panel.Unknown
	}
	if !hasKey {
		return fmt.Sprintf("%.1f BPM · %s", bpm, panel.Unknown)
	}
	return fmt.Sprintf("%.1f BPM · %s (%s)", bpm, key, camelot)
}

func describeRecipe(guildID string, row transitionRow, marked bool) string {
	panel := messages.T(guildID).AutoMixPanel
	parts := make([]string, 0, len(transitionCategories))
	for _, category := range transitionCategories {
		style := row.effective[category]
		label := styleLabel(guildID, category, style)
		if style == "none" {
			label = panel.Unknown
		}
		if marked && row.source[category] != "auto" {
			label += " " + panel.OverrideMarker
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " · ")
}

func transitionLabel(guildID string, pair transitionPair) string {
	if pair.isOutro() {
		return fmt.Sprintf("%d → %s", pair.fromIndex+1, messages.T(guildID).AutoMixPanel.OutroLabel)
	}
	return fmt.Sprintf("%d → %d", pair.fromIndex+1, pair.toIndex+1)
}

func describeSongLine(guildID string, index int, song *queue.Song, analysis *analysis.TrackAnalysis, analyzing bool) string {
	return fmt.Sprintf("**%d.** %s · %s\n\n",
		index+1,
		messages.EscapeMarkdown(discord.TruncateRunes(song.Title, 50)),
		describeTrack(guildID, analysis, analyzing))
}

func createTransitionPanelEmbed(guildID string, state panelState, rows []transitionRow, page, totalPages int) *discordgo.MessageEmbed {
	panel := messages.T(guildID).AutoMixPanel

	if len(rows) == 0 {
		return messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc)
	}

	var description strings.Builder
	description.WriteString(panel.Description)
	description.WriteString("\n")
	if !state.autoSelect && !state.crossfade {
		description.WriteString(panel.TransitionsOffNotice)
		description.WriteString("\n")
	} else if !state.autoSelect {
		description.WriteString(panel.AutoMixOffNotice)
		description.WriteString("\n")
	}
	if state.repeatSingle {
		description.WriteString(panel.RepeatSingleNotice)
		description.WriteString("\n")
	}
	description.WriteString("\n")

	previousIndex := -1
	for _, row := range rows {
		if row.fromIndex != previousIndex {
			if previousIndex >= 0 {
				description.WriteString("\n")
			}
			description.WriteString(describeSongLine(guildID, row.fromIndex, row.fromSong, row.fromAnalysis, row.fromAnalyzing))
		}

		marker := "　"
		if row.fromIndex == 0 {
			marker = panel.NowMarker
		}
		description.WriteString(fmt.Sprintf("%s **%s** · %s\n\n",
			marker, transitionLabel(guildID, row.transitionPair), describeRecipe(guildID, row, true)))

		if row.isOutro() {
			previousIndex = -1
			continue
		}

		description.WriteString(describeSongLine(guildID, row.toIndex, row.toSong, row.toAnalysis, row.toAnalyzing))
		previousIndex = row.toIndex
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       panel.Title,
		Description: description.String(),
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("%s | %d/%d", panel.Legend, page, totalPages),
		},
	}
}

func createTransitionPanelComponents(guildID string, rows []transitionRow, page, totalPages int, token string) []discordgo.MessageComponent {
	panel := messages.T(guildID).AutoMixPanel
	buttons := messages.T(guildID).Buttons

	options := make([]discordgo.SelectMenuOption, 0, transitionsPerPage)
	for _, row := range rows {
		if len(options) >= discordSelectLimit {
			break
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(transitionLabel(guildID, row.transitionPair), discordLabelLimit),
			Description: discord.TruncateRunes(describeRecipe(guildID, row, false), discordLabelLimit),
			Value:       strconv.Itoa(row.fromSong.ID),
		})
	}

	components := make([]discordgo.MessageComponent, 0, 2)
	if len(options) > 0 {
		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    "automix_pick_" + token,
					Placeholder: panel.SelectPlaceholder,
					Options:     options,
				},
			},
		})
	}

	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    buttons.Previous,
				Style:    discordgo.PrimaryButton,
				CustomID: "automix_panel_prev_" + token,
				Disabled: page <= 1,
			},
			discordgo.Button{
				Label:    buttons.Next,
				Style:    discordgo.PrimaryButton,
				CustomID: "automix_panel_next_" + token,
				Disabled: page >= totalPages,
			},
			discordgo.Button{
				Label:    panel.RefreshButton,
				Style:    discordgo.SecondaryButton,
				CustomID: "automix_panel_refresh_" + token,
			},
		},
	})

	return components
}

func createTransitionEditorEmbed(guildID string, state panelState, row transitionRow, errorMessage string) *discordgo.MessageEmbed {
	panel := messages.T(guildID).AutoMixPanel

	autoStyles := autoStylesFor(state, row)
	autoParts := make([]string, 0, len(transitionCategories))
	effectiveParts := make([]string, 0, len(transitionCategories))
	for _, category := range transitionCategories {
		autoParts = append(autoParts, fmt.Sprintf("%s: %s",
			categoryLabel(guildID, category), styleLabel(guildID, category, autoStyles[category])))
		effectiveParts = append(effectiveParts, fmt.Sprintf("%s: %s (%s)",
			categoryLabel(guildID, category),
			styleLabel(guildID, category, row.effective[category]),
			sourceLabel(guildID, row.source[category])))
	}

	compatibility := panel.Unknown
	if delta, distance, ok := analysis.Compare(row.fromAnalysis, row.toAnalysis); ok {
		compatibility = fmt.Sprintf(panel.BPMDelta, delta*100)
		if distance >= 0 {
			verdict := panel.Clashing
			if distance <= 1 {
				verdict = panel.Harmonic
			}
			compatibility += fmt.Sprintf(" · %s (%s)", fmt.Sprintf(panel.CamelotDistance, distance), verdict)
		}
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name: fmt.Sprintf("%s (#%d)", panel.OutgoingField, row.fromIndex+1),
			Value: fmt.Sprintf("%s\n%s",
				messages.EscapeMarkdown(discord.TruncateRunes(row.fromSong.Title, 80)),
				describeTrack(guildID, row.fromAnalysis, row.fromAnalyzing)),
			Inline: false,
		},
	}

	if row.isOutro() {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   panel.IncomingField,
			Value:  panel.OutroField,
			Inline: false,
		})
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: fmt.Sprintf("%s (#%d)", panel.IncomingField, row.toIndex+1),
			Value: fmt.Sprintf("%s\n%s",
				messages.EscapeMarkdown(discord.TruncateRunes(row.toSong.Title, 80)),
				describeTrack(guildID, row.toAnalysis, row.toAnalyzing)),
			Inline: false,
		})
	}

	fields = append(fields,
		&discordgo.MessageEmbedField{Name: panel.CompatibilityField, Value: compatibility, Inline: false},
		&discordgo.MessageEmbedField{Name: panel.AutoRecipeField, Value: strings.Join(autoParts, "\n"), Inline: false},
		&discordgo.MessageEmbedField{Name: panel.EffectiveField, Value: strings.Join(effectiveParts, "\n"), Inline: false},
	)
	if errorMessage != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   messages.T(guildID).Titles.Error,
			Value:  discord.TruncateRunes(errorMessage, 1024),
			Inline: false,
		})
	}

	return &discordgo.MessageEmbed{
		Color:  messages.ColorInfo,
		Title:  discord.TruncateRunes(fmt.Sprintf(panel.EditorTitle, transitionLabel(guildID, row.transitionPair)), 256),
		Fields: fields,
	}
}

func sourceLabel(guildID, source string) string {
	panel := messages.T(guildID).AutoMixPanel
	switch source {
	case "guild":
		return panel.SourceGuild
	case "song":
		return panel.SourceSong
	}
	return panel.SourceAuto
}

func createTransitionEditorComponents(guildID string, state panelState, row transitionRow, token string) []discordgo.MessageComponent {
	panel := messages.T(guildID).AutoMixPanel
	autoStyles := autoStylesFor(state, row)
	songOverrides := songStyleOverrides(row.fromSong)

	components := make([]discordgo.MessageComponent, 0, len(transitionCategories))
	for _, category := range transitionCategories {
		current := overrideForCategory(songOverrides, category)
		if current == "" {
			current = queue.AutoMixStyleAuto
		}

		prefix := categoryLabel(guildID, category) + ": "

		options := []discordgo.SelectMenuOption{{
			Label:       discord.TruncateRunes(prefix+panel.AutoOption, discordLabelLimit),
			Description: discord.TruncateRunes(fmt.Sprintf(panel.AutoOptionDesc, styleLabel(guildID, category, autoStyles[category])), discordLabelLimit),
			Value:       queue.AutoMixStyleAuto,
			Default:     current == queue.AutoMixStyleAuto,
		}}

		for _, style := range transition.StyleValues(category) {
			if style == queue.AutoMixStyleAuto {
				continue
			}
			options = append(options, discordgo.SelectMenuOption{
				Label:   discord.TruncateRunes(prefix+styleLabel(guildID, category, style), discordLabelLimit),
				Value:   style,
				Default: current == style,
			})
		}

		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    fmt.Sprintf("automix_style_%s_%s_%d", category, token, row.fromSong.ID),
					Placeholder: categoryLabel(guildID, category),
					Options:     options,
				},
			},
		})
	}

	return components
}

func overrideForCategory(overrides transition.StyleOverrides, category string) string {
	switch category {
	case "volume":
		return overrides.Volume
	case "eq":
		return overrides.EQ
	case "filter":
		return overrides.Filter
	case "effect":
		return overrides.Effect
	case "loop":
		return overrides.Loop
	}
	return ""
}

func HandleAutoMixPanel(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	panel := messages.T(guildID).AutoMixPanel

	state, ok := loadPanelState(guildID)
	if !ok || len(state.pairs) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc))
		return nil
	}

	go player.StartAnalysisBackfill(guildID, voiceChannelBitrate(s, guildID))

	totalPages := transitionPageCount(state.pairs)
	page := 1
	if options := i.ApplicationCommandData().Options; len(options) > 0 {
		page = int(options[0].IntValue())
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	token := newPanelToken()
	rows := hydrateTransitionRows(guildID, state, transitionPageSlice(state.pairs, page))
	embed := createTransitionPanelEmbed(guildID, state, rows, page, totalPages)
	components := createTransitionPanelComponents(guildID, rows, page, totalPages, token)

	msg, err := discord.RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		logger.Errorf("Failed to send panel: %v", err)
		return err
	}

	go runTransitionPanel(s, msg, guildID, token, page)
	return nil
}

func OpenPanelFromComponent(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	guildID := ic.GuildID
	panel := messages.T(guildID).AutoMixPanel

	state, ok := loadPanelState(guildID)
	if !ok || len(state.pairs) == 0 {
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc)},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	go player.StartAnalysisBackfill(guildID, voiceChannelBitrate(s, guildID))

	totalPages := transitionPageCount(state.pairs)
	token := newPanelToken()
	rows := hydrateTransitionRows(guildID, state, transitionPageSlice(state.pairs, 1))
	embed := createTransitionPanelEmbed(guildID, state, rows, 1, totalPages)
	components := createTransitionPanelComponents(guildID, rows, 1, totalPages, token)

	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	}); err != nil {
		logger.Errorf("Failed to open panel from component: %v", err)
		return
	}

	msg, err := s.InteractionResponse(ic.Interaction)
	if err != nil {
		logger.Errorf("Failed to resolve panel message: %v", err)
		return
	}

	go runTransitionPanel(s, msg, guildID, token, 1)
}

func runTransitionPanel(s *discordgo.Session, panelMsg *discordgo.Message, guildID, token string, startPage int) {
	if panelMsg == nil {
		return
	}

	timeout := time.After(transitionPanelExpiry)
	currentPage := startPage
	var pageMu sync.Mutex
	var closeMu sync.Mutex
	panelClosed := false

	renderPanel := func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		state, ok := loadPanelState(guildID)
		if !ok {
			panel := messages.T(guildID).AutoMixPanel
			return messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc), nil
		}
		totalPages := transitionPageCount(state.pairs)

		pageMu.Lock()
		if currentPage > totalPages {
			currentPage = totalPages
		}
		if currentPage < 1 {
			currentPage = 1
		}
		page := currentPage
		pageMu.Unlock()

		rows := hydrateTransitionRows(guildID, state, transitionPageSlice(state.pairs, page))
		return createTransitionPanelEmbed(guildID, state, rows, page, totalPages),
			createTransitionPanelComponents(guildID, rows, page, totalPages, token)
	}

	refreshPanelMessage := func() {
		embed, components := renderPanel()

		closeMu.Lock()
		defer closeMu.Unlock()
		if panelClosed {
			return
		}
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         panelMsg.ID,
			Channel:    panelMsg.ChannelID,
			Embeds:     &[]*discordgo.MessageEmbed{embed},
			Components: &components,
		})
	}

	handler := func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic.Type != discordgo.InteractionMessageComponent {
			return
		}
		if ic.Message == nil || ic.Message.ID != panelMsg.ID {
			return
		}

		data := ic.MessageComponentData()
		if !strings.HasSuffix(data.CustomID, token) {
			return
		}

		switch {
		case strings.HasPrefix(data.CustomID, "automix_panel_prev_"):
			pageMu.Lock()
			if currentPage > 1 {
				currentPage--
			}
			pageMu.Unlock()
		case strings.HasPrefix(data.CustomID, "automix_panel_next_"):
			pageMu.Lock()
			currentPage++
			pageMu.Unlock()
		case strings.HasPrefix(data.CustomID, "automix_panel_refresh_"):
		case strings.HasPrefix(data.CustomID, "automix_pick_"):
			if len(data.Values) == 0 {
				return
			}
			songID, err := strconv.Atoi(data.Values[0])
			if err != nil {
				return
			}
			openTransitionEditor(s, ic, guildID, songID, refreshPanelMessage)
			refreshPanelMessage()
			return
		default:
			return
		}

		embed, components := renderPanel()
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	}

	removeHandler := s.AddHandler(handler)
	defer removeHandler()

	<-timeout

	embed, _ := renderPanel()

	closeMu.Lock()
	panelClosed = true
	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         panelMsg.ID,
		Channel:    panelMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	})
	closeMu.Unlock()
}

func openTransitionEditor(s *discordgo.Session, ic *discordgo.InteractionCreate, guildID string, songID int, refreshPanel func()) {
	panel := messages.T(guildID).AutoMixPanel

	state, ok := loadPanelState(guildID)
	if !ok {
		respondEditorClosed(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc))
		return
	}
	pair, found := findTransitionPair(state.pairs, songID)
	if !found {
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone)},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	row := hydrateTransitionRow(guildID, state, pair)
	token := newPanelToken()
	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{createTransitionEditorEmbed(guildID, state, row, "")},
			Components: createTransitionEditorComponents(guildID, state, row, token),
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		logger.Errorf("Failed to open editor: %v", err)
		return
	}

	editorMsg, err := s.InteractionResponse(ic.Interaction)
	if err != nil {
		logger.Errorf("Failed to resolve editor message: %v", err)
		return
	}

	go runTransitionEditor(s, ic.Interaction, editorMsg.ID, guildID, songID, token, refreshPanel)
}

func runTransitionEditor(s *discordgo.Session, origin *discordgo.Interaction, editorMsgID, guildID string, songID int, token string, refreshPanel func()) {
	timeout := time.After(transitionEditExpiry)
	suffix := fmt.Sprintf("_%s_%d", token, songID)

	handler := func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic.Type != discordgo.InteractionMessageComponent {
			return
		}
		if ic.Message == nil || ic.Message.ID != editorMsgID {
			return
		}

		data := ic.MessageComponentData()
		if !strings.HasPrefix(data.CustomID, "automix_style_") || !strings.HasSuffix(data.CustomID, suffix) {
			return
		}
		if len(data.Values) == 0 {
			return
		}

		category := strings.TrimSuffix(strings.TrimPrefix(data.CustomID, "automix_style_"), suffix)
		style := data.Values[0]

		panel := messages.T(guildID).AutoMixPanel
		if !transition.ValidStyle(category, style) {
			respondEditorRetry(s, ic, guildID, songID, token, fmt.Sprintf(panel.UpdateFailed, style))
			return
		}

		if err := queue.SetSongAutoMixStyle(guildID, songID, category, style); err != nil {
			if errors.Is(err, queue.ErrSongNotInQueue) {
				respondEditorClosed(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone))
				return
			}
			respondEditorRetry(s, ic, guildID, songID, token, fmt.Sprintf(panel.UpdateFailed, err))
			return
		}

		state, ok := loadPanelState(guildID)
		if !ok {
			respondEditorClosed(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc))
			return
		}
		pair, found := findTransitionPair(state.pairs, songID)
		if !found {
			respondEditorClosed(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone))
			return
		}

		row := hydrateTransitionRow(guildID, state, pair)
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{createTransitionEditorEmbed(guildID, state, row, "")},
				Components: createTransitionEditorComponents(guildID, state, row, token),
			},
		})

		if refreshPanel != nil {
			refreshPanel()
		}
	}

	removeHandler := s.AddHandler(handler)
	defer removeHandler()

	<-timeout

	if origin != nil {
		s.InteractionResponseEdit(origin, &discordgo.WebhookEdit{
			Components: &[]discordgo.MessageComponent{},
		})
	}
}

func respondEditorRetry(s *discordgo.Session, ic *discordgo.InteractionCreate, guildID string, songID int, token, message string) {
	panel := messages.T(guildID).AutoMixPanel

	state, ok := loadPanelState(guildID)
	if !ok {
		respondEditorClosed(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.EmptyDesc))
		return
	}
	pair, found := findTransitionPair(state.pairs, songID)
	if !found {
		respondEditorClosed(s, ic, messages.CreateErrorEmbed(panel.EmptyTitle, panel.SongGone))
		return
	}

	row := hydrateTransitionRow(guildID, state, pair)
	s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{createTransitionEditorEmbed(guildID, state, row, message)},
			Components: createTransitionEditorComponents(guildID, state, row, token),
		},
	})
}

func respondEditorClosed(s *discordgo.Session, ic *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{},
		},
	})
}
