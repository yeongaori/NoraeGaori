package settings

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
)

type panelTarget struct {
	isAdmin  bool
	category string
	key      string
}

type panelHandler func(s *discordgo.Session, ic *discordgo.InteractionCreate, target *panelTarget)

func registerPanelRoutes() {
	discord.RegisterComponentRoute(categoryRoute, routePanel(1, switchCategory))
	discord.RegisterComponentRoute(pickRoute, routePanel(2, pickSetting))
	discord.RegisterComponentRoute(modalRoute, routePanel(3, submitSettingModal))
}

func routePanel(argumentCount int, handle panelHandler) discord.ComponentRoute {
	return func(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
		if len(arguments) != argumentCount {
			return
		}
		if target, isValid := parsePanelArguments(arguments); isValid {
			handle(s, ic, &target)
		}
	}
}

func parsePanelArguments(arguments []string) (panelTarget, bool) {
	if len(arguments) == 0 || len(arguments) > 3 {
		return panelTarget{}, false
	}

	isAdmin, isValidView := discord.ParseViewArgument(arguments[0])
	if !isValidView {
		return panelTarget{}, false
	}

	target := panelTarget{isAdmin: isAdmin}
	if len(arguments) > 1 {
		target.category = arguments[1]
		if !isVisibleCategory(target.category, isAdmin) {
			return panelTarget{}, false
		}
	}
	if len(arguments) > 2 {
		target.key = arguments[2]
	}
	return target, true
}
