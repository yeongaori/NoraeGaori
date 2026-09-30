package automix_test

import (
	"testing"

	"noraegaori/internal/commands/automix"
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
)

func TestRegisterAddsTheAutoMixCommandsAndRoutes(t *testing.T) {
	commandStrings := func(string) messages.CommandStrings { return messages.CommandStrings{} }
	settings.Register(commandStrings)
	automix.Register(commandStrings)

	commandtest.WantRegistered(t, map[string]commandtest.Registration{
		"automixstyle": {Handler: automix.HandleAutoMixStyle},
		"automixpanel": {Handler: automix.HandleAutoMixPanel},
		"fadein":       {SettingKey: "fadein"},
		"fadeout":      {SettingKey: "fadeout"},
		"automix":      {SettingKey: "automix"},
		"crossfade":    {SettingKey: "crossfade"},
		"fadeonstop":   {SettingKey: "fadeonstop"},
		"trimsilence":  {SettingKey: "trimsilence"},
	})

	for _, route := range []string{automix.HookTransitionPageRoute, automix.HookTransitionPickRoute, automix.HookTransitionStyleRoute} {
		if !discord.HandleComponentRoute(nil, discordtest.ComponentInteraction(commandtest.GuildID, route+":not-a-page", nil)) {
			t.Errorf("route %q was not registered", route)
		}
	}
}
