package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseCommandOptions(t *testing.T) {
	testCases := []struct {
		name           string
		cmd            *Command
		args           []string
		expectedCount  int
		expectedName   string
		expectedValue  interface{}
	}{
		{
			name: "String option - single word",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "query",
						Type: discordgo.ApplicationCommandOptionString,
					},
				},
			},
			args:          []string{"hello"},
			expectedCount: 1,
			expectedName:  "query",
			expectedValue: "hello",
		},
		{
			name: "String option - multiple words",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "query",
						Type: discordgo.ApplicationCommandOptionString,
					},
				},
			},
			args:          []string{"hello", "world", "test"},
			expectedCount: 1,
			expectedName:  "query",
			expectedValue: "hello world test",
		},
		{
			name: "Integer option - valid",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "position",
						Type: discordgo.ApplicationCommandOptionInteger,
					},
				},
			},
			args:          []string{"42"},
			expectedCount: 1,
			expectedName:  "position",
			expectedValue: int64(42),
		},
		{
			name: "Integer option - invalid",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "position",
						Type: discordgo.ApplicationCommandOptionInteger,
					},
				},
			},
			args:          []string{"not_a_number"},
			expectedCount: 0,
		},
		{
			name: "Boolean option - true variants",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "enabled",
						Type: discordgo.ApplicationCommandOptionBoolean,
					},
				},
			},
			args:          []string{"true"},
			expectedCount: 1,
			expectedName:  "enabled",
			expectedValue: true,
		},
		{
			name: "Boolean option - on",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "enabled",
						Type: discordgo.ApplicationCommandOptionBoolean,
					},
				},
			},
			args:          []string{"on"},
			expectedCount: 1,
			expectedName:  "enabled",
			expectedValue: true,
		},
		{
			name: "Boolean option - false",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "enabled",
						Type: discordgo.ApplicationCommandOptionBoolean,
					},
				},
			},
			args:          []string{"off"},
			expectedCount: 1,
			expectedName:  "enabled",
			expectedValue: false,
		},
		{
			name: "Channel option - with mention",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "channel",
						Type: discordgo.ApplicationCommandOptionChannel,
					},
				},
			},
			args:          []string{"<#123456789>"},
			expectedCount: 1,
			expectedName:  "channel",
			expectedValue: "123456789",
		},
		{
			name: "Channel option - raw ID",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "channel",
						Type: discordgo.ApplicationCommandOptionChannel,
					},
				},
			},
			args:          []string{"123456789"},
			expectedCount: 1,
			expectedName:  "channel",
			expectedValue: "123456789",
		},
		{
			name: "Multiple options",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name: "position1",
						Type: discordgo.ApplicationCommandOptionInteger,
					},
					{
						Name: "position2",
						Type: discordgo.ApplicationCommandOptionInteger,
					},
				},
			},
			args:          []string{"1", "5"},
			expectedCount: 2,
		},
		{
			name: "No args with required option",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:     "query",
						Type:     discordgo.ApplicationCommandOptionString,
						Required: true,
					},
				},
			},
			args:          []string{},
			expectedCount: 0,
		},
		{
			name: "Optional option with no args",
			cmd: &Command{
				Name: "test",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:     "query",
						Type:     discordgo.ApplicationCommandOptionString,
						Required: false,
					},
				},
			},
			args:          []string{},
			expectedCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := parseCommandOptions(tc.cmd, tc.args)

			if len(options) != tc.expectedCount {
				t.Errorf("Expected %d options, got %d", tc.expectedCount, len(options))
			}

			if tc.expectedCount > 0 && tc.expectedName != "" {
				if options[0].Name != tc.expectedName {
					t.Errorf("Expected option name %s, got %s", tc.expectedName, options[0].Name)
				}

				if tc.expectedValue != nil && options[0].Value != tc.expectedValue {
					t.Errorf("Expected value %v, got %v", tc.expectedValue, options[0].Value)
				}
			}
		})
	}
}

func TestCreatePseudoInteraction(t *testing.T) {
	session := &discordgo.Session{}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg123",
			ChannelID: "channel123",
			GuildID:   "guild123",
			Author: &discordgo.User{
				ID:       "user123",
				Username: "testuser",
			},
		},
	}

	cmd := &Command{
		Name: "play",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name: "query",
				Type: discordgo.ApplicationCommandOptionString,
			},
		},
	}

	args := []string{"test", "song"}

	interaction := CreatePseudoInteraction(session, message, cmd, args)

	if interaction == nil {
		t.Fatal("Interaction should not be nil")
	}

	if interaction.GuildID != message.GuildID {
		t.Errorf("Expected GuildID %s, got %s", message.GuildID, interaction.GuildID)
	}

	if interaction.ChannelID != message.ChannelID {
		t.Errorf("Expected ChannelID %s, got %s", message.ChannelID, interaction.ChannelID)
	}

	data, ok := interaction.Data.(discordgo.ApplicationCommandInteractionData)
	if !ok {
		t.Fatal("Interaction data should be ApplicationCommandInteractionData")
	}

	if data.Name != cmd.Name {
		t.Errorf("Expected command name %s, got %s", cmd.Name, data.Name)
	}
}

func TestMessageResponseFunctions(t *testing.T) {
	// Test that MessageResponse methods don't panic
	mr := &MessageResponse{
		Session:   &discordgo.Session{},
		ChannelID: "channel123",
		Message:   &discordgo.Message{},
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Test",
		Description: "Test Description",
	}

	// These will fail to send (no real connection), but shouldn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MessageResponse method panicked: %v", r)
		}
	}()

	mr.SendMessage("test")
	mr.SendEmbed(embed)
	mr.SendFollowUp("followup")
	mr.SendFollowUpEmbed(embed)
}

func TestIsMessageCommand(t *testing.T) {
	testCases := []struct {
		name     string
		token    string
		expected bool
	}{
		{"Message command token", "message_123_456", true},
		{"Regular token", "regular_token", false},
		{"Empty token", "", false},
		{"Message prefix only", "message_", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			interaction := &discordgo.InteractionCreate{
				Interaction: &discordgo.Interaction{
					Token: tc.token,
				},
			}

			result := isMessageCommand(interaction)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("Nil command", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with nil command")
			}
		}()
		parseCommandOptions(nil, []string{"test"})
	})

	t.Run("Empty args with no options", func(t *testing.T) {
		cmd := &Command{
			Name:    "test",
			Options: []*discordgo.ApplicationCommandOption{},
		}
		options := parseCommandOptions(cmd, []string{})
		if options != nil && len(options) > 0 {
			t.Error("Expected nil or empty options")
		}
	})

	t.Run("Excess args", func(t *testing.T) {
		cmd := &Command{
			Name: "test",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name: "single",
					Type: discordgo.ApplicationCommandOptionInteger,
				},
			},
		}
		options := parseCommandOptions(cmd, []string{"1", "extra", "args"})
		if len(options) != 1 {
			t.Errorf("Expected 1 option, got %d", len(options))
		}
	})
}
