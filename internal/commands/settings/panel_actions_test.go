package settings

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/guild"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

func panelActionSession(t *testing.T, status int) (*discordgo.Session, func() []discordtest.Request) {
	t.Helper()

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

func newActionPanel() *panelSession {
	return &panelSession{guildID: checkGuildID, token: checkToken, panelAdmin: true, category: categoryPlayback}
}

func adminMember() *discordgo.Member {
	return memberWithRoles("boss", adminRoleID)
}

func plainMember() *discordgo.Member {
	return memberWithRoles("regular", memberRoleID)
}

func modalInteraction(key string, member *discordgo.Member, components ...discordgo.MessageComponent) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionModalSubmit,
			GuildID: checkGuildID,
			Member:  member,
			Data: discordgo.ModalSubmitInteractionData{
				CustomID:   customID(modalPrefix, key, checkToken),
				Components: components,
			},
		},
	}
}

func volumeInput(value string) discordgo.MessageComponent {
	return &discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			&discordgo.TextInput{CustomID: inputPrefix + "volume", Value: value},
		},
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

func TestSwitchingCategoryRedrawsThePanel(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	panel := newActionPanel()

	handlePanelInteraction(session, componentInteraction(categoryPrefix+checkToken, adminMember(), categoryMixing), panel)

	assertReplies(t, requests(), discordgo.InteractionResponseUpdateMessage)
	if got := panel.currentCategory(); got != categoryMixing {
		t.Errorf("category = %q, want %q", got, categoryMixing)
	}
}

func TestPickingSettingsOpensTheRightControl(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	panel := newActionPanel()
	pick := pickPrefix + checkToken

	seedSetting(t, "sponsorblock", valueOff)

	handlePanelInteraction(session, componentInteraction(pick, adminMember(), "volume"), panel)
	handlePanelInteraction(session, componentInteraction(pick, adminMember(), "language"), panel)
	handlePanelInteraction(session, componentInteraction(pick, adminMember(), "sponsorblock"), panel)
	handlePanelInteraction(session, componentInteraction(pick, plainMember(), "prefix"), panel)

	assertReplies(t, requests(),
		discordgo.InteractionResponseModal,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
	)
	if got := panel.currentOpenSetting(); got != "language" {
		t.Errorf("open setting = %q, want the language value list", got)
	}
	if got := storedValue(t, "sponsorblock"); got != valueOn {
		t.Errorf("picking sponsorblock left it %q, want it toggled on", got)
	}
}

func TestChoosingFromTheValueList(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	panel := newActionPanel()
	choose := customID(choicePrefix, "language", checkToken)
	t.Cleanup(func() { guild.InvalidateCaches(checkGuildID) })

	panel.setOpenSetting("language")
	handlePanelInteraction(session, componentInteraction(choose, adminMember(), backValue), panel)
	if got := panel.currentOpenSetting(); got != "" {
		t.Errorf("going back left %q open", got)
	}
	if got := storedValue(t, "language"); got != "" {
		t.Errorf("going back stored language %q", got)
	}

	handlePanelInteraction(session, componentInteraction(choose, adminMember(), "ko"), panel)
	if got := storedValue(t, "language"); got != "ko" {
		t.Errorf("choosing ko stored %q", got)
	}

	handlePanelInteraction(session, componentInteraction(customID(choicePrefix, "sponsorblock", checkToken), adminMember(), valueOn), panel)
	handlePanelInteraction(session, componentInteraction(choose, plainMember(), "en"), panel)

	assertReplies(t, requests(),
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseUpdateMessage,
		discordgo.InteractionResponseChannelMessageWithSource,
	)
	if got := storedValue(t, "language"); got != "ko" {
		t.Errorf("a plain member changed the language to %q", got)
	}
}

func TestTogglingAnUnreadableSettingReportsIt(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	closeDatabaseUntilCleanup(t)

	handlePanelInteraction(session, componentInteraction(pickPrefix+checkToken, adminMember(), "sponsorblock"), newActionPanel())

	assertReplies(t, requests(), discordgo.InteractionResponseChannelMessageWithSource)
}

func TestARejectedFormFallsBackToAnErrorReply(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusBadRequest)

	handlePanelInteraction(session, componentInteraction(pickPrefix+checkToken, adminMember(), "volume"), newActionPanel())

	if got := len(requests()); got != 2 {
		t.Errorf("sent %d requests, want the form and the fallback error reply", got)
	}
}

func TestModalSubmissions(t *testing.T) {
	dbtest.Setup(t)
	session, requests := panelActionSession(t, http.StatusOK)
	panel := newActionPanel()

	handlePanelInteraction(session, modalInteraction("nope", adminMember(), volumeInput("80")), panel)
	handlePanelInteraction(session, modalInteraction("prefix", plainMember(), volumeInput("80")), panel)
	handlePanelInteraction(session, modalInteraction("volume", adminMember()), panel)
	handlePanelInteraction(session, modalInteraction("volume", adminMember(), volumeInput("80")), panel)
	handlePanelInteraction(session, modalInteraction("volume", adminMember(), volumeInput("5000")), panel)

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
	panel := newActionPanel()

	handlePanelInteraction(session, componentInteraction(categoryPrefix+checkToken, adminMember(), categoryMixing), panel)
	handlePanelInteraction(session, componentInteraction(pickPrefix+checkToken, plainMember(), "prefix"), panel)

	if got := len(requests()); got != 2 {
		t.Errorf("sent %d requests, want one redraw and one error reply attempt", got)
	}
}
