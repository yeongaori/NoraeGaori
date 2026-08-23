package discordtest

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func Session(t *testing.T, botUserID string) *discordgo.Session {
	t.Helper()

	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: botUserID}
	return session
}

func SessionWithGuild(t *testing.T, botUserID, guildID string, states []*discordgo.VoiceState, members []*discordgo.Member) *discordgo.Session {
	t.Helper()

	session := Session(t, botUserID)
	if err := session.State.GuildAdd(&discordgo.Guild{ID: guildID, VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	for _, member := range members {
		if err := session.State.MemberAdd(member); err != nil {
			t.Fatalf("failed to seed member %s: %v", member.User.ID, err)
		}
	}
	return session
}

func VoiceState(guildID, userID, channelID string, isBot bool) *discordgo.VoiceState {
	return &discordgo.VoiceState{
		GuildID:   guildID,
		UserID:    userID,
		ChannelID: channelID,
		Member:    &discordgo.Member{GuildID: guildID, User: &discordgo.User{ID: userID, Bot: isBot}},
	}
}

func ReactionAdd(guildID, userID, messageID, emoji string) *discordgo.MessageReactionAdd {
	return &discordgo.MessageReactionAdd{MessageReaction: messageReaction(guildID, userID, messageID, emoji)}
}

func ReactionRemove(guildID, userID, messageID, emoji string) *discordgo.MessageReactionRemove {
	return &discordgo.MessageReactionRemove{MessageReaction: messageReaction(guildID, userID, messageID, emoji)}
}

func messageReaction(guildID, userID, messageID, emoji string) *discordgo.MessageReaction {
	return &discordgo.MessageReaction{
		GuildID:   guildID,
		UserID:    userID,
		MessageID: messageID,
		Emoji:     discordgo.Emoji{Name: emoji},
	}
}
