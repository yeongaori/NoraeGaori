package commands

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	autocompleteCacheTTL         = 5 * time.Minute
	autocompleteCacheCapacity    = 512
	autocompleteMinFetchInterval = 350 * time.Millisecond
	autocompleteGateCapacity     = 1024
	autocompleteGateIdlePeriod   = 10 * time.Minute
)

type autocompleteCacheEntry struct {
	key       string
	choices   []*discordgo.ApplicationCommandOptionChoice
	timestamp time.Time
	element   *list.Element
}

var (
	autocompleteCacheEntries = make(map[string]*autocompleteCacheEntry)
	autocompleteCacheOrder   = list.New()
	autocompleteCacheMutex   sync.Mutex

	autocompleteGates      = make(map[string]time.Time)
	autocompleteGatesMutex sync.Mutex
)

func normalizeAutocompleteQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func autocompleteCacheKey(kind, language, query string) string {
	return kind + "|" + language + "|" + query
}

func loadAutocompleteChoices(key string) ([]*discordgo.ApplicationCommandOptionChoice, bool) {
	autocompleteCacheMutex.Lock()
	defer autocompleteCacheMutex.Unlock()

	entry, exists := autocompleteCacheEntries[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.timestamp) > autocompleteCacheTTL {
		autocompleteCacheOrder.Remove(entry.element)
		delete(autocompleteCacheEntries, key)
		return nil, false
	}

	autocompleteCacheOrder.MoveToFront(entry.element)
	return entry.choices, true
}

func saveAutocompleteChoices(key string, choices []*discordgo.ApplicationCommandOptionChoice) {
	autocompleteCacheMutex.Lock()
	defer autocompleteCacheMutex.Unlock()

	if entry, exists := autocompleteCacheEntries[key]; exists {
		entry.choices = choices
		entry.timestamp = time.Now()
		autocompleteCacheOrder.MoveToFront(entry.element)
		return
	}

	entry := &autocompleteCacheEntry{
		key:       key,
		choices:   choices,
		timestamp: time.Now(),
	}
	entry.element = autocompleteCacheOrder.PushFront(entry)
	autocompleteCacheEntries[key] = entry

	for autocompleteCacheOrder.Len() > autocompleteCacheCapacity {
		oldest := autocompleteCacheOrder.Back()
		if oldest == nil {
			break
		}
		autocompleteCacheOrder.Remove(oldest)
		if evicted, ok := oldest.Value.(*autocompleteCacheEntry); ok {
			delete(autocompleteCacheEntries, evicted.key)
		}
	}
}

func loadNearestAutocompleteChoices(kind, language, query string) []*discordgo.ApplicationCommandOptionChoice {
	runes := []rune(query)
	for length := len(runes); length >= minAutocompleteQueryRunes; length-- {
		key := autocompleteCacheKey(kind, language, string(runes[:length]))
		if choices, found := loadAutocompleteChoices(key); found {
			return choices
		}
	}
	return nil
}

func allowAutocompleteFetch(userID, commandName string) bool {
	key := userID + "|" + commandName

	autocompleteGatesMutex.Lock()
	defer autocompleteGatesMutex.Unlock()

	now := time.Now()
	if last, exists := autocompleteGates[key]; exists && now.Sub(last) < autocompleteMinFetchInterval {
		return false
	}

	if len(autocompleteGates) >= autocompleteGateCapacity {
		for gateKey, last := range autocompleteGates {
			if now.Sub(last) > autocompleteGateIdlePeriod {
				delete(autocompleteGates, gateKey)
			}
		}
	}

	autocompleteGates[key] = now
	return true
}
