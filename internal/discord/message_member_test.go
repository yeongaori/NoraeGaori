package discord

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func memberMessage(member *discordgo.Member) *discordgo.MessageCreate {
	return &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "111",
			ChannelID: "222",
			GuildID:   menuGuildID,
			Author:    &discordgo.User{ID: "author"},
			Member:    member,
		},
	}
}

func TestMessageMemberUsesTheMemberSentWithTheMessage(t *testing.T) {
	session, requests := stubDiscordAPI(t, http.StatusOK)
	sent := &discordgo.Member{Roles: []string{"role"}}
	message := memberMessage(sent)

	member := MessageMember(session, message)

	if member.User != message.Author || member.GuildID != menuGuildID {
		t.Errorf("member = %+v, want the author and guild filled in", member)
	}
	if len(member.Roles) != 1 || member.Roles[0] != "role" {
		t.Errorf("member roles = %v, want the roles sent with the message", member.Roles)
	}
	if sent.User != nil || sent.GuildID != "" {
		t.Error("the member attached to the message event was modified")
	}
	if got := len(requests()); got != 0 {
		t.Errorf("sent %d requests, want no member lookup", got)
	}
}

func TestMessageMemberFetchesTheMemberWhenTheMessageHasNone(t *testing.T) {
	session, requests := stubDiscordAPI(t, http.StatusOK)

	member := MessageMember(session, memberMessage(nil))

	if member.User == nil || member.User.ID != "fetched" || len(member.Roles) != 1 {
		t.Errorf("member = %+v, want the member returned by Discord", member)
	}
	sent := requests()
	if len(sent) != 1 || sent[0].method != "GET" || sent[0].path != "/guilds/"+menuGuildID+"/members/author" {
		t.Errorf("sent %v, want one member lookup", sent)
	}
}

func TestMessageMemberFallsBackToTheAuthorWhenTheLookupFails(t *testing.T) {
	session, _ := stubDiscordAPI(t, http.StatusNotFound)
	message := memberMessage(nil)

	member := MessageMember(session, message)

	if member.User != message.Author || len(member.Roles) != 0 {
		t.Errorf("member = %+v, want only the author", member)
	}
}

func TestCreatePseudoInteractionCarriesTheGivenMember(t *testing.T) {
	message := memberMessage(nil)
	member := &discordgo.Member{User: message.Author}

	interaction := CreatePseudoInteraction(message, member, "play", nil, nil)

	if interaction.Member != member {
		t.Error("the pseudo interaction does not carry the resolved member")
	}
	if interaction.Token != "message_111_222" {
		t.Errorf("token = %q, want message_111_222", interaction.Token)
	}
}
