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
var HookBeginAutocomplete = beginAutocomplete
var HookCreateVideoChoiceName = createVideoChoiceName
var HookCheckAvailability = checkAvailability
var HookConfirmedByRequester = confirmedByRequester
var HookExcludeVideo = excludeVideo
var HookExpireSearch = expireSearch
var HookConfirmPlaylist = confirmPlaylist
var HookConfirmPlaylistRest = confirmPlaylistRest
var HookIsLatestAutocomplete = isLatestAutocomplete
var HookLoadChoices = loadChoices
var HookLoadNearestChoices = loadNearestChoices
var HookNormalizeQuery = normalizeQuery
var HookParseSearchSelection = parseSearchSelection
var HookPlaylistPromptExpiry = &playlistPromptExpiry
var HookPlaylistTimeoutEmbed = playlistTimeoutEmbed
var HookReadPlayRequest = readPlayRequest
var HookSaveChoices = saveChoices
var HookSearchResultsPanel = searchResultsPanel
var HookSearchSelectionExpiry = &searchSelectionExpiry

func HookCreateSearchSelection(fields *HookSearchSelectionFields) *searchSelection {
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

func HookEnqueueAvailable(guildID string, songs []*youtube.Song, results []youtube.BatchResult) (int, int, []string) {
	outcome := enqueueAvailable(guildID, songs, results, false)
	skippedURLs := make([]string, 0, len(outcome.skippedSongs))
	for i := range outcome.skippedSongs {
		skippedURLs = append(skippedURLs, outcome.skippedSongs[i].URL)
	}
	return outcome.added, outcome.skipped, skippedURLs
}
