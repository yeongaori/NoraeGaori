//go:build testhooks

package automix

import (
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/queue"
)

const HookDiscordLabelLimit = discordLabelLimit
const HookDiscordSelectLimit = discordSelectLimit
const HookTransitionPageRoute = transitionPageRoute
const HookTransitionPickRoute = transitionPickRoute
const HookTransitionStyleRoute = transitionStyleRoute
const HookTransitionTabRoute = transitionTabRoute
const HookMixingSettingsRoute = mixingSettingsRoute

type HookEditorTab = editorTab
type HookPanelLocation = panelLocation
type HookPanelState = panelState
type HookTransitionRow = transitionRow

type HookPanelLocationFields struct {
	MessageID string
	Page      int
}

type HookPanelStateFields struct {
	Pairs          []transitionPair
	GuildOverrides map[string]string
	AutoSelect     bool
	Crossfade      bool
	AutoMixBeats   int
	CrossfadeSec   float64
	Pending        map[int]int
}

var HookChooseTransitionStyle = chooseTransitionStyle
var HookCreateTransitionEditorComponents = createTransitionEditorComponents
var HookCreateTransitionEditorEmbed = createTransitionEditorEmbed
var HookCreateTransitionPanelComponents = createTransitionPanelComponents
var HookCreateTransitionPanelEmbed = createTransitionPanelEmbed
var HookDescribeRecipe = describeRecipe
var HookDescribeTrack = describeTrack
var HookEditorTabs = &editorTabs
var HookFindTab = findTab
var HookFindTransitionPair = findTransitionPair
var HookHydrateTransitionRows = hydrateTransitionRows
var HookPanelOpenButtons = panelOpenButtons
var HookPickTransition = pickTransition
var HookRegisterPanelRoutes = registerPanelRoutes
var HookSourceLabel = sourceLabel
var HookTransitionPageCount = transitionPageCount
var HookTransitionPageSlice = transitionPageSlice
var HookTransitionPairs = transitionPairs
var HookTurnEditorTab = turnEditorTab
var HookTurnTransitionPage = turnTransitionPage
var HookVoiceChannelBitrate = voiceChannelBitrate

func HookBuildPanelLocation(fields HookPanelLocationFields) *panelLocation {
	return &panelLocation{messageID: fields.MessageID, page: fields.Page}
}

func HookBuildPanelState(fields HookPanelStateFields) *panelState {
	return &panelState{
		pairs:          fields.Pairs,
		guildOverrides: fields.GuildOverrides,
		autoSelect:     fields.AutoSelect,
		crossfade:      fields.Crossfade,
		autoMixBeats:   fields.AutoMixBeats,
		crossfadeSec:   fields.CrossfadeSec,
		pending:        fields.Pending,
	}
}

func (p *panelLocation) HookMessageID() *string {
	return &p.messageID
}

func (p *panelLocation) HookPage() *int {
	return &p.page
}

func (p *panelState) HookPending() *map[int]int {
	return &p.pending
}

func (p *panelState) HookPairs() *[]transitionPair {
	return &p.pairs
}

func (pair *transitionPair) HookFromIndex() *int {
	return &pair.fromIndex
}

func (pair *transitionPair) HookFromSong() **queue.Song {
	return &pair.fromSong
}

func (pair *transitionPair) HookToIndex() *int {
	return &pair.toIndex
}

func (pair *transitionPair) HookToSong() **queue.Song {
	return &pair.toSong
}

func (pair *transitionPair) HookIsOutro() bool {
	return pair.isOutro()
}

func (row *transitionRow) HookResolved() *transition.Resolved {
	return row.resolved
}

func (row *transitionRow) HookOverlap() *transition.Overlap {
	return row.overlap
}

func (row *transitionRow) HookSource(category transition.Category) string {
	return row.source(category)
}

func (row *transitionRow) HookFromAnalyzing() *bool {
	return &row.fromAnalyzing
}

func (row *transitionRow) HookToAnalyzing() *bool {
	return &row.toAnalyzing
}

func (tab *editorTab) HookKey() string {
	return tab.key
}

func (tab *editorTab) HookCategories() []transition.Category {
	return tab.categories
}
