package automix_test

import (
	"cmp"
	"net/http"
	"strconv"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/commands/automix"
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
	component := fixture.Component(automix.HookTransitionPickRoute)
	picked := fixture.Component(automix.HookTransitionPickRoute, "not-a-song")
	styled := fixture.Component(automix.HookTransitionStyleRoute, firstStyle(transition.CategoryVolumeOut))
	modal := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:    discordgo.InteractionModalSubmit,
		GuildID: commandtest.GuildID,
		Data:    discordgo.ModalSubmitInteractionData{CustomID: automix.HookTransitionPageRoute},
	}}
	volumeOut := string(transition.CategoryVolumeOut)

	automix.HookTurnTransitionPage(fixture.Session, component, nil)
	automix.HookTurnTransitionPage(fixture.Session, component, []string{"two"})
	automix.HookTurnTransitionPage(fixture.Session, modal, []string{"2"})
	automix.HookPickTransition(fixture.Session, component, []string{"1"})
	automix.HookPickTransition(fixture.Session, picked, []string{"1"})
	automix.HookChooseTransitionStyle(fixture.Session, component, []string{volumeOut, songID, "123", "1"})
	automix.HookChooseTransitionStyle(fixture.Session, styled, []string{volumeOut, songID, "123"})
	automix.HookChooseTransitionStyle(fixture.Session, styled, []string{volumeOut, "not-a-song", "123", "1"})
	automix.HookChooseTransitionStyle(fixture.Session, styled, []string{"volume", songID, "123", "1"})
	automix.HookChooseTransitionStyle(fixture.Session, styled, []string{"bogus", songID, "123", "1"})
	automix.HookTurnEditorTab(fixture.Session, component, []string{"bogus", songID, "123", "1"})
	automix.HookTurnEditorTab(fixture.Session, component, []string{"incoming", "not-a-song", "123", "1"})
	automix.HookTurnEditorTab(fixture.Session, component, []string{"incoming", songID, "123"})

	fixture.WantNoRequests(t)
}

func TestTurningAnEditorTabRedrawsItsDropdowns(t *testing.T) {
	fixture := panelFixture(t, 3)
	songID := firstSongID(fixture.Queue)

	automix.HookTurnEditorTab(fixture.Session, fixture.Component(automix.HookTransitionTabRoute), []string{"incoming", songID, discordtest.PanelMessageID, "1"})

	sent := fixture.Requests()
	commandtest.WantSingleResponse(t, sent, discordgo.InteractionResponseUpdateMessage)
	want := discord.ComponentID(automix.HookTransitionStyleRoute, string(transition.CategoryVolumeIn), songID, discordtest.PanelMessageID, "1")
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 0, "components", 0, "custom_id"); got != want {
		t.Errorf("the incoming tab starts with %v, want %q", got, want)
	}
}

func TestTheMixingSettingsButtonOpensTheMixingSettings(t *testing.T) {
	automix.HookRegisterPanelRoutes()
	fixture := panelFixture(t, 3)

	if !discord.HandleComponentRoute(fixture.Session, fixture.Component(automix.HookMixingSettingsRoute)) {
		t.Fatalf("the custom ID %q is not routed", automix.HookMixingSettingsRoute)
	}

	sent := fixture.Requests()
	commandtest.WantSingleResponse(t, sent, discordgo.InteractionResponseChannelMessageWithSource)
	if !discordtest.IsEphemeral(&sent[0]) {
		t.Error("the mixing settings were not sent privately")
	}
	commandtest.WantReplyText(t, sent, messages.T(commandtest.GuildID).SettingsPanel.Categories["mixing"])
}

func TestTheAutoMixPanelCommand(t *testing.T) {
	commandtest.Run(t, "automixpanel", automix.HandleAutoMixPanel, []commandtest.Case{
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

	automix.HookTurnTransitionPage(fixture.Session, fixture.Component(automix.HookTransitionPageRoute), []string{"2"})

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

			automix.HookPickTransition(fixture.Session, fixture.Component(automix.HookTransitionPickRoute, songValue), []string{"1"})

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
			want := discord.ComponentID(automix.HookTransitionStyleRoute, string(transition.CategoryVolumeOut), songValue, discordtest.PanelMessageID, "1")
			if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 0, "components", 0, "custom_id"); got != want {
				t.Errorf("the editor routes to %v, want %q", got, want)
			}
		})
	}
}

func TestChoosingATransitionStyle(t *testing.T) {
	volumeStyle := firstStyle(transition.CategoryVolumeOut)

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
			ic := discordtest.ComponentInteraction(guildID, automix.HookTransitionStyleRoute, discordtest.Member(guildID, commandtest.CallerID), check.style)

			automix.HookChooseTransitionStyle(fixture.Session, ic, []string{string(transition.CategoryVolumeOut), check.songValue(fixture.Queue), discordtest.PanelMessageID, "1"})

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
			if saved, err := queue.GetQueue(commandtest.GuildID, true); err != nil || saved.Songs[0].AutoMixOverrides[string(transition.CategoryVolumeOut)] != check.style {
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

	if got := automix.HookVoiceChannelBitrate(session, commandtest.GuildID); got != 0 || len(requests()) != 0 {
		t.Fatalf("bitrate without a voice channel = %d after %d requests, want 0 and none", got, len(requests()))
	}

	if err := queue.UpdateVoiceChannel(commandtest.GuildID, "voice-1"); err != nil {
		t.Fatalf("failed to set the voice channel: %v", err)
	}
	queue.InvalidateCache(commandtest.GuildID)

	if got := automix.HookVoiceChannelBitrate(session, commandtest.GuildID); got != 64000 {
		t.Errorf("bitrate = %d, want the channel's 64000", got)
	}
	if sent := requests(); len(sent) != 1 || sent[0].Path != "/channels/voice-1" {
		t.Errorf("sent %v, want one lookup of the voice channel", sent)
	}
}

func TestTheAutoMixMenuButtonOpensThePanel(t *testing.T) {
	automix.HookRegisterPanelRoutes()
	buttons := automix.HookPanelOpenButtons(commandtest.GuildID)
	if len(buttons) != 1 {
		t.Fatalf("built %d buttons, want one", len(buttons))
	}
	button, ok := buttons[0].(discordgo.Button)
	if !ok {
		t.Fatalf("built %T, want a discordgo.Button", buttons[0])
	}
	if want := messages.T(commandtest.GuildID).AutoMixPanel.OpenButton; button.Label != want {
		t.Errorf("button label = %q, want %q", button.Label, want)
	}

	for _, check := range []struct {
		name          string
		songCount     int
		wantEphemeral bool
		wantText      func(locale *messages.Locale) string
	}{
		{"an empty queue", 0, true, func(locale *messages.Locale) string { return locale.AutoMixPanel.EmptyTitle }},
		{"a queue with transitions", 3, false, func(locale *messages.Locale) string { return locale.AutoMixPanel.Title }},
	} {
		t.Run(check.name, func(t *testing.T) {
			fixture := panelFixture(t, check.songCount)

			if !discord.HandleComponentRoute(fixture.Session, fixture.Component(button.CustomID)) {
				t.Fatalf("the button custom ID %q is not routed", button.CustomID)
			}

			sent := fixture.Requests()
			commandtest.WantSingleResponse(t, sent, discordgo.InteractionResponseChannelMessageWithSource)
			if discordtest.IsEphemeral(&sent[0]) != check.wantEphemeral {
				t.Errorf("reply ephemeral = %v, want %v", !check.wantEphemeral, check.wantEphemeral)
			}
			commandtest.WantReplyText(t, sent, check.wantText(messages.T(commandtest.GuildID)))
		})
	}
}
