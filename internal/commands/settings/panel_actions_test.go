package settings

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/guild"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

func panelActionSession(t *testing.T, status int) (*discordgo.Session, func() []discordtest.Request) {
	t.Helper()

	registerPanelRoutes()
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
	return discord.ComponentID(categoryRoute, discord.ViewArgument(isAdmin))
}

func pickID(category string) string {
	return discord.ComponentID(pickRoute, discord.ViewArgument(true), category)
}

func modalID(category, key string) string {
	return discord.ComponentID(modalRoute, discord.ViewArgument(true), category, key)
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
			&discordgo.TextInput{CustomID: modalValueID, Value: value},
		},
	}
}

func choiceInput(value string) discordgo.MessageComponent {
	return &discordgo.Label{Component: &discordgo.SelectMenu{CustomID: modalValueID, Values: []string{value}}}
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

func storedValue(t *testing.T, key string) string {
	t.Helper()

	value, ok := currentValue(checkGuildID, specFor(t, key))
	if !ok {
		t.Fatalf("failed to read %s", key)
	}
	return value
}

func TestSwitchingCategoryRedrawsThePanelOnTheChosenCategory(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)

	firePanel(t, session, componentInteraction(categoryID(true), adminMember(), categoryMixing))

	sent := requests()
	assertReplies(t, sent, discordgo.InteractionResponseUpdateMessage)
	if title, _ := discordtest.JSONAt(t, sent[0].Body, "data", "embeds", 0, "title").(string); !strings.Contains(title, categoryLabel(checkGuildID, categoryMixing)) {
		t.Errorf("the redrawn title %q does not name the mixing category", title)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 1, "components", 0, "custom_id"); got != pickID(categoryMixing) {
		t.Errorf("the redrawn picker routes to %v, want %q", got, pickID(categoryMixing))
	}
}

func TestPickingSettingsOpensTheRightControl(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	pick := pickID(categoryPlayback)

	seedSetting(t, "sponsorblock", valueOff)

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
		if got := discordtest.JSONAt(t, sent[index].Body, "data", "custom_id"); got != modalID(categoryPlayback, key) {
			t.Errorf("the %s modal routes to %v, want %q", key, got, modalID(categoryPlayback, key))
		}
	}
	for index, key := range modalKeys[1:] {
		if got := discordtest.JSONAt(t, sent[index+1].Body, "data", "components", 0, "component", "type"); got != float64(discordgo.SelectMenuComponent) {
			t.Errorf("the %s modal holds a component of type %v, want a string select", key, got)
		}
	}
	if got := storedValue(t, "sponsorblock"); got != valueOn {
		t.Errorf("picking sponsorblock left it %q, want it toggled on", got)
	}
}

func TestChoosingFromASelectModal(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	t.Cleanup(func() { guild.InvalidateCaches(checkGuildID) })
	language := modalID(categoryGeneral, "language")

	firePanel(t, session, modalInteraction(language, adminMember(), choiceInput("ko")))
	if got := storedValue(t, "language"); got != "ko" {
		t.Errorf("choosing ko stored %q", got)
	}

	firePanel(t, session, modalInteraction(modalID(categoryPlayback, "repeat"), plainMember(), choiceInput(valueRepeatSingle)))
	if got := storedValue(t, "repeat"); got != valueRepeatSingle {
		t.Errorf("choosing single repeat stored %q", got)
	}

	firePanel(t, session, modalInteraction(language, plainMember(), choiceInput("en")))
	firePanel(t, session, modalInteraction(language, adminMember(), choiceInput("xx")))
	if got := storedValue(t, "language"); got != "ko" {
		t.Errorf("a refused or unknown choice changed the language to %q", got)
	}

	firePanel(t, session, modalInteraction(language, adminMember(), choiceInput(defaultChoiceValue)))
	if got := storedValue(t, "language"); got != "" {
		t.Errorf("choosing the default stored language %q", got)
	}

	assertReplies(t, requests(),
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
		discordgo.InteractionResponseChannelMessageWithSource,
		discordgo.InteractionResponseUpdateMessage,
	)
}

func TestTogglingAnUnreadableSettingReportsIt(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	dbtest.CloseUntilCleanup(t)

	firePanel(t, session, componentInteraction(pickID(categoryPlayback), adminMember(), "sponsorblock"))

	assertReplies(t, requests(), discordgo.InteractionResponseChannelMessageWithSource)
}

func TestARejectedFormFallsBackToAnErrorReply(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusBadRequest)

	firePanel(t, session, componentInteraction(pickID(categoryPlayback), adminMember(), "volume"))

	if got := len(requests()); got != 2 {
		t.Errorf("sent %d requests, want the form and the fallback error reply", got)
	}
}

func TestModalSubmissions(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	volume := modalID(categoryPlayback, "volume")

	firePanel(t, session, modalInteraction(modalID(categoryPlayback, "nope"), adminMember(), textInput("80")))
	firePanel(t, session, modalInteraction(modalID(categoryGeneral, "prefix"), plainMember(), textInput("80")))
	firePanel(t, session, modalInteraction(volume, adminMember()))
	firePanel(t, session, modalInteraction(volume, adminMember(), textInput("80")))
	firePanel(t, session, modalInteraction(volume, adminMember(), textInput("5000")))

	assertReplies(t, requests(),
		discordgo.InteractionResponseChannelMessageWithSource,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
	)
	if got := storedValue(t, "volume"); got != "80" {
		t.Errorf("volume = %q after the submissions, want 80", got)
	}
}

func TestPanelRepliesSurviveDiscordRejectingThem(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusBadRequest)

	firePanel(t, session, componentInteraction(categoryID(true), adminMember(), categoryMixing))
	firePanel(t, session, componentInteraction(pickID(categoryGeneral), plainMember(), "prefix"))

	if got := len(requests()); got != 2 {
		t.Errorf("sent %d requests, want one redraw and one error reply attempt", got)
	}
}
