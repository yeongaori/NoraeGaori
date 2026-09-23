package discord

import (
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

const (
	componentIDSeparator = ":"

	adminViewArgument  = "admin"
	memberViewArgument = "member"
)

type ComponentRoute func(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string)

var componentRoutes sync.Map

func RegisterComponentRoute(prefix string, route ComponentRoute) {
	componentRoutes.Store(prefix, route)
}

func ComponentID(prefix string, arguments ...string) string {
	size := len(prefix)
	for _, argument := range arguments {
		size += len(componentIDSeparator) + len(argument)
	}

	var builder strings.Builder
	builder.Grow(size)
	builder.WriteString(prefix)
	for _, argument := range arguments {
		builder.WriteString(componentIDSeparator)
		builder.WriteString(argument)
	}
	return builder.String()
}

func HandleComponentRoute(s *discordgo.Session, ic *discordgo.InteractionCreate) bool {
	if ic.GuildID == "" {
		return false
	}
	customID, isRoutable := interactionCustomID(ic)
	if !isRoutable {
		return false
	}

	prefix, rest, hasArguments := strings.Cut(customID, componentIDSeparator)
	route, found := componentRoutes.Load(prefix)
	if !found {
		return false
	}

	var arguments []string
	if hasArguments {
		arguments = strings.Split(rest, componentIDSeparator)
	}
	route.(ComponentRoute)(s, ic, arguments)
	return true
}

func ViewArgument(isAdmin bool) string {
	if isAdmin {
		return adminViewArgument
	}
	return memberViewArgument
}

func ParseViewArgument(argument string) (bool, bool) {
	switch argument {
	case adminViewArgument:
		return true, true
	case memberViewArgument:
		return false, true
	}
	return false, false
}

func SelectedValue(ic *discordgo.InteractionCreate) (string, bool) {
	data, isComponent := componentData(ic)
	if !isComponent || len(data.Values) == 0 {
		return "", false
	}
	return data.Values[0], true
}

func PageArgument(ic *discordgo.InteractionCreate, arguments []string, argumentCount int) (int, bool) {
	if _, isComponent := componentData(ic); !isComponent || argumentCount == 0 || len(arguments) != argumentCount {
		return 0, false
	}
	page, err := strconv.Atoi(arguments[argumentCount-1])
	return page, err == nil
}

func ModalComponents(ic *discordgo.InteractionCreate) ([]discordgo.MessageComponent, bool) {
	data, isModal := modalData(ic)
	if !isModal {
		return nil, false
	}
	return data.Components, true
}

func interactionCustomID(ic *discordgo.InteractionCreate) (string, bool) {
	if data, isComponent := componentData(ic); isComponent {
		return data.CustomID, true
	}
	if data, isModal := modalData(ic); isModal {
		return data.CustomID, true
	}
	return "", false
}

func componentData(ic *discordgo.InteractionCreate) (discordgo.MessageComponentInteractionData, bool) {
	if ic.Interaction == nil || ic.Type != discordgo.InteractionMessageComponent {
		return discordgo.MessageComponentInteractionData{}, false
	}
	data, isComponent := ic.Data.(discordgo.MessageComponentInteractionData)
	return data, isComponent
}

func modalData(ic *discordgo.InteractionCreate) (discordgo.ModalSubmitInteractionData, bool) {
	if ic.Interaction == nil || ic.Type != discordgo.InteractionModalSubmit {
		return discordgo.ModalSubmitInteractionData{}, false
	}
	data, isModal := ic.Data.(discordgo.ModalSubmitInteractionData)
	return data, isModal
}
