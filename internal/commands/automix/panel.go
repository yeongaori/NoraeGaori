package automix

import (
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"noraegaori/internal/youtube"
	"slices"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

const (
	transitionsPerPage = 5
	discordLabelLimit  = 100
	discordSelectLimit = 25
	tabOutgoing        = "outgoing"
	tabIncoming        = "incoming"
	tabSettings        = "settings"
	sourceAuto         = "auto"
	sourceSong         = "song"
)

type editorTab struct {
	key        string
	categories []transition.Category
}

var editorTabs = []editorTab{
	{tabOutgoing, []transition.Category{
		transition.CategoryVolumeOut, transition.CategoryEQOut, transition.CategoryFilterOut, transition.CategoryFXOut,
	}},
	{tabIncoming, []transition.Category{
		transition.CategoryVolumeIn, transition.CategoryEQIn, transition.CategoryFilterIn, transition.CategoryFXIn,
	}},
	{tabSettings, []transition.Category{
		transition.CategoryPreset, transition.CategoryLength, transition.CategoryBeatmatch, transition.CategoryLoop,
	}},
}

var outroSettingsCategories = []transition.Category{transition.CategoryLoop}

func findTab(key string) *editorTab {
	for index := range editorTabs {
		if editorTabs[index].key == key {
			return &editorTabs[index]
		}
	}
	return nil
}

func tabOfCategory(category transition.Category) *editorTab {
	for index := range editorTabs {
		if slices.Contains(editorTabs[index].categories, category) {
			return &editorTabs[index]
		}
	}
	return nil
}

type transitionPair struct {
	fromIndex int
	toIndex   int
	fromSong  *queue.Song
	toSong    *queue.Song
}

type transitionRow struct {
	*transitionPair
	overlap       *transition.Overlap
	auto          *transition.Recipe
	resolved      *transition.Resolved
	fromAnalysis  *analysis.TrackAnalysis
	toAnalysis    *analysis.TrackAnalysis
	fromAnalyzing bool
	toAnalyzing   bool
}

type panelState struct {
	pairs          []transitionPair
	guildOverrides map[string]string
	autoSelect     bool
	crossfade      bool
	autoMixBeats   int
	crossfadeSec   float64
	repeatSingle   bool
	pending        map[int]int
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

func (pair *transitionPair) isOutro() bool {
	return pair.toSong == nil
}

func analysisPeriod(track *analysis.TrackAnalysis) float64 {
	if track == nil {
		return 0
	}
	return track.PeriodSec
}

func hydrateTransitionRow(guildID string, state *panelState, pair *transitionPair) *transitionRow {
	row := &transitionRow{
		transitionPair: pair,
		auto:           transition.PresetRecipe(transition.NoPreset),
		fromAnalysis:   player.LookupAnalysisForDisplay(guildID, pair.fromSong, analysis.SegmentTail),
		fromAnalyzing:  state.pending[pair.fromSong.ID] > 0,
	}
	frames, _ := transition.CrossfadeFrames(state.autoSelect, state.autoMixBeats, state.crossfadeSec, row.fromAnalysis)

	if pair.isOutro() {
		if state.autoSelect {
			row.auto = transition.OutroRecipe()
		}
	} else {
		row.toAnalysis = player.LookupAnalysisForDisplay(guildID, pair.toSong, analysis.SegmentHead)
		row.toAnalyzing = state.pending[pair.toSong.ID] > 0
		if state.autoSelect {
			row.overlap = previewOverlap(pair, row.fromAnalysis, row.toAnalysis, state.autoMixBeats)
		}
		if row.overlap != nil {
			row.auto = transition.PresetRecipe(row.overlap.Preset)
			frames = int(row.overlap.Length * dsp.FramesPerSecond)
		}
	}

	row.resolved = transition.ResolveStyles(row.auto, state.guildOverrides, pair.fromSong.AutoMixOverrides)
	row.resolved.ClampRolls(analysisPeriod(row.fromAnalysis), frames)
	return row
}

func hydrateTransitionRows(guildID string, state *panelState, pairs []transitionPair) []*transitionRow {
	rows := make([]*transitionRow, 0, len(pairs))
	for index := range pairs {
		rows = append(rows, hydrateTransitionRow(guildID, state, &pairs[index]))
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

func findTransitionPair(pairs []transitionPair, songID int) *transitionPair {
	for index := range pairs {
		if pairs[index].fromSong.ID == songID {
			return &pairs[index]
		}
	}
	return nil
}

func loadPanelState(guildID string) (*panelState, bool) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		return nil, false
	}
	return &panelState{
		pairs:          transitionPairs(q.Songs),
		guildOverrides: q.AutoMixOverrides,
		autoSelect:     q.AutoMix,
		crossfade:      q.Crossfade,
		autoMixBeats:   q.AutoMixBeats,
		crossfadeSec:   q.CrossfadeDuration,
		repeatSingle:   q.RepeatMode == queue.RepeatSingle,
		pending:        player.PendingAnalyses(guildID),
	}, true
}

func transitionPageCount(pairs []transitionPair) int {
	return discord.PageCount(len(pairs), transitionsPerPage)
}

func previewOverlap(pair *transitionPair, from, to *analysis.TrackAnalysis, maxBeats int) *transition.Overlap {
	overlap, ok := transition.SelectOverlap(&transition.Pair{
		From:     previewTrack(pair.fromSong, from),
		To:       previewTrack(pair.toSong, to),
		MaxBeats: maxBeats,
		Settings: transition.ResolveSettings(pair.fromSong.AutoMixOverrides),
	})
	if !ok {
		return nil
	}
	return &overlap
}

func previewTrack(song *queue.Song, trackAnalysis *analysis.TrackAnalysis) transition.Track {
	return transition.Track{
		URL:      song.URL,
		Duration: float64(youtube.ParseDurationToSeconds(song.Duration)),
		Analysis: trackAnalysis,
	}
}

func styleLabelKey(category transition.Category, style string) string {
	kind := strings.TrimSuffix(strings.TrimSuffix(string(category), "_out"), "_in")
	return kind + "." + style
}

func lookupLabel(guildID string, labels map[string]string, key, fallback string) string {
	if label := labels[key]; label != "" {
		return label
	}
	logger.Errorf("AutoMix panel label %q is missing from the %s locale", key, messages.Lang(guildID))
	return fallback
}

func styleLabel(guildID string, category transition.Category, style string) string {
	return lookupLabel(guildID, messages.T(guildID).AutoMixPanel.StyleLabels, styleLabelKey(category, style), style)
}

func categoryLabel(guildID string, category transition.Category) string {
	return lookupLabel(guildID, messages.T(guildID).AutoMixPanel.CategoryLabels, string(category), string(category))
}

func MissingLabels(panel *messages.AutoMixPanelMessages) []string {
	var missing []string
	for _, category := range slices.Concat(transition.StyleCategories(), transition.SettingCategories()) {
		if panel.CategoryLabels[string(category)] == "" {
			missing = append(missing, string(category))
		}
		for _, style := range transition.StyleValues(category)[1:] {
			if key := styleLabelKey(category, style); panel.StyleLabels[key] == "" && !slices.Contains(missing, key) {
				missing = append(missing, key)
			}
		}
	}
	return missing
}

func reportMissingLabels() {
	for _, lang := range messages.AvailableLocales() {
		for _, key := range MissingLabels(&messages.ForLang(lang).AutoMixPanel) {
			logger.Errorf("AutoMix panel label %q is missing from the %s locale", key, lang)
		}
	}
}

func tabLabel(guildID, tab string) string {
	panel := &messages.T(guildID).AutoMixPanel
	switch tab {
	case tabOutgoing:
		return panel.OutgoingField
	case tabIncoming:
		return panel.IncomingField
	}
	return panel.SettingsTab
}

func (row *transitionRow) source(category transition.Category) string {
	if category.IsStyle() {
		return row.resolved.Sources[category]
	}
	if row.override(category) != transition.StyleAuto {
		return sourceSong
	}
	return sourceAuto
}

func (row *transitionRow) override(category transition.Category) string {
	if value, ok := row.fromSong.AutoMixOverrides[string(category)]; ok && transition.ValidStyle(category, value) {
		return value
	}
	return transition.StyleAuto
}

func (row *transitionRow) tabCategories(tab *editorTab) []transition.Category {
	if row.isOutro() && tab.key == tabSettings {
		return outroSettingsCategories
	}
	return tab.categories
}

func autoValueLabel(guildID string, row *transitionRow, category transition.Category) string {
	if category.IsStyle() {
		return styleLabel(guildID, category, transition.StyleOf(row.auto, category))
	}
	if row.overlap == nil {
		return messages.T(guildID).AutoMixPanel.Unknown
	}
	switch category {
	case transition.CategoryPreset:
		return styleLabel(guildID, category, strconv.Itoa(row.overlap.Preset))
	case transition.CategoryLength:
		return lengthLabel(guildID, row.overlap)
	}
	if row.overlap.Beatmatched {
		return styleLabel(guildID, category, "on")
	}
	return styleLabel(guildID, category, "off")
}

func effectiveValueLabel(guildID string, row *transitionRow, category transition.Category) string {
	if category.IsStyle() {
		return styleLabel(guildID, category, transition.StyleOf(&row.resolved.Recipe, category))
	}
	if value := row.override(category); value != transition.StyleAuto {
		return styleLabel(guildID, category, value)
	}
	return autoValueLabel(guildID, row, category)
}

func lengthLabel(guildID string, overlap *transition.Overlap) string {
	panel := &messages.T(guildID).AutoMixPanel
	if overlap.Bars > 0 {
		return fmt.Sprintf(panel.BarsFormat, overlap.Bars)
	}
	return fmt.Sprintf(panel.SecondsFormat, overlap.Length)
}

func describeOverlap(guildID string, row *transitionRow) []string {
	panel := &messages.T(guildID).AutoMixPanel
	if row.isOutro() {
		if row.resolved.Recipe.IsOutroDefault() {
			return []string{panel.EndsNaturally}
		}
		return nil
	}
	if row.overlap == nil {
		return nil
	}
	match := panel.NotBeatmatched
	if row.overlap.Beatmatched {
		match = fmt.Sprintf(panel.Beatmatched, row.overlap.SpeedB*100)
	}
	return []string{
		styleLabel(guildID, transition.CategoryPreset, strconv.Itoa(row.overlap.Preset)),
		lengthLabel(guildID, row.overlap),
		match,
	}
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

func describeRecipe(guildID string, row *transitionRow, marked bool) string {
	panel := &messages.T(guildID).AutoMixPanel
	parts := describeOverlap(guildID, row)
	for index := range editorTabs {
		for _, category := range row.tabCategories(&editorTabs[index]) {
			if row.source(category) == sourceAuto {
				continue
			}
			label := categoryLabel(guildID, category) + ": " + effectiveValueLabel(guildID, row, category)
			if marked {
				label += " " + panel.OverrideMarker
			}
			parts = append(parts, label)
		}
	}
	if len(parts) == 0 {
		return panel.AutoOption
	}
	return strings.Join(parts, " · ")
}

func transitionLabel(guildID string, pair *transitionPair) string {
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

func createTransitionPanelEmbed(guildID string, state *panelState, rows []*transitionRow, page, totalPages int) *discordgo.MessageEmbed {
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

func createTransitionPanelComponents(guildID string, rows []*transitionRow, page, totalPages int) []discordgo.MessageComponent {
	t := messages.T(guildID)
	pageArgument := strconv.Itoa(page)

	options := make([]discordgo.SelectMenuOption, 0, min(len(rows), discordSelectLimit))
	for _, row := range rows {
		if len(options) == discordSelectLimit {
			break
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(transitionLabel(guildID, row.transitionPair), discordLabelLimit),
			Description: discord.TruncateRunes(describeRecipe(guildID, row, false), discordLabelLimit),
			Value:       strconv.Itoa(row.fromSong.ID),
		})
	}

	refreshButton := discordgo.Button{
		Label:    t.AutoMixPanel.RefreshButton,
		Style:    discordgo.SecondaryButton,
		CustomID: discord.ComponentID(transitionPageRoute, pageArgument),
	}
	mixingButton := discordgo.Button{
		Label:    t.AutoMixPanel.MixingButton,
		Style:    discordgo.SecondaryButton,
		CustomID: mixingSettingsRoute,
	}
	pageRow := discord.PageButtonRow(transitionPageRoute, page, totalPages, t.Buttons.Previous, t.Buttons.Next, nil, refreshButton, mixingButton)
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

func describeCategories(guildID string, categories []transition.Category, describe func(category transition.Category) string) string {
	lines := make([]string, 0, len(categories))
	for _, category := range categories {
		lines = append(lines, categoryLabel(guildID, category)+": "+describe(category))
	}
	return strings.Join(lines, "\n")
}

func createTransitionEditorEmbed(guildID string, row *transitionRow, tab *editorTab, errorMessage string) *discordgo.MessageEmbed {
	panel := &messages.T(guildID).AutoMixPanel
	categories := row.tabCategories(tab)

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

	incoming := &discordgo.MessageEmbedField{Name: panel.IncomingField, Value: panel.OutroField}
	if !row.isOutro() {
		incoming = &discordgo.MessageEmbedField{
			Name: fmt.Sprintf("%s (#%d)", panel.IncomingField, row.toIndex+1),
			Value: fmt.Sprintf("%s\n%s",
				messages.EscapeMarkdown(discord.TruncateRunes(row.toSong.Title, 80)),
				describeTrack(guildID, row.toAnalysis, row.toAnalyzing)),
		}
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name: fmt.Sprintf("%s (#%d)", panel.OutgoingField, row.fromIndex+1),
			Value: fmt.Sprintf("%s\n%s",
				messages.EscapeMarkdown(discord.TruncateRunes(row.fromSong.Title, 80)),
				describeTrack(guildID, row.fromAnalysis, row.fromAnalyzing)),
		},
		incoming,
		{Name: panel.CompatibilityField, Value: compatibility},
	}
	if overlap := describeOverlap(guildID, row); len(overlap) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{Name: panel.OverlapField, Value: strings.Join(overlap, "\n")})
	}
	fields = append(fields,
		&discordgo.MessageEmbedField{
			Name: panel.AutoRecipeField,
			Value: describeCategories(guildID, categories, func(category transition.Category) string {
				return autoValueLabel(guildID, row, category)
			}),
		},
		&discordgo.MessageEmbedField{
			Name: panel.EffectiveField,
			Value: describeCategories(guildID, categories, func(category transition.Category) string {
				return fmt.Sprintf("%s (%s)", effectiveValueLabel(guildID, row, category), sourceLabel(guildID, row.source(category)))
			}),
		},
	)
	if errorMessage != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  messages.T(guildID).Titles.Error,
			Value: discord.TruncateRunes(errorMessage, 1024),
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
	case sourceSong:
		return panel.SourceSong
	}
	return panel.SourceAuto
}

func createTransitionEditorComponents(guildID string, row *transitionRow, tab *editorTab, location *panelLocation) []discordgo.MessageComponent {
	panel := &messages.T(guildID).AutoMixPanel
	songArgument := strconv.Itoa(row.fromSong.ID)
	pageArgument := strconv.Itoa(location.page)
	categories := row.tabCategories(tab)

	components := make([]discordgo.MessageComponent, 0, len(categories)+1)
	for _, category := range categories {
		current := row.override(category)
		prefix := categoryLabel(guildID, category) + ": "
		values := transition.StyleValues(category)

		options := make([]discordgo.SelectMenuOption, 0, len(values))
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(prefix+panel.AutoOption, discordLabelLimit),
			Description: discord.TruncateRunes(fmt.Sprintf(panel.AutoOptionDesc, autoValueLabel(guildID, row, category)), discordLabelLimit),
			Value:       transition.StyleAuto,
			Default:     current == transition.StyleAuto,
		})
		for _, style := range values[1:] {
			options = append(options, discordgo.SelectMenuOption{
				Label:   discord.TruncateRunes(prefix+styleLabel(guildID, category, style), discordLabelLimit),
				Value:   style,
				Default: current == style,
			})
		}

		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    discord.ComponentID(transitionStyleRoute, string(category), songArgument, location.messageID, pageArgument),
					Placeholder: categoryLabel(guildID, category),
					Options:     options,
				},
			},
		})
	}

	return append(components, editorTabRow(guildID, row, tab, songArgument, location))
}

func editorTabRow(guildID string, row *transitionRow, tab *editorTab, songArgument string, location *panelLocation) discordgo.ActionsRow {
	pageArgument := strconv.Itoa(location.page)
	buttons := make([]discordgo.MessageComponent, 0, len(editorTabs))
	for index := range editorTabs {
		candidate := &editorTabs[index]
		style := discordgo.SecondaryButton
		if candidate == tab {
			style = discordgo.PrimaryButton
		}
		buttons = append(buttons, discordgo.Button{
			Label:    tabLabel(guildID, candidate.key),
			Style:    style,
			CustomID: discord.ComponentID(transitionTabRoute, candidate.key, songArgument, location.messageID, pageArgument),
			Disabled: candidate == tab || (row.isOutro() && candidate.key == tabIncoming),
		})
	}
	return discordgo.ActionsRow{Components: buttons}
}
