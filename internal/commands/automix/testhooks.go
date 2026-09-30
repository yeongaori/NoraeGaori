//go:build testhooks

package automix

import (
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/queue"
)

const HookDiscordLabelLimit = discordLabelLimit
const HookDiscordSelectLimit = discordSelectLimit
const HookTransitionPageRoute = transitionPageRoute
const HookTransitionPickRoute = transitionPickRoute
const HookTransitionStyleRoute = transitionStyleRoute

type HookPanelLocation = panelLocation
type HookPanelState = panelState
type HookTransitionRow = transitionRow

type HookPanelLocationFields struct {
	MessageID string
	Page      int
}

type HookPanelStateFields struct {
	Pairs          []transitionPair
	GuildOverrides transition.StyleOverrides
	AutoSelect     bool
	Crossfade      bool
	AutoMixBeats   int
	CrossfadeSec   float64
	BackfillActive bool
}

var HookChooseTransitionStyle = chooseTransitionStyle
var HookCreateTransitionEditorComponents = createTransitionEditorComponents
var HookCreateTransitionEditorEmbed = createTransitionEditorEmbed
var HookCreateTransitionPanelComponents = createTransitionPanelComponents
var HookCreateTransitionPanelEmbed = createTransitionPanelEmbed
var HookDescribeTrack = describeTrack
var HookFindTransitionPair = findTransitionPair
var HookHydrateTransitionRows = hydrateTransitionRows
var HookPanelOpenButtons = panelOpenButtons
var HookPickTransition = pickTransition
var HookQueueStyleOverrides = queueStyleOverrides
var HookRegisterPanelRoutes = registerPanelRoutes
var HookSourceLabel = sourceLabel
var HookTransitionCategories = &transitionCategories
var HookTransitionPageCount = transitionPageCount
var HookTransitionPageSlice = transitionPageSlice
var HookTransitionPairs = transitionPairs
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
		backfillActive: fields.BackfillActive,
	}
}

func (p *panelLocation) HookMessageID() *string {
	return &p.messageID
}

func (p *panelLocation) HookPage() *int {
	return &p.page
}

func (p *panelState) HookBackfillActive() *bool {
	return &p.backfillActive
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

func (pair transitionPair) HookIsOutro() bool {
	return pair.isOutro()
}

func (t *transitionRow) HookEffective() *map[string]string {
	return &t.effective
}

func (t *transitionRow) HookFromAnalysis() **analysis.TrackAnalysis {
	return &t.fromAnalysis
}

func (t *transitionRow) HookFromAnalyzing() *bool {
	return &t.fromAnalyzing
}

func (t *transitionRow) HookSource() *map[string]string {
	return &t.source
}

func (t *transitionRow) HookToAnalyzing() *bool {
	return &t.toAnalyzing
}
