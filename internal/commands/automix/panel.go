package automix

import (
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

const (
	transitionsPerPage = 5
	discordLabelLimit  = 100
	discordSelectLimit = 25
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

func hydrateTransitionRow(guildID string, state *panelState, pair transitionPair) transitionRow {
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

func hydrateTransitionRows(guildID string, state *panelState, pairs []transitionPair) []transitionRow {
	rows := make([]transitionRow, 0, len(pairs))
	for _, pair := range pairs {
		rows = append(rows, hydrateTransitionRow(guildID, state, pair))
	}
	return rows
}

func transitionPageSlice(pairs []transitionPair, page int) []transitionPair {
	start, end := discord.PageBounds(page, transitionsPerPage, len(pairs))
	if start == end {
		return nil
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
	return discord.PageCount(len(pairs), transitionsPerPage)
}

func autoStylesFor(state *panelState, row transitionRow) map[string]string {
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
	panel := &messages.T(guildID).AutoMixPanel
	if label, ok := panel.StyleLabels[category+"."+style]; ok && label != "" {
		return label
	}
	return style
}

func categoryLabel(guildID, category string) string {
	panel := &messages.T(guildID).AutoMixPanel
	if label, ok := panel.CategoryLabels[category]; ok && label != "" {
		return label
	}
	return category
}

func describeTrack(guildID string, track *analysis.TrackAnalysis, analyzing bool) string {
	panel := &messages.T(guildID).AutoMixPanel
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
	panel := &messages.T(guildID).AutoMixPanel
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

func createTransitionPanelEmbed(guildID string, state *panelState, rows []transitionRow, page, totalPages int) *discordgo.MessageEmbed {
	panel := &messages.T(guildID).AutoMixPanel

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

func createTransitionPanelComponents(guildID string, rows []transitionRow, page, totalPages int) []discordgo.MessageComponent {
	t := messages.T(guildID)
	pageArgument := strconv.Itoa(page)

	options := make([]discordgo.SelectMenuOption, 0, min(len(rows), discordSelectLimit))
	for index := range rows {
		if len(options) == discordSelectLimit {
			break
		}
		row := &rows[index]
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(transitionLabel(guildID, row.transitionPair), discordLabelLimit),
			Description: discord.TruncateRunes(describeRecipe(guildID, *row, false), discordLabelLimit),
			Value:       strconv.Itoa(row.fromSong.ID),
		})
	}

	refreshButton := discordgo.Button{
		Label:    t.AutoMixPanel.RefreshButton,
		Style:    discordgo.SecondaryButton,
		CustomID: discord.ComponentID(transitionPageRoute, pageArgument),
	}
	pageRow := discord.PageButtonRow(transitionPageRoute, page, totalPages, t.Buttons.Previous, t.Buttons.Next, nil, refreshButton)
	if len(options) == 0 {
		return []discordgo.MessageComponent{pageRow}
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    discord.ComponentID(transitionPickRoute, pageArgument),
					Placeholder: t.AutoMixPanel.SelectPlaceholder,
					Options:     options,
				},
			},
		},
		pageRow,
	}
}

func createTransitionEditorEmbed(guildID string, state *panelState, row transitionRow, errorMessage string) *discordgo.MessageEmbed {
	panel := &messages.T(guildID).AutoMixPanel

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
	panel := &messages.T(guildID).AutoMixPanel
	switch source {
	case "guild":
		return panel.SourceGuild
	case "song":
		return panel.SourceSong
	}
	return panel.SourceAuto
}

func createTransitionEditorComponents(guildID string, state *panelState, row transitionRow, location *panelLocation) []discordgo.MessageComponent {
	panel := &messages.T(guildID).AutoMixPanel
	autoStyles := autoStylesFor(state, row)
	songOverrides := songStyleOverrides(row.fromSong)
	songArgument := strconv.Itoa(row.fromSong.ID)
	pageArgument := strconv.Itoa(location.page)

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
					CustomID:    discord.ComponentID(transitionStyleRoute, category, songArgument, location.messageID, pageArgument),
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
