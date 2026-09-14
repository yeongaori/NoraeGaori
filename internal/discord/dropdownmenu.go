package discord

import (
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

const dropdownMenuPrefix = "dropdown_menu_"

type DropdownMenu struct {
	Label   string
	Current string
	Options []discordgo.SelectMenuOption
	Apply   func(value string) (*discordgo.MessageEmbed, error)
}

var dropdownMenus sync.Map

func RegisterDropdownMenu(key string, build func(guildID string) DropdownMenu) {
	dropdownMenus.Store(key, build)
}

func lookupDropdownMenu(key string) (func(string) DropdownMenu, bool) {
	build, ok := dropdownMenus.Load(key)
	if !ok {
		return nil, false
	}
	return build.(func(string) DropdownMenu), true
}

func RespondDropdownMenu(s *discordgo.Session, i *discordgo.InteractionCreate, key string) error {
	build, ok := lookupDropdownMenu(key)
	if !ok {
		return fmt.Errorf("dropdown menu %q is not registered", key)
	}

	menu := build(i.GuildID)
	embed, components := renderDropdownMenu(i.GuildID, key, &menu)
	_, err := sendEmbedWithComponents(s, i, embed, components)
	return err
}

func HandleDropdownMenuPick(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	key, failure, isApplied := applyDropdownPick(ic)
	if !isApplied {
		return
	}
	if failure != nil {
		respondDropdownFailure(s, ic, failure)
		return
	}

	build, _ := lookupDropdownMenu(key)
	menu := build(ic.GuildID)
	embed, components := renderDropdownMenu(ic.GuildID, key, &menu)
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		logger.Errorf("Failed to refresh the %s dropdown: %v", key, err)
	}
}

func applyDropdownPick(ic *discordgo.InteractionCreate) (string, *discordgo.MessageEmbed, bool) {
	if ic.Type != discordgo.InteractionMessageComponent || ic.GuildID == "" {
		return "", nil, false
	}

	data := ic.MessageComponentData()
	key, hasPrefix := strings.CutPrefix(data.CustomID, dropdownMenuPrefix)
	if !hasPrefix || len(data.Values) == 0 {
		return "", nil, false
	}

	build, ok := lookupDropdownMenu(key)
	if !ok {
		return "", nil, false
	}
	menu := build(ic.GuildID)
	if !isOptionOffered(menu.Options, data.Values[0]) {
		return "", nil, false
	}

	embed, err := menu.Apply(data.Values[0])
	if err != nil {
		logger.Errorf("Failed to apply %q from the %s dropdown: %v", data.Values[0], key, err)
		return key, embed, true
	}
	return key, nil, true
}

func respondDropdownFailure(s *discordgo.Session, ic *discordgo.InteractionCreate, failure *discordgo.MessageEmbed) {
	err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{failure},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		logger.Errorf("Failed to report a dropdown failure: %v", err)
	}
}

func renderDropdownMenu(guildID, key string, menu *DropdownMenu) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	menuText := &messages.T(guildID).DropdownMenu

	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       menu.Label,
		Description: fmt.Sprintf(menuText.Current, menu.Current),
	}
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    dropdownMenuPrefix + key,
					Placeholder: menuText.Placeholder,
					Options:     menu.Options,
				},
			},
		},
	}
	return embed, components
}

func isOptionOffered(options []discordgo.SelectMenuOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}
