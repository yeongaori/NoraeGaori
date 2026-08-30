package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func guildAdminSession(t *testing.T, guildID, roleID string) *discordgo.Session {
	t.Helper()

	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "bot"}
	guild := &discordgo.Guild{
		ID:      guildID,
		OwnerID: "owner",
		Roles: []*discordgo.Role{
			{ID: guildID, Permissions: discordgo.PermissionViewChannel},
			{ID: roleID, Permissions: discordgo.PermissionAdministrator},
		},
	}
	if err := session.State.GuildAdd(guild); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	return session
}

func TestInteractionMemberRejectsInteractionsWithoutAMember(t *testing.T) {
	cases := map[string]*discordgo.InteractionCreate{
		"nil interaction": nil,
		"nil member": {
			Interaction: &discordgo.Interaction{GuildID: "guild"},
		},
		"member without a user": {
			Interaction: &discordgo.Interaction{GuildID: "guild", Member: &discordgo.Member{}},
		},
	}

	for name, interaction := range cases {
		t.Run(name, func(t *testing.T) {
			if got := InteractionMember(interaction); got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

func TestInteractionMemberReturnsThePopulatedMember(t *testing.T) {
	member := &discordgo.Member{User: &discordgo.User{ID: "user"}}
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{GuildID: "guild", Member: member},
	}

	if got := InteractionMember(interaction); got != member {
		t.Errorf("got %+v, want the seeded member", got)
	}
}

func TestIsAdminMemberAcceptsAMemberHoldingAnAdministratorRole(t *testing.T) {
	session := guildAdminSession(t, "guild", "admins")
	member := &discordgo.Member{
		GuildID: "guild",
		User:    &discordgo.User{ID: "user"},
		Roles:   []string{"admins"},
	}

	if !IsAdminMember(session, "guild", member) {
		t.Error("a member with an administrator role was not treated as an admin")
	}
}

func TestIsAdminMemberRejectsAMemberWithoutAdministrator(t *testing.T) {
	session := guildAdminSession(t, "guild", "admins")
	member := &discordgo.Member{
		GuildID: "guild",
		User:    &discordgo.User{ID: "user"},
	}

	if IsAdminMember(session, "guild", member) {
		t.Error("a member without an administrator role was treated as an admin")
	}
}

func TestIsAdminMemberRejectsAMissingMember(t *testing.T) {
	session := guildAdminSession(t, "guild", "admins")

	if IsAdminMember(session, "guild", nil) {
		t.Error("a nil member was treated as an admin")
	}
	if IsAdminMember(session, "guild", &discordgo.Member{}) {
		t.Error("a member without a user was treated as an admin")
	}
}
