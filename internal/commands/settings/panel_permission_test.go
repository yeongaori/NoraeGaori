package settings

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
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

func componentInteraction(customID string, member *discordgo.Member, values ...string) *discordgo.InteractionCreate {
	return discordtest.ComponentInteraction(checkGuildID, customID, member, values...)
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

	if !allowedToEdit(session, componentInteraction("", memberWithRoles("regular", memberRoleID)), specFor(t, "sponsorblock")) {
		t.Error("a plain member was refused a non-admin setting")
	}
}

func TestAdminOnlySettingsStayOpenToAdmins(t *testing.T) {
	session := guildSession(t)

	if !allowedToEdit(session, componentInteraction("", memberWithRoles("boss", adminRoleID)), specFor(t, "prefix")) {
		t.Error("an administrator was refused an admin-only setting")
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

func TestThePanelIgnoresMalformedCustomIDs(t *testing.T) {
	dbtest.Setup(t)
	registerPanelRoutes()

	assertSponsorBlockUnchanged(t, func() {
		for _, id := range []string{
			pickRoute + ":owner:" + categoryPlayback,
			pickRoute + ":admin",
			pickRoute + ":admin:nonsense",
			pickRoute + ":member:" + categoryGeneral,
			pickRoute + ":admin:" + categoryPlayback + ":extra",
		} {
			discord.HandleComponentRoute(nil, componentInteraction(id, nil, "sponsorblock"))
		}
		discord.HandleComponentRoute(nil, modalInteraction(modalRoute+":admin:"+categoryPlayback, nil, choiceInput(valueOff)))
	})
}

func TestThePanelIgnoresEmptyUnknownAndMismatchedSelections(t *testing.T) {
	dbtest.Setup(t)
	registerPanelRoutes()
	pick := pickID(categoryPlayback)

	assertSponsorBlockUnchanged(t, func() {
		for _, ic := range []*discordgo.InteractionCreate{
			componentInteraction(pick, nil),
			componentInteraction(pick, nil, "no-such-setting"),
			componentInteraction(categoryID(true), nil),
			componentInteraction(categoryID(true), nil, "no-such-category"),
			componentInteraction(categoryID(false), nil, categoryGeneral),
			componentInteraction(modalID(categoryPlayback, "sponsorblock"), nil, valueOff),
			modalInteraction(pick, nil, choiceInput("sponsorblock")),
		} {
			discord.HandleComponentRoute(nil, ic)
		}
	})
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
