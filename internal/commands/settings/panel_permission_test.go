package settings

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

const (
	adminRoleID  = "role-admin"
	memberRoleID = "role-member"
)

func guildSession(t *testing.T) *discordgo.Session {
	t.Helper()

	session := discordtest.Session(t, "bot")
	err := session.State.GuildAdd(&discordgo.Guild{
		ID: checkGuildID,
		Roles: []*discordgo.Role{
			{ID: checkGuildID, Permissions: 0},
			{ID: adminRoleID, Permissions: discordgo.PermissionAdministrator},
			{ID: memberRoleID, Permissions: discordgo.PermissionSendMessages},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	return session
}

func memberWithRoles(userID string, roles ...string) *discordgo.Member {
	return &discordgo.Member{
		GuildID: checkGuildID,
		User:    &discordgo.User{ID: userID},
		Roles:   roles,
	}
}

func TestAServerAdministratorMayEditAdminSettings(t *testing.T) {
	session := guildSession(t)

	if !canEditAdminSettings(session, checkGuildID, memberWithRoles("boss", adminRoleID)) {
		t.Error("a member holding the Administrator role was refused")
	}
}

func TestAPlainMemberMayNotEditAdminSettings(t *testing.T) {
	session := guildSession(t)

	if canEditAdminSettings(session, checkGuildID, memberWithRoles("regular", memberRoleID)) {
		t.Error("a member without the Administrator role was allowed")
	}
	if canEditAdminSettings(session, checkGuildID, memberWithRoles("roleless")) {
		t.Error("a member with no roles at all was allowed")
	}
}

func TestNonAdminSettingsAreOpenToEveryone(t *testing.T) {
	session := guildSession(t)
	interaction := componentInteraction("panel-message", "", memberWithRoles("regular", memberRoleID))
	panelSess := &panelSession{guildID: checkGuildID, token: checkToken, messageID: "panel-message"}

	if !allowedToEdit(session, interaction, panelSess, specFor(t, "sponsorblock")) {
		t.Error("a plain member was refused a non-admin setting")
	}
}

func TestAdminOnlySettingsStayOpenToAdmins(t *testing.T) {
	session := guildSession(t)
	interaction := componentInteraction("panel-message", "", memberWithRoles("boss", adminRoleID))
	panelSess := &panelSession{guildID: checkGuildID, token: checkToken, messageID: "panel-message"}

	if !allowedToEdit(session, interaction, panelSess, specFor(t, "prefix")) {
		t.Error("an administrator was refused an admin-only setting")
	}
}

func componentInteraction(messageID, customID string, member *discordgo.Member, values ...string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionMessageComponent,
			GuildID: checkGuildID,
			Member:  member,
			Message: &discordgo.Message{ID: messageID},
			Data:    discordgo.MessageComponentInteractionData{CustomID: customID, Values: values},
		},
	}
}

func assertSponsorBlockUnchanged(t *testing.T, fire func()) {
	t.Helper()

	spec := specFor(t, "sponsorblock")
	if err := applySetting(checkGuildID, spec, valueOn); err != nil {
		t.Fatalf("failed to seed sponsorblock: %v", err)
	}

	fire()

	after, ok := currentValue(checkGuildID, spec)
	if !ok {
		t.Fatal("could not read sponsorblock back")
	}
	if after != valueOn {
		t.Errorf("an ignored interaction changed sponsorblock to %q", after)
	}
}

func TestThePanelIgnoresInteractionsFromOtherMessages(t *testing.T) {
	dbtest.Setup(t)

	panelSess := &panelSession{guildID: checkGuildID, token: checkToken, messageID: "panel-message"}
	pick := pickPrefix + checkToken

	assertSponsorBlockUnchanged(t, func() {
		handlePanelInteraction(nil, componentInteraction("another-message", pick, nil, "sponsorblock"), panelSess)

		noMessage := componentInteraction("", pick, nil, "sponsorblock")
		noMessage.Message = nil
		handlePanelInteraction(nil, noMessage, panelSess)
	})
}

func TestThePanelIgnoresUnrelatedComponentsOnItsOwnMessage(t *testing.T) {
	dbtest.Setup(t)

	panelSess := &panelSession{guildID: checkGuildID, token: checkToken, messageID: "panel-message"}

	assertSponsorBlockUnchanged(t, func() {
		for _, id := range []string{
			"automix_pick_" + checkToken,
			"help_next",
			pickPrefix + "other-token",
		} {
			handlePanelInteraction(nil, componentInteraction("panel-message", id, nil, "sponsorblock"), panelSess)
		}
	})
}

func TestThePanelIgnoresModalSubmitsFromAnotherPanel(t *testing.T) {
	dbtest.Setup(t)

	panelSess := &panelSession{guildID: checkGuildID, token: checkToken, messageID: "panel-message"}

	assertSponsorBlockUnchanged(t, func() {
		foreign := &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				Type:    discordgo.InteractionModalSubmit,
				GuildID: checkGuildID,
				Data: discordgo.ModalSubmitInteractionData{
					CustomID: customID(modalPrefix, "sponsorblock", "other-token"),
				},
			},
		}
		handlePanelInteraction(nil, foreign, panelSess)
	})
}

func TestThePickerIgnoresEmptyAndUnknownSelections(t *testing.T) {
	dbtest.Setup(t)

	panelSess := &panelSession{guildID: checkGuildID, token: checkToken, messageID: "panel-message"}
	pick := pickPrefix + checkToken

	assertSponsorBlockUnchanged(t, func() {
		handlePanelInteraction(nil, componentInteraction("panel-message", pick, nil), panelSess)
		handlePanelInteraction(nil, componentInteraction("panel-message", pick, nil, "no-such-setting"), panelSess)

		choice := customID(choicePrefix, "language", checkToken)
		handlePanelInteraction(nil, componentInteraction("panel-message", choice, nil), panelSess)

		category := categoryPrefix + checkToken
		handlePanelInteraction(nil, componentInteraction("panel-message", category, nil), panelSess)
		handlePanelInteraction(nil, componentInteraction("panel-message", category, nil, "no-such-category"), panelSess)
	})

	if got := panelSess.currentCategory(); got != "" {
		t.Errorf("an unknown category selection set the panel to %q", got)
	}
}

func TestLanguageChoicesCoverEveryAvailableLocale(t *testing.T) {
	choices := BuildLanguageChoices()

	if len(choices) == 0 {
		t.Fatal("no language choices were built")
	}
	for _, choice := range choices {
		if choice.Name == "" || choice.Value == "" {
			t.Errorf("choice %+v has an empty name or value", choice)
		}
	}
}
