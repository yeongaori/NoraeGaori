package discordtest

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

const (
	InteractionID    = "111"
	InteractionAppID = "app"
	InteractionToken = "token"
	ChannelID        = "222"
	PanelMessageID   = "panel-message"
)

func StringOption(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionString, Value: value}
}

func IntegerOption(name string, value float64) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionInteger, Value: value}
}

func Member(guildID, userID string) *discordgo.Member {
	return &discordgo.Member{GuildID: guildID, User: &discordgo.User{ID: userID, Username: userID}}
}

func SlashInteraction(guildID, name string, member *discordgo.Member, options ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        InteractionID,
			AppID:     InteractionAppID,
			Token:     InteractionToken,
			Type:      discordgo.InteractionApplicationCommand,
			GuildID:   guildID,
			ChannelID: ChannelID,
			Member:    member,
			Data:      discordgo.ApplicationCommandInteractionData{Name: name, Options: options},
		},
	}
}

func ComponentInteraction(guildID, customID string, member *discordgo.Member, values ...string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        InteractionID,
			AppID:     InteractionAppID,
			Token:     InteractionToken,
			Type:      discordgo.InteractionMessageComponent,
			GuildID:   guildID,
			ChannelID: ChannelID,
			Member:    member,
			Message:   &discordgo.Message{ID: PanelMessageID, ChannelID: ChannelID},
			Data:      discordgo.MessageComponentInteractionData{CustomID: customID, Values: values},
		},
	}
}

func AddGuild(t *testing.T, session *discordgo.Session, guildID string, states ...*discordgo.VoiceState) {
	t.Helper()

	if err := session.State.GuildAdd(&discordgo.Guild{ID: guildID, VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
}

func ResponseType(request *Request) discordgo.InteractionResponseType {
	value, _ := request.Body["type"].(float64)
	return discordgo.InteractionResponseType(value)
}

func IsEphemeral(request *Request) bool {
	data, _ := request.Body["data"].(map[string]any)
	flags, _ := data["flags"].(float64)
	return discordgo.MessageFlags(flags) == discordgo.MessageFlagsEphemeral
}

func ReplyEmbed(t *testing.T, request *Request) map[string]any {
	t.Helper()

	body := request.Body
	if data, isCallback := body["data"].(map[string]any); isCallback {
		body = data
	}
	embeds, _ := body["embeds"].([]any)
	if len(embeds) == 0 {
		t.Fatalf("%s %s carries no embed", request.Method, request.Path)
	}
	embed, isObject := embeds[0].(map[string]any)
	if !isObject {
		t.Fatalf("%s %s carries an embed of type %T", request.Method, request.Path, embeds[0])
	}
	return embed
}

func EmbedText(embed map[string]any) string {
	var builder strings.Builder
	writeText(&builder, embed["title"])
	writeText(&builder, embed["description"])
	if footer, hasFooter := embed["footer"].(map[string]any); hasFooter {
		writeText(&builder, footer["text"])
	}
	fields, _ := embed["fields"].([]any)
	for _, field := range fields {
		if entry, isObject := field.(map[string]any); isObject {
			writeText(&builder, entry["name"])
			writeText(&builder, entry["value"])
		}
	}
	return builder.String()
}

func writeText(builder *strings.Builder, value any) {
	if text, isText := value.(string); isText {
		builder.WriteString(text)
		builder.WriteByte('\n')
	}
}
