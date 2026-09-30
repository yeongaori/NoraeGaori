package settings_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"
)

func toggleCommandStrings(name string) messages.CommandStrings {
	return messages.CommandStrings{
		Description: name + " description",
		Options: map[string]string{
			"setting":  name + " setting",
			"duration": name + " duration",
			"beats":    name + " beats",
		},
	}
}

func registeredToggle(t *testing.T, name string) *command.Command {
	t.Helper()

	settings.RegisterToggleCommand(toggleCommandStrings, name)
	registered, found := command.Snapshot()[name]
	if !found {
		t.Fatalf("%s was not registered", name)
	}
	return registered
}

func wantToggleOption(t *testing.T, name string, option *discordgo.ApplicationCommandOption) {
	t.Helper()

	if option.Name != "setting" || option.Type != discordgo.ApplicationCommandOptionString || option.Description != name+" setting" {
		t.Errorf("%s first option is %q (type %v, %q), want the setting string option", name, option.Name, option.Type, option.Description)
	}
	if len(option.Choices) != 2 || option.Choices[0].Value != settings.HookValueOn || option.Choices[1].Value != settings.HookValueOff {
		t.Errorf("%s offers choices %+v, want on and off", name, option.Choices)
	}
	if option.Required {
		t.Errorf("%s requires its setting option, want it optional so the bare command opens the menu", name)
	}
}

func TestPlainTogglesGetOnlyTheSettingOption(t *testing.T) {
	for _, name := range []string{"sponsorblock", "fadeonstop", "trimsilence"} {
		registered := registeredToggle(t, name)

		if len(registered.Options) != 1 {
			t.Fatalf("%s has %d options, want 1", name, len(registered.Options))
		}
		wantToggleOption(t, name, registered.Options[0])
		if registered.Description != name+" description" || registered.TextOnly || registered.Handler == nil {
			t.Errorf("%s registered as %+v, want a described slash command with a handler", name, registered)
		}
	}
}

func TestTogglesWithANumberSettingGetItsLimits(t *testing.T) {
	cases := []struct {
		name   string
		option string
		min    float64
		max    float64
	}{
		{name: "fadein", option: "duration", min: 1, max: 30},
		{name: "fadeout", option: "duration", min: 1, max: 30},
		{name: "crossfade", option: "duration", min: 1, max: 30},
		{name: "automix", option: "beats", min: 4, max: 64},
	}

	for _, testCase := range cases {
		registered := registeredToggle(t, testCase.name)

		if len(registered.Options) != 2 {
			t.Fatalf("%s has %d options, want 2", testCase.name, len(registered.Options))
		}
		wantToggleOption(t, testCase.name, registered.Options[0])

		number := registered.Options[1]
		if number.Name != testCase.option || number.Type != discordgo.ApplicationCommandOptionInteger {
			t.Errorf("%s second option is %q (type %v), want integer %q", testCase.name, number.Name, number.Type, testCase.option)
		}
		if number.Description != testCase.name+" "+testCase.option {
			t.Errorf("%s %s description is %q", testCase.name, testCase.option, number.Description)
		}
		if number.Required {
			t.Errorf("%s requires its %s option, want it optional", testCase.name, testCase.option)
		}
		if number.MinValue == nil || *number.MinValue != testCase.min || number.MaxValue != testCase.max {
			t.Errorf("%s %s limits are %v..%v, want %v..%v", testCase.name, testCase.option, number.MinValue, number.MaxValue, testCase.min, testCase.max)
		}
	}
}

func isToggleOption(option *discordgo.ApplicationCommandOption) bool {
	return option.Name == "setting" && len(option.Choices) == 2 &&
		option.Choices[0].Value == settings.HookValueOn && option.Choices[1].Value == settings.HookValueOff
}

func TestToggleCommandsExistOnlyForToggleSettings(t *testing.T) {
	settings.Register(toggleCommandStrings)

	for name, registered := range command.Snapshot() {
		if len(registered.Options) == 0 || !isToggleOption(registered.Options[0]) {
			continue
		}
		if spec, found := settings.HookFindSetting(name); !found || *spec.HookKind() != settings.HookSettingToggle {
			t.Errorf("%s is registered as an on/off command, but it is not a toggle setting", name)
		}
	}
}

func TestToggleNumberLimitsAreIndependentCopies(t *testing.T) {
	fadeIn := registeredToggle(t, "fadein").Options[1]
	*fadeIn.MinValue = 99

	if spec := specFor(t, "fadein_duration"); *spec.HookMin() != 1 {
		t.Errorf("changing the option minimum changed the spec minimum to %v", *spec.HookMin())
	}
}
