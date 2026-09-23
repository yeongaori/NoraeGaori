package automix

import (
	"testing"

	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/discordtest"
)

func TestRegisterAddsTheAutoMixCommandsAndRoutes(t *testing.T) {
	Register(func(string) messages.CommandStrings { return messages.CommandStrings{} })

	registered := command.Snapshot()
	for _, name := range []string{"fadein", "fadeout", "automix", "automixstyle", "automixpanel", "crossfade", "fadeonstop", "trimsilence"} {
		if _, found := registered[name]; !found {
			t.Errorf("command %q was not registered", name)
		}
	}

	for _, route := range []string{transitionPageRoute, transitionPickRoute, transitionStyleRoute} {
		if !discord.HandleComponentRoute(nil, discordtest.ComponentInteraction(commandtest.GuildID, route+":not-a-page", nil)) {
			t.Errorf("route %q was not registered", route)
		}
	}
}
