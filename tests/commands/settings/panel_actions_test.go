package settings_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord"
	"noraegaori/internal/guild"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
)

func panelActionSession(t *testing.T, status int) (*discordgo.Session, func() []discordtest.Request) {
	t.Helper()

	settings.HookRegisterPanelRoutes()
	session, requests := discordtest.StubAPI(t, discordtest.Status(status))
	err := session.State.GuildAdd(&discordgo.Guild{
		ID: checkGuildID,
		Roles: []*discordgo.Role{
			{ID: checkGuildID},
			{ID: adminRoleID, Permissions: discordgo.PermissionAdministrator},
			{ID: memberRoleID, Permissions: discordgo.PermissionSendMessages},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	return session, requests
}

func adminMember() *discordgo.Member {
	return memberWithRoles("boss", adminRoleID)
}

func plainMember() *discordgo.Member {
	return memberWithRoles("regular", memberRoleID)
}

func categoryID(isAdmin bool) string {
	return discord.ComponentID(settings.HookCategoryRoute, discord.ViewArgument(isAdmin))
}

func pickID(category string) string {
	return discord.ComponentID(settings.HookPickRoute, discord.ViewArgument(true), category)
}

func modalID(category, key string) string {
	return discord.ComponentID(settings.HookModalRoute, discord.ViewArgument(true), category, key)
}

func modalInteraction(customID string, member *discordgo.Member, components ...discordgo.MessageComponent) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionModalSubmit,
			GuildID: checkGuildID,
			Member:  member,
			Data:    discordgo.ModalSubmitInteractionData{CustomID: customID, Components: components},
		},
	}
}

func textInput(value string) discordgo.MessageComponent {
	return &discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			&discordgo.TextInput{CustomID: settings.HookModalValueID, Value: value},
		},
	}
}

func choiceInput(value string) discordgo.MessageComponent {
	return &discordgo.Label{Component: &discordgo.SelectMenu{CustomID: settings.HookModalValueID, Values: []string{value}}}
}

func firePanel(t *testing.T, session *discordgo.Session, ic *discordgo.InteractionCreate) {
	t.Helper()

	if !discord.HandleComponentRoute(session, ic) {
		t.Fatalf("interaction type %v was not routed to the settings panel", ic.Type)
	}
}

func assertReplies(t *testing.T, sent []discordtest.Request, types ...discordgo.InteractionResponseType) {
	t.Helper()

	if len(sent) != len(types) {
		t.Fatalf("sent %d replies, want %d", len(sent), len(types))
	}
	for index, want := range types {
		if got := discordtest.JSONAt(t, sent[index].Body, "type"); got != float64(want) {
			t.Errorf("reply %d type = %v, want %d", index, got, want)
		}
		if want == discordgo.InteractionResponseChannelMessageWithSource {
			if flags := discordtest.JSONAt(t, sent[index].Body, "data", "flags"); flags != float64(discordgo.MessageFlagsEphemeral) {
				t.Errorf("reply %d flags = %v, want an ephemeral error", index, flags)
			}
		}
	}
}

func wantReplyText(t *testing.T, request *discordtest.Request, want string) {
	t.Helper()

	if text := discordtest.EmbedText(discordtest.ReplyEmbed(t, request)); !strings.Contains(text, want) {
		t.Errorf("the reply %q does not contain %q", text, want)
	}
}

func storedValue(t *testing.T, key string) string {
	t.Helper()

	value, ok := settings.HookCurrentValue(checkGuildID, specFor(t, key))
	if !ok {
		t.Fatalf("failed to read %s", key)
	}
	return value
}

func TestSwitchingCategoryRedrawsThePanelOnTheChosenCategory(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)

	firePanel(t, session, componentInteraction(categoryID(true), adminMember(), settings.HookCategoryMixing))

	sent := requests()
	assertReplies(t, sent, discordgo.InteractionResponseUpdateMessage)
	if title, _ := discordtest.JSONAt(t, sent[0].Body, "data", "embeds", 0, "title").(string); !strings.Contains(title, settings.HookCategoryLabel(checkGuildID, settings.HookCategoryMixing)) {
		t.Errorf("the redrawn title %q does not name the mixing category", title)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 1, "components", 0, "custom_id"); got != pickID(settings.HookCategoryMixing) {
		t.Errorf("the redrawn picker routes to %v, want %q", got, pickID(settings.HookCategoryMixing))
	}
}

func TestPickingSettingsOpensTheRightControl(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	pick := pickID(settings.HookCategoryPlayback)

	seedSetting(t, "sponsorblock", settings.HookValueOff)

	modalKeys := []string{"volume", "language", "repeat"}
	for _, key := range modalKeys {
		firePanel(t, session, componentInteraction(pick, adminMember(), key))
	}
	firePanel(t, session, componentInteraction(pick, adminMember(), "sponsorblock"))
	firePanel(t, session, componentInteraction(pick, plainMember(), "prefix"))

	sent := requests()
	assertReplies(t, sent,
		discordgo.InteractionResponseModal,
		discordgo.InteractionResponseModal,
		discordgo.InteractionResponseModal,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
	)
	for index, key := range modalKeys {
		if got := discordtest.JSONAt(t, sent[index].Body, "data", "custom_id"); got != modalID(settings.HookCategoryPlayback, key) {
			t.Errorf("the %s modal routes to %v, want %q", key, got, modalID(settings.HookCategoryPlayback, key))
		}
	}
	for index, key := range modalKeys[1:] {
		if got := discordtest.JSONAt(t, sent[index+1].Body, "data", "components", 0, "component", "type"); got != float64(discordgo.SelectMenuComponent) {
			t.Errorf("the %s modal holds a component of type %v, want a string select", key, got)
		}
	}
	if got := storedValue(t, "sponsorblock"); got != settings.HookValueOn {
		t.Errorf("picking sponsorblock left it %q, want it toggled on", got)
	}
	wantReplyText(t, &sent[4], settings.HookPanelStrings(checkGuildID).NotAdmin)
}

func TestChoosingFromASelectModal(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	t.Cleanup(func() { guild.InvalidateCaches(checkGuildID) })
	language := modalID(settings.HookCategoryGeneral, "language")

	firePanel(t, session, modalInteraction(language, adminMember(), choiceInput("ko")))
	if got := storedValue(t, "language"); got != "ko" {
		t.Errorf("choosing ko stored %q", got)
	}

	firePanel(t, session, modalInteraction(modalID(settings.HookCategoryPlayback, "repeat"), plainMember(), choiceInput(settings.HookValueRepeatSingle)))
	if got := storedValue(t, "repeat"); got != settings.HookValueRepeatSingle {
		t.Errorf("choosing single repeat stored %q", got)
	}

	firePanel(t, session, modalInteraction(language, plainMember(), choiceInput("en")))
	firePanel(t, session, modalInteraction(language, adminMember(), choiceInput("xx")))
	if got := storedValue(t, "language"); got != "ko" {
		t.Errorf("a refused or unknown choice changed the language to %q", got)
	}

	firePanel(t, session, modalInteraction(language, adminMember(), choiceInput(settings.HookDefaultChoiceValue)))
	if got := storedValue(t, "language"); got != "" {
		t.Errorf("choosing the default stored language %q", got)
	}

	sent := requests()
	assertReplies(t, sent,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
		discordgo.InteractionResponseChannelMessageWithSource,
		discordgo.InteractionResponseUpdateMessage,
	)
	wantReplyText(t, &sent[2], settings.HookPanelStrings(checkGuildID).NotAdmin)
	wantReplyText(t, &sent[3], settings.HookValidationMessage(checkGuildID, specFor(t, "language"), *settings.HookErrUnknownValue))
}

func TestTogglingAnUnreadableSettingReportsIt(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	dbtest.CloseUntilCleanup(t)

	firePanel(t, session, componentInteraction(pickID(settings.HookCategoryPlayback), adminMember(), "sponsorblock"))

	sent := requests()
	assertReplies(t, sent, discordgo.InteractionResponseChannelMessageWithSource)
	wantReplyText(t, &sent[0], settings.HookPanelStrings(checkGuildID).ReadFailed)
}

func TestARejectedFormFallsBackToAnErrorReply(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusBadRequest)

	firePanel(t, session, componentInteraction(pickID(settings.HookCategoryPlayback), adminMember(), "volume"))

	sent := requests()
	if len(sent) != 2 {
		t.Fatalf("sent %d requests, want the form and the fallback error reply", len(sent))
	}
	if !discordtest.IsEphemeral(&sent[1]) {
		t.Error("the fallback error reply was not private")
	}
	wantReplyText(t, &sent[1], fmt.Sprintf(settings.HookPanelStrings(checkGuildID).ModalFailed, settings.HookSettingLabel(checkGuildID, "volume")))
}

func TestModalSubmissions(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	volume := modalID(settings.HookCategoryPlayback, "volume")

	firePanel(t, session, modalInteraction(modalID(settings.HookCategoryPlayback, "nope"), adminMember(), textInput("80")))
	firePanel(t, session, modalInteraction(modalID(settings.HookCategoryGeneral, "prefix"), plainMember(), textInput("80")))
	firePanel(t, session, modalInteraction(volume, adminMember()))
	firePanel(t, session, modalInteraction(volume, adminMember(), textInput("80")))
	firePanel(t, session, modalInteraction(volume, adminMember(), textInput("5000")))

	sent := requests()
	assertReplies(t, sent,
		discordgo.InteractionResponseChannelMessageWithSource,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
	)
	wantReplyText(t, &sent[0], settings.HookPanelStrings(checkGuildID).NotAdmin)
	wantReplyText(t, &sent[2], settings.HookValidationMessage(checkGuildID, specFor(t, "volume"), *settings.HookErrOutOfRange))
	if got := storedValue(t, "volume"); got != "80" {
		t.Errorf("volume = %q after the submissions, want 80", got)
	}
}

func TestPanelRepliesSurviveDiscordRejectingThem(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusBadRequest)

	firePanel(t, session, componentInteraction(categoryID(true), adminMember(), settings.HookCategoryMixing))
	firePanel(t, session, componentInteraction(pickID(settings.HookCategoryGeneral), plainMember(), "prefix"))

	sent := requests()
	if len(sent) != 2 {
		t.Fatalf("sent %d requests, want one redraw and one error reply attempt", len(sent))
	}
	wantReplyText(t, &sent[1], settings.HookPanelStrings(checkGuildID).NotAdmin)
}
