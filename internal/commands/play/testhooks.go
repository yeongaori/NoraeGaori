//go:build testhooks

package play

import (
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/youtube"
)

const HookAutocompleteCacheCapacity = autocompleteCacheCapacity
const HookAutocompleteCacheTTL = autocompleteCacheTTL
const HookMaxChoiceNameRunes = maxChoiceNameRunes

type HookAutocompleteCacheEntry = autocompleteCacheEntry
type HookAutocompleteGateState = autocompleteGateState
type HookSearchSelection = searchSelection

type HookSearchSelectionFields struct {
	SearchMessageID string
	Original        *discordgo.InteractionCreate
	PanelMsg        *discordgo.Message
	Done            chan struct{}
}

var HookAutocompleteCacheEntries = &autocompleteCacheEntries
var HookAutocompleteCacheKey = autocompleteCacheKey
var HookAutocompleteCacheMutex = &autocompleteCacheMutex
var HookAutocompleteCacheOrder = &autocompleteCacheOrder
var HookAutocompleteGates = &autocompleteGates
var HookAutocompleteGatesMutex = &autocompleteGatesMutex
var HookBeginAutocompleteFetch = beginAutocompleteFetch
var HookBuildVideoChoiceName = buildVideoChoiceName
var HookCheckSongsAvailability = checkSongsAvailability
var HookConfirmedByRequester = confirmedByRequester
var HookExcludeVideo = excludeVideo
var HookExpireSearchSelection = expireSearchSelection
var HookHandlePlaylistConfirmationReaction = handlePlaylistConfirmationReaction
var HookHandlePlaylistRestConfirmationReaction = handlePlaylistRestConfirmationReaction
var HookIsLatestAutocompleteFetch = isLatestAutocompleteFetch
var HookLoadAutocompleteChoices = loadAutocompleteChoices
var HookLoadNearestAutocompleteChoices = loadNearestAutocompleteChoices
var HookNormalizeAutocompleteQuery = normalizeAutocompleteQuery
var HookParseSearchSelection = parseSearchSelection
var HookPlaylistPromptExpiry = &playlistPromptExpiry
var HookPlaylistTimeoutEmbed = playlistTimeoutEmbed
var HookReadPlayRequest = readPlayRequest
var HookSaveAutocompleteChoices = saveAutocompleteChoices
var HookSearchResultsPanel = searchResultsPanel
var HookSearchSelectionExpiry = &searchSelectionExpiry

func HookBuildSearchSelection(fields *HookSearchSelectionFields) *searchSelection {
	return &searchSelection{
		searchMessageID: fields.SearchMessageID,
		original:        fields.Original,
		panelMsg:        fields.PanelMsg,
		done:            fields.Done,
	}
}

func (a *autocompleteCacheEntry) HookTimestamp() *time.Time {
	return &a.timestamp
}

func (c *searchSelection) HookDone() *chan struct{} {
	return &c.done
}

func (c *searchSelection) HookPanelMsg() **discordgo.Message {
	return &c.panelMsg
}

func HookEnqueueAvailableSongs(guildID string, songs []*youtube.Song, results []youtube.BatchResult) (int, int, []string) {
	outcome := enqueueAvailableSongs(guildID, songs, results, false)
	skippedURLs := make([]string, 0, len(outcome.skippedSongs))
	for i := range outcome.skippedSongs {
		skippedURLs = append(skippedURLs, outcome.skippedSongs[i].URL)
	}
	return outcome.added, outcome.skipped, skippedURLs
}
