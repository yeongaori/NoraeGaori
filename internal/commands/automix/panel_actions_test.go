package automix

import (
	"cmp"
	"net/http"
	"strconv"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func panelFixture(t *testing.T, songCount int) *commandtest.Fixture {
	t.Helper()
	return commandtest.NewFixture(t, nil, queuetest.SongsBy(commandtest.CallerID, songCount)...)
}

func firstSongID(seeded *queue.Queue) string {
	return strconv.Itoa(seeded.Songs[0].ID)
}

func missingSongID(*queue.Queue) string {
	return "999"
}

func TestTransitionRoutesIgnoreMalformedInteractions(t *testing.T) {
	fixture := panelFixture(t, 3)
	songID := firstSongID(fixture.Queue)
	component := fixture.Component(transitionPickRoute)
	picked := fixture.Component(transitionPickRoute, "not-a-song")
	styled := fixture.Component(transitionStyleRoute, firstStyle("volume"))
	modal := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:    discordgo.InteractionModalSubmit,
		GuildID: commandtest.GuildID,
		Data:    discordgo.ModalSubmitInteractionData{CustomID: transitionPageRoute},
	}}

	turnTransitionPage(fixture.Session, component, nil)
	turnTransitionPage(fixture.Session, component, []string{"two"})
	turnTransitionPage(fixture.Session, modal, []string{"2"})
	pickTransition(fixture.Session, component, []string{"1"})
	pickTransition(fixture.Session, picked, []string{"1"})
	chooseTransitionStyle(fixture.Session, component, []string{"volume", songID, "123", "1"})
	chooseTransitionStyle(fixture.Session, styled, []string{"volume", songID, "123"})
	chooseTransitionStyle(fixture.Session, styled, []string{"volume", "not-a-song", "123", "1"})

	fixture.WantNoRequests(t)
}

func TestTheAutoMixPanelCommand(t *testing.T) {
	commandtest.Run(t, "automixpanel", HandleAutoMixPanel, []commandtest.Case{
		{
			Name:     "an empty queue",
			WantText: func(locale *messages.Locale) string { return locale.AutoMixPanel.EmptyTitle },
		},
		{
			Name:     "a page past the end",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 12),
			Options:  []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("page", 9)},
			WantText: func(*messages.Locale) string { return "3/3" },
		},
	})
}

func TestTurningATransitionPage(t *testing.T) {
	fixture := panelFixture(t, 12)

	turnTransitionPage(fixture.Session, fixture.Component(transitionPageRoute), []string{"2"})

	sent := fixture.Requests()
	commandtest.WantSingleResponse(t, sent, discordgo.InteractionResponseUpdateMessage)
	commandtest.WantReplyText(t, sent, "2/3")
}

func TestPickingATransitionOpensItsEditor(t *testing.T) {
	panelEditPath := "/channels/" + discordtest.ChannelID + "/messages/" + discordtest.PanelMessageID

	for _, check := range []struct {
		name       string
		songValue  func(*queue.Queue) string
		wantEditor bool
	}{
		{"a queued song", firstSongID, true},
		{"a song that left the queue", missingSongID, false},
	} {
		t.Run(check.name, func(t *testing.T) {
			fixture := panelFixture(t, 3)
			songValue := check.songValue(fixture.Queue)

			pickTransition(fixture.Session, fixture.Component(transitionPickRoute, songValue), []string{"1"})

			sent := fixture.Requests()
			if len(sent) != 2 {
				t.Fatalf("sent %d requests, want the editor reply and the panel refresh", len(sent))
			}
			if !discordtest.IsEphemeral(&sent[0]) {
				t.Error("the editor reply was not private")
			}
			if sent[1].Method != http.MethodPatch || sent[1].Path != panelEditPath {
				t.Errorf("the second request is %s %s, want PATCH %s", sent[1].Method, sent[1].Path, panelEditPath)
			}

			if !check.wantEditor {
				commandtest.WantReplyText(t, sent[:1], messages.T(commandtest.GuildID).AutoMixPanel.SongGone)
				return
			}
			want := discord.ComponentID(transitionStyleRoute, transitionCategories[0], songValue, discordtest.PanelMessageID, "1")
			if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 0, "components", 0, "custom_id"); got != want {
				t.Errorf("the editor routes to %v, want %q", got, want)
			}
		})
	}
}

func TestChoosingATransitionStyle(t *testing.T) {
	volumeStyle := firstStyle("volume")

	for _, check := range []struct {
		name         string
		guildID      string
		songValue    func(*queue.Queue) string
		style        string
		wantRequests int
		wantText     func(locale *messages.Locale) string
		wantClosed   bool
		wantSaved    bool
	}{
		{
			name:         "an unknown style",
			songValue:    firstSongID,
			style:        "bogus",
			wantRequests: 1,
			wantText: func(locale *messages.Locale) string {
				return commandtest.FormatPrefix(locale.AutoMixPanel.UpdateFailed)
			},
		},
		{
			name:         "a saved style",
			songValue:    firstSongID,
			style:        volumeStyle,
			wantRequests: 2,
			wantText:     func(locale *messages.Locale) string { return locale.AutoMixPanel.EffectiveField },
			wantSaved:    true,
		},
		{
			name:         "a song that left the queue",
			songValue:    missingSongID,
			style:        volumeStyle,
			wantRequests: 1,
			wantText:     func(locale *messages.Locale) string { return locale.AutoMixPanel.SongGone },
			wantClosed:   true,
		},
		{
			name:         "a guild without a queue",
			guildID:      "guild-without-queue",
			songValue:    firstSongID,
			style:        volumeStyle,
			wantRequests: 1,
			wantText:     func(locale *messages.Locale) string { return locale.AutoMixPanel.SongGone },
			wantClosed:   true,
		},
	} {
		t.Run(check.name, func(t *testing.T) {
			fixture := panelFixture(t, 3)
			guildID := cmp.Or(check.guildID, commandtest.GuildID)
			ic := discordtest.ComponentInteraction(guildID, transitionStyleRoute, discordtest.Member(guildID, commandtest.CallerID), check.style)

			chooseTransitionStyle(fixture.Session, ic, []string{"volume", check.songValue(fixture.Queue), discordtest.PanelMessageID, "1"})

			sent := fixture.Requests()
			if len(sent) != check.wantRequests {
				t.Fatalf("sent %d requests, want %d", len(sent), check.wantRequests)
			}
			if got := discordtest.ResponseType(&sent[0]); got != discordgo.InteractionResponseUpdateMessage {
				t.Errorf("the editor response type = %d, want an in-place update", got)
			}
			commandtest.WantReplyText(t, sent[:1], check.wantText(messages.T(guildID)))
			if components, _ := discordtest.JSONAt(t, sent[0].Body, "data", "components").([]any); (len(components) == 0) != check.wantClosed {
				t.Errorf("the editor kept %d component rows, want closed=%v", len(components), check.wantClosed)
			}
			if !check.wantSaved {
				return
			}
			if refresh := sent[1]; refresh.Method != http.MethodPatch || refresh.Path != "/channels/"+discordtest.ChannelID+"/messages/"+discordtest.PanelMessageID {
				t.Errorf("the panel refresh went to %s %s", refresh.Method, refresh.Path)
			}
			if saved, err := queue.GetQueue(commandtest.GuildID, true); err != nil || saved.Songs[0].AutoMixStyleVolume != check.style {
				t.Errorf("the saved song style is wrong (err %v)", err)
			}
		})
	}
}

func TestVoiceChannelBitrateOnlyAsksDiscordForAKnownChannel(t *testing.T) {
	queuetest.Seed(t, commandtest.GuildID, queuetest.SongsBy(commandtest.CallerID, 1)...)
	session, requests := discordtest.StubAPIResponder(t, func(*http.Request) (int, string) {
		return http.StatusOK, `{"id":"voice-1","type":2,"bitrate":64000}`
	})

	if got := voiceChannelBitrate(session, commandtest.GuildID); got != 0 || len(requests()) != 0 {
		t.Fatalf("bitrate without a voice channel = %d after %d requests, want 0 and none", got, len(requests()))
	}

	if err := queue.UpdateVoiceChannel(commandtest.GuildID, "voice-1"); err != nil {
		t.Fatalf("failed to set the voice channel: %v", err)
	}
	queue.InvalidateCache(commandtest.GuildID)

	if got := voiceChannelBitrate(session, commandtest.GuildID); got != 64000 {
		t.Errorf("bitrate = %d, want the channel's 64000", got)
	}
	if sent := requests(); len(sent) != 1 || sent[0].Path != "/channels/voice-1" {
		t.Errorf("sent %v, want one lookup of the voice channel", sent)
	}
}
