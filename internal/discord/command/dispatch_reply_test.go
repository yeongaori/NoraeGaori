package command

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/discordtest"
)

const replyGuildID = "reply-guild"

func slashProbe(name string, member *discordgo.Member) *discordgo.InteractionCreate {
	return discordtest.SlashInteraction(replyGuildID, name, member)
}

func TestHandleInteractionAnswersEveryRefusalAndFailure(t *testing.T) {
	errors := messages.T(replyGuildID).Errors
	plain := discordtest.Member(replyGuildID, "plain")
	owner := discordtest.Member(replyGuildID, "owner")

	for name, check := range map[string]struct {
		ic          *discordgo.InteractionCreate
		wantRun     string
		wantReply   string
		wantNoReply bool
	}{
		"an unknown command":                {ic: slashProbe("ghost", plain), wantReply: errors.UnknownCommand},
		"no member":                         {ic: slashProbe("probe", nil), wantReply: errors.GuildOnly},
		"a member without a user":           {ic: slashProbe("probe", &discordgo.Member{}), wantReply: errors.GuildOnly},
		"an admin command for a plain user": {ic: slashProbe("probeadmin", plain), wantReply: errors.AdminOnly},
		"an admin command for the owner":    {ic: slashProbe("probeadmin", owner), wantRun: "probeadmin", wantNoReply: true},
		"a member command":                  {ic: slashProbe("probe", plain), wantRun: "probe", wantNoReply: true},
		"a failing command":                 {ic: slashProbe("probefail", plain), wantRun: "probefail", wantReply: "probe failed"},
		"a ping":                            {ic: &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionPing, GuildID: replyGuildID}}, wantNoReply: true},
	} {
		t.Run(name, func(t *testing.T) {
			ran := map[string]*bool{
				"probe":      registerProbeCommand(t, "probe", false),
				"probeadmin": registerProbeCommand(t, "probeadmin", true),
				"probefail":  registerProbe(t, "probefail", false, failingHandler),
			}
			session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
			discordtest.AddGuild(t, session, replyGuildID)
			guild, _ := session.State.Guild(replyGuildID)
			guild.OwnerID = "owner"

			HandleInteraction(session, check.ic)

			for command, hasRun := range ran {
				if *hasRun != (command == check.wantRun) {
					t.Errorf("%s ran = %v, want %v", command, *hasRun, command == check.wantRun)
				}
			}
			sent := requests()
			if check.wantNoReply {
				if len(sent) != 0 {
					t.Errorf("sent %v, want no reply", sent)
				}
				return
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d replies, want one", len(sent))
			}
			if text := discordtest.EmbedText(discordtest.ReplyEmbed(t, &sent[0])); !strings.Contains(text, check.wantReply) {
				t.Errorf("the reply %q does not contain %q", text, check.wantReply)
			}
		})
	}
}
