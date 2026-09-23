package discord

import (
	"strconv"

	"github.com/bwmarrin/discordgo"
)

func PageCount(itemCount, perPage int) int {
	return max(1, (itemCount+perPage-1)/perPage)
}

func ClampPage(page, totalPages int) int {
	return max(1, min(page, totalPages))
}

func PageBounds(page, perPage, itemCount int) (int, int) {
	if page < 1 || page > PageCount(itemCount, perPage) {
		return 0, 0
	}
	start := (page - 1) * perPage
	return start, min(start+perPage, itemCount)
}

func PageButtonRow(route string, page, totalPages int, previousLabel, nextLabel string, arguments []string, extras ...discordgo.MessageComponent) discordgo.ActionsRow {
	pageArguments := make([]string, len(arguments)+1)
	copy(pageArguments, arguments)
	last := len(arguments)

	pageArguments[last] = strconv.Itoa(page - 1)
	previousID := ComponentID(route, pageArguments...)
	pageArguments[last] = strconv.Itoa(page + 1)
	nextID := ComponentID(route, pageArguments...)

	components := make([]discordgo.MessageComponent, 0, 2+len(extras))
	components = append(components,
		discordgo.Button{Label: previousLabel, Style: discordgo.PrimaryButton, CustomID: previousID, Disabled: page <= 1},
		discordgo.Button{Label: nextLabel, Style: discordgo.PrimaryButton, CustomID: nextID, Disabled: page >= totalPages},
	)
	return discordgo.ActionsRow{Components: append(components, extras...)}
}
