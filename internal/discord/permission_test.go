package discord

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/testutil/discordtest"
)

func memberOf(userID string, roleIDs ...string) *discordgo.Member {
	return &discordgo.Member{User: &discordgo.User{ID: userID}, Roles: roleIDs}
}

func TestEveryoneRoleCanGrantAdministrator(t *testing.T) {
	const guildID = "everyone-admin-guild"
	session := &discordgo.Session{State: discordgo.NewState()}
	err := session.State.GuildAdd(&discordgo.Guild{
		ID:      guildID,
		OwnerID: "owner",
		Roles: []*discordgo.Role{
			{ID: guildID, Permissions: discordgo.PermissionAdministrator},
			{ID: "member-role", Permissions: discordgo.PermissionSendMessages},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}

	if !IsGuildAdmin(session, guildID, memberOf("anyone")) {
		t.Error("a member was refused although @everyone holds Administrator")
	}
	if !IsGuildAdmin(session, guildID, memberOf("anyone", "member-role", "deleted-role")) {
		t.Error("an extra role or a deleted role hid the Administrator from @everyone")
	}
}

func TestGuildAdminFallsBackToDiscordForAnUncachedGuild(t *testing.T) {
	const guildID = "uncached-guild"
	adminBits := strconv.FormatInt(discordgo.PermissionAdministrator, 10)
	session, requests := discordtest.StubAPIResponder(t, func(*http.Request) (int, string) {
		return http.StatusOK, `{"id":"` + guildID + `","owner_id":"owner","roles":[{"id":"` + guildID + `","permissions":"0"},{"id":"admin-role","permissions":"` + adminBits + `"}]}`
	})

	for name, check := range map[string]struct {
		member    *discordgo.Member
		wantAdmin bool
	}{
		"the owner":            {memberOf("owner"), true},
		"an administrator":     {memberOf("someone", "admin-role"), true},
		"a plain member":       {memberOf("someone"), false},
		"a member of no roles": {memberOf("someone", "deleted-role"), false},
	} {
		if got := IsGuildAdmin(session, guildID, check.member); got != check.wantAdmin {
			t.Errorf("%s: IsGuildAdmin = %v, want %v", name, got, check.wantAdmin)
		}
	}
	if sent := requests(); len(sent) == 0 || sent[0].Path != "/guilds/"+guildID {
		t.Errorf("sent %v, want a lookup of the guild", sent)
	}
}

func TestGuildAdminRefusesWhenTheGuildCannotBeFound(t *testing.T) {
	session, _ := discordtest.StubAPI(t, discordtest.Status(http.StatusNotFound))

	if IsGuildAdmin(session, "missing-guild", memberOf("owner")) {
		t.Error("a member of a guild Discord cannot find was treated as an admin")
	}
	if IsGuildAdmin(session, "missing-guild", nil) || IsGuildAdmin(session, "missing-guild", &discordgo.Member{}) {
		t.Error("a missing member was treated as an admin")
	}
}
