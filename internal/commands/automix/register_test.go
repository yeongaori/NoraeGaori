package automix

import (
	"testing"

	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/discordtest"
)

func TestRegisterAddsTheAutoMixCommandsAndRoutes(t *testing.T) {
	commandStrings := func(string) messages.CommandStrings { return messages.CommandStrings{} }
	settings.Register(commandStrings)
	Register(commandStrings)

	commandtest.WantRegistered(t, map[string]commandtest.Registration{
		"automixstyle": {Handler: HandleAutoMixStyle},
		"automixpanel": {Handler: HandleAutoMixPanel},
		"fadein":       {SettingKey: "fadein"},
		"fadeout":      {SettingKey: "fadeout"},
		"automix":      {SettingKey: "automix"},
		"crossfade":    {SettingKey: "crossfade"},
		"fadeonstop":   {SettingKey: "fadeonstop"},
		"trimsilence":  {SettingKey: "trimsilence"},
	})

	for _, route := range []string{transitionPageRoute, transitionPickRoute, transitionStyleRoute} {
		if !discord.HandleComponentRoute(nil, discordtest.ComponentInteraction(commandtest.GuildID, route+":not-a-page", nil)) {
			t.Errorf("route %q was not registered", route)
		}
	}
}
