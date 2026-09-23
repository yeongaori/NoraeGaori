package discord

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/discordtest"
	"noraegaori/internal/testutil/queuetest"
)

const (
	replyChannelID     = "222"
	storedMessageID    = "555"
	interactionReply   = "/api/interactions/111/token/callback"
	followUpPath       = "/webhooks/app/token"
	originalReplyPath  = "/webhooks/app/token/messages/@original"
	channelMessagePath = "/channels/" + replyChannelID + "/messages"
)

type replyCase struct {
	name             string
	isMessageCommand bool
	hasResponder     bool
	send             func(s *discordgo.Session, i *discordgo.InteractionCreate)
	wantMethod       string
	wantPath         string
	wantTexts        []string
	unwantedTexts    []string
}

const (
	privateFlag       = `"flags":64`
	replyingToCommand = `"message_reference":{"message_id":"origin"`
)

func replyInteraction(isMessageCommand bool) *discordgo.InteractionCreate {
	if isMessageCommand {
		return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: menuGuildID, ChannelID: replyChannelID, Token: "message_reply_" + replyChannelID}}
	}
	return interactionWithToken(&discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionApplicationCommand, GuildID: menuGuildID}})
}

func newResponder(session *discordgo.Session, message *discordgo.Message) *MessageResponse {
	return &MessageResponse{Session: session, ChannelID: replyChannelID, Message: message, OriginalMsgID: "origin"}
}

func runReplyCases(t *testing.T, cases []replyCase) {
	t.Helper()

	for index := range cases {
		current := &cases[index]
		t.Run(current.name, func(t *testing.T) {
			session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
			ic := replyInteraction(current.isMessageCommand)
			if current.hasResponder {
				defer RegisterResponder(ic.Token, newResponder(session, nil))()
			}

			current.send(session, ic)

			sent := requests()
			if current.wantMethod == "" {
				if len(sent) != 0 {
					t.Errorf("sent %v, want nothing", sent)
				}
				return
			}
			if len(sent) != 1 || sent[0].Method != current.wantMethod || sent[0].Path != current.wantPath {
				t.Fatalf("sent %v, want one %s %s", sent, current.wantMethod, current.wantPath)
			}
			body := string(sent[0].RawBody)
			for _, want := range current.wantTexts {
				if !strings.Contains(body, want) {
					t.Errorf("the request body %s does not contain %s", body, want)
				}
			}
			for _, unwanted := range current.unwantedTexts {
				if strings.Contains(body, unwanted) {
					t.Errorf("the request body %s contains %s", body, unwanted)
				}
			}
		})
	}
}

func TestReplyHelpersOnEveryPath(t *testing.T) {
	respondError := func(s *discordgo.Session, i *discordgo.InteractionCreate) { RespondError(s, i, "broken") }
	respondSuccess := func(s *discordgo.Session, i *discordgo.InteractionCreate) { RespondSuccess(s, i, "done") }
	respondEmbed := func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		RespondEmbed(s, i, &discordgo.MessageEmbed{Title: "answer"})
	}
	deferResponse := func(s *discordgo.Session, i *discordgo.InteractionCreate) { DeferResponse(s, i) }
	followUpMessage := func(s *discordgo.Session, i *discordgo.InteractionCreate) { FollowUpMessage(s, i, "later") }
	followUpEmbed := func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		FollowUpEmbed(s, i, &discordgo.MessageEmbed{Title: "summary"})
	}
	loadingTitle := messages.T(menuGuildID).Titles.Loading

	runReplyCases(t, []replyCase{
		{name: "an error reply to a slash command", send: respondError, wantMethod: http.MethodPost, wantPath: interactionReply, wantTexts: []string{`"content":"broken"`, privateFlag}},
		{name: "an error reply to a text command", isMessageCommand: true, hasResponder: true, send: respondError, wantMethod: http.MethodPost, wantPath: channelMessagePath, wantTexts: []string{`"content":"broken"`, replyingToCommand}},
		{name: "an error reply without a responder", isMessageCommand: true, send: respondError},
		{name: "a success reply to a slash command", send: respondSuccess, wantMethod: http.MethodPost, wantPath: interactionReply, wantTexts: []string{`"content":"done"`}, unwantedTexts: []string{privateFlag}},
		{name: "a success reply to a text command", isMessageCommand: true, hasResponder: true, send: respondSuccess, wantMethod: http.MethodPost, wantPath: channelMessagePath, wantTexts: []string{`"content":"done"`, replyingToCommand}},
		{name: "a success reply without a responder", isMessageCommand: true, send: respondSuccess},
		{name: "an embed reply to a slash command", send: respondEmbed, wantMethod: http.MethodPost, wantPath: interactionReply, wantTexts: []string{`"type":4`, `"title":"answer"`}, unwantedTexts: []string{privateFlag}},
		{name: "an embed reply to a text command", isMessageCommand: true, hasResponder: true, send: respondEmbed, wantMethod: http.MethodPost, wantPath: channelMessagePath, wantTexts: []string{`"title":"answer"`, replyingToCommand}},
		{name: "an embed reply without a responder", isMessageCommand: true, send: respondEmbed},
		{name: "a deferred slash command", send: deferResponse, wantMethod: http.MethodPost, wantPath: interactionReply, wantTexts: []string{`"type":5`}},
		{name: "a deferred text command", isMessageCommand: true, hasResponder: true, send: deferResponse, wantMethod: http.MethodPost, wantPath: channelMessagePath, wantTexts: []string{loadingTitle, replyingToCommand}},
		{name: "a deferred text command without a responder", isMessageCommand: true, send: deferResponse},
		{name: "a follow-up text for a slash command", send: followUpMessage, wantMethod: http.MethodPost, wantPath: followUpPath, wantTexts: []string{`"content":"later"`}},
		{name: "a follow-up text for a text command", isMessageCommand: true, hasResponder: true, send: followUpMessage, wantMethod: http.MethodPost, wantPath: channelMessagePath, wantTexts: []string{`"content":"later"`}, unwantedTexts: []string{"message_reference"}},
		{name: "a follow-up text without a responder", isMessageCommand: true, send: followUpMessage},
		{name: "a follow-up embed for a slash command", send: followUpEmbed, wantMethod: http.MethodPost, wantPath: followUpPath, wantTexts: []string{`"title":"summary"`}},
		{name: "a follow-up embed for a text command", isMessageCommand: true, hasResponder: true, send: followUpEmbed, wantMethod: http.MethodPost, wantPath: channelMessagePath, wantTexts: []string{`"title":"summary"`}, unwantedTexts: []string{"message_reference"}},
		{name: "a follow-up embed without a responder", isMessageCommand: true, send: followUpEmbed},
	})
}

func TestUpdateResponseEmbedOnEveryPath(t *testing.T) {
	embed := &discordgo.MessageEmbed{Title: "final"}

	t.Run("a slash command", func(t *testing.T) {
		session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

		if err := UpdateResponseEmbed(session, replyInteraction(false), embed); err != nil {
			t.Fatalf("UpdateResponseEmbed returned %v", err)
		}
		if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodPatch || sent[0].Path != originalReplyPath || !strings.Contains(string(sent[0].RawBody), `"title":"final"`) {
			t.Errorf("sent %v, want an edit of the original reply with the embed", sent)
		}
	})

	t.Run("a text command with a reply", func(t *testing.T) {
		session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
		ic := replyInteraction(true)
		defer RegisterResponder(ic.Token, newResponder(session, &discordgo.Message{ID: storedMessageID}))()

		if err := UpdateResponseEmbed(session, ic, embed); err != nil {
			t.Fatalf("UpdateResponseEmbed returned %v", err)
		}
		if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodPatch || sent[0].Path != channelMessagePath+"/"+storedMessageID || !strings.Contains(string(sent[0].RawBody), `"title":"final"`) {
			t.Errorf("sent %v, want an edit of the stored reply with the embed", sent)
		}
	})

	for name, hasResponder := range map[string]bool{"a text command before its reply": true, "a text command without responder": false} {
		t.Run(name, func(t *testing.T) {
			session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
			ic := replyInteraction(true)
			if hasResponder {
				defer RegisterResponder(ic.Token, newResponder(session, nil))()
			}

			if err := UpdateResponseEmbed(session, ic, embed); err == nil {
				t.Error("UpdateResponseEmbed returned no error without a reply to edit")
			}
			if sent := requests(); len(sent) != 0 {
				t.Errorf("sent %v, want nothing", sent)
			}
		})
	}
}

func TestUpdateResponseEmbedWithComponentsOnEveryPath(t *testing.T) {
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Label: "Next", CustomID: "next"}}}}
	embed := &discordgo.MessageEmbed{Title: "results"}

	t.Run("a slash command", func(t *testing.T) {
		session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

		if err := UpdateResponseEmbedWithComponents(session, replyInteraction(false), embed, components); err != nil {
			t.Fatalf("UpdateResponseEmbedWithComponents returned %v", err)
		}
		if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodPatch || sent[0].Path != originalReplyPath || !strings.Contains(string(sent[0].RawBody), `"custom_id":"next"`) {
			t.Errorf("sent %v, want an edit of the original reply with the button", sent)
		}
	})

	t.Run("a text command with a reply", func(t *testing.T) {
		session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
		ic := replyInteraction(true)
		defer RegisterResponder(ic.Token, newResponder(session, &discordgo.Message{ID: storedMessageID}))()

		if err := UpdateResponseEmbedWithComponents(session, ic, embed, components); err != nil {
			t.Fatalf("UpdateResponseEmbedWithComponents returned %v", err)
		}
		if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodPatch || sent[0].Path != channelMessagePath+"/"+storedMessageID || !strings.Contains(string(sent[0].RawBody), `"custom_id":"next"`) {
			t.Errorf("sent %v, want an edit of the stored reply with the button", sent)
		}
	})

	for name, check := range map[string]struct {
		hasResponder bool
		want         string
	}{
		"a text command before its reply":  {true, "message is nil"},
		"a text command without responder": {false, "message responder not found"},
	} {
		t.Run(name, func(t *testing.T) {
			session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
			ic := replyInteraction(true)
			if check.hasResponder {
				defer RegisterResponder(ic.Token, newResponder(session, nil))()
			}

			if err := UpdateResponseEmbedWithComponents(session, ic, embed, components); err == nil || err.Error() != check.want {
				t.Errorf("UpdateResponseEmbedWithComponents returned %v, want %q", err, check.want)
			}
			if sent := requests(); len(sent) != 0 {
				t.Errorf("sent %v, want nothing", sent)
			}
		})
	}
}

func TestGetResponseMessageOnEveryPath(t *testing.T) {
	t.Run("a slash command", func(t *testing.T) {
		session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

		message, err := GetResponseMessage(session, replyInteraction(false))
		if err != nil || message == nil || message.ID != "333" {
			t.Fatalf("GetResponseMessage = (%v, %v), want the fetched reply", message, err)
		}
		if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodGet || sent[0].Path != originalReplyPath {
			t.Errorf("sent %v, want a lookup of the original reply", sent)
		}
	})

	t.Run("a text command with a reply", func(t *testing.T) {
		session, _ := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
		ic := replyInteraction(true)
		stored := &discordgo.Message{ID: storedMessageID}
		defer RegisterResponder(ic.Token, newResponder(session, stored))()

		if message, err := GetResponseMessage(session, ic); err != nil || message != stored {
			t.Errorf("GetResponseMessage = (%v, %v), want the stored reply", message, err)
		}
	})

	for name, hasResponder := range map[string]bool{"a text command before its reply": true, "a text command without responder": false} {
		t.Run(name, func(t *testing.T) {
			session, _ := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
			ic := replyInteraction(true)
			if hasResponder {
				defer RegisterResponder(ic.Token, newResponder(session, nil))()
			}

			if message, err := GetResponseMessage(session, ic); err == nil || message != nil {
				t.Errorf("GetResponseMessage = (%v, %v), want an error", message, err)
			}
		})
	}
}

func TestSendMessageKeepsOnlyADeliveredReply(t *testing.T) {
	for name, check := range map[string]struct {
		status        int
		wantMessageID string
	}{
		"a delivered reply": {http.StatusOK, "333"},
		"a rejected reply":  {http.StatusBadRequest, ""},
	} {
		t.Run(name, func(t *testing.T) {
			session, _ := discordtest.StubAPI(t, discordtest.Status(check.status))
			responder := newResponder(session, nil)

			responder.SendMessage("hello")

			got := ""
			if responder.Message != nil {
				got = responder.Message.ID
			}
			if got != check.wantMessageID {
				t.Errorf("stored message = %q, want %q", got, check.wantMessageID)
			}
		})
	}
}

func TestCheckUserInBotVoiceChannel(t *testing.T) {
	const (
		guildID      = "voice-check-guild"
		callerID     = "caller"
		voiceChannel = "voice-1"
	)
	errors := messages.T(guildID).Errors

	for name, check := range map[string]struct {
		isInVoice     bool
		queueVoice    string
		wantChannel   string
		wantEmbedText string
	}{
		"a caller outside voice":                {false, "", "", errors.NotInVoiceChannel},
		"a queue without a voice channel":       {true, "", voiceChannel, ""},
		"a queue in another voice channel":      {true, "voice-2", "", errors.MustBeInBotChannel},
		"a queue in the caller's voice channel": {true, voiceChannel, voiceChannel, ""},
	} {
		t.Run(name, func(t *testing.T) {
			queuetest.SeedWithVoice(t, guildID, check.queueVoice, queuetest.Songs(callerID)...)
			var states []*discordgo.VoiceState
			if check.isInVoice {
				states = append(states, discordtest.VoiceState(guildID, callerID, voiceChannel, false))
			}
			session := discordtest.SessionWithGuild(t, "bot", guildID, states, nil)
			ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: guildID, Member: discordtest.Member(guildID, callerID)}}

			channel, embed := CheckUserInBotVoiceChannel(session, ic)

			if channel != check.wantChannel {
				t.Errorf("channel = %q, want %q", channel, check.wantChannel)
			}
			switch {
			case check.wantEmbedText == "" && embed != nil:
				t.Errorf("got the error %q, want none", embed.Description)
			case check.wantEmbedText != "" && (embed == nil || embed.Description != check.wantEmbedText):
				t.Errorf("got %v, want the error %q", embed, check.wantEmbedText)
			}
		})
	}
}
