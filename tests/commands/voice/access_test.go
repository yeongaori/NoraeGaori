package voice_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/voice"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

const thirdChannelID = "third-channel"

func addListener(t *testing.T, fixture *commandtest.Fixture, userID, channelID string) {
	t.Helper()

	guild, err := fixture.Session.State.Guild(commandtest.GuildID)
	if err != nil {
		t.Fatalf("failed to read the seeded guild: %v", err)
	}
	guild.VoiceStates = append(guild.VoiceStates, discordtest.VoiceState(commandtest.GuildID, userID, channelID, false))
}

func TestLeaveRefusesOtherChannel(t *testing.T) {
	probe := stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.OtherID, 1)...)
	addListener(t, fixture, commandtest.OtherID, otherChannelID)

	reply := invoke(t, fixture, "leave", voice.HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Errors.MustBeInBotChannel)
	wantCalls(t, probe)
}

func TestSwitchVCRefusesOtherChannel(t *testing.T) {
	probe := stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.OtherID, 1)...)
	addListener(t, fixture, commandtest.OtherID, otherChannelID)

	reply := invoke(t, fixture, "switchvc", voice.HandleSwitchVC)

	wantText(t, reply, messages.T(commandtest.GuildID).Errors.MustBeInBotChannel)
	wantCalls(t, probe)
}

func TestJoinRefusesOtherChannel(t *testing.T) {
	probe := stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.OtherID, 1)...)
	addListener(t, fixture, commandtest.OtherID, otherChannelID)

	reply := invoke(t, fixture, "join", voice.HandleJoin)

	wantText(t, reply, messages.T(commandtest.GuildID).Errors.MustBeInBotChannel)
	wantCalls(t, probe)
}

func TestLeaveAllowsEmptyChannel(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, otherChannelID)

	reply := invoke(t, fixture, "leave", voice.HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.LeaveSuccessDesc)
	wantCalls(t, probe, "cancel skip votes", "suspend")
}

func TestLeaveAllowsSameChannel(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, commandtest.VoiceChannelID)
	addListener(t, fixture, commandtest.OtherID, commandtest.VoiceChannelID)

	reply := invoke(t, fixture, "leave", voice.HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.LeaveSuccessDesc)
	wantCalls(t, probe, "cancel skip votes", "suspend")
}

func TestLeaveIgnoresOtherChannels(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, otherChannelID)
	addListener(t, fixture, commandtest.OtherID, thirdChannelID)

	reply := invoke(t, fixture, "leave", voice.HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.LeaveSuccessDesc)
	wantCalls(t, probe, "cancel skip votes", "suspend")
}

func makeAdmin(t *testing.T, fixture *commandtest.Fixture) {
	t.Helper()

	guild, err := fixture.Session.State.Guild(commandtest.GuildID)
	if err != nil {
		t.Fatalf("failed to read the seeded guild: %v", err)
	}
	guild.OwnerID = commandtest.CallerID
}

func promptButtonIDs(t *testing.T, requests []discordtest.Request) []string {
	t.Helper()

	request := commandtest.LastRequest(t, requests)
	buttons, ok := discordtest.JSONAt(t, request.Body, "data", "components", 0, "components").([]any)
	if !ok {
		t.Fatalf("the prompt has no buttons: %v", request.Body)
	}
	customIDs := make([]string, 0, len(buttons))
	for i := range buttons {
		customIDs = append(customIDs, discordtest.JSONAt(t, buttons[i], "custom_id").(string))
	}
	return customIDs
}

func createAdminPrompt(t *testing.T, name string, handle commandtest.Handler) (*commandtest.Fixture, *playerProbe, []string) {
	t.Helper()

	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, otherChannelID)
	addListener(t, fixture, commandtest.OtherID, otherChannelID)
	makeAdmin(t, fixture)

	invoke(t, fixture, name, handle)
	return fixture, probe, promptButtonIDs(t, fixture.Requests())
}

func TestLeaveAsksAdmin(t *testing.T) {
	fixture, probe, customIDs := createAdminPrompt(t, "leave", voice.HandleLeave)

	if len(customIDs) != 2 {
		t.Fatalf("got %d buttons, want 2", len(customIDs))
	}
	wantText(t, discordtest.EmbedText(commandtest.LastReply(t, fixture.Requests())),
		fmt.Sprintf(messages.T(commandtest.GuildID).Voice.ConfirmLeaveDesc, "<#"+otherChannelID+">"))
	wantCalls(t, probe)
}

func TestSwitchVCAsksAdmin(t *testing.T) {
	fixture, probe, customIDs := createAdminPrompt(t, "switchvc", voice.HandleSwitchVC)

	if len(customIDs) != 2 {
		t.Fatalf("got %d buttons, want 2", len(customIDs))
	}
	wantText(t, discordtest.EmbedText(commandtest.LastReply(t, fixture.Requests())),
		fmt.Sprintf(messages.T(commandtest.GuildID).Voice.ConfirmMoveDesc, "<#"+otherChannelID+">", "<#"+commandtest.VoiceChannelID+">"))
	wantCalls(t, probe)
}

func TestVoiceConfirmButtons(t *testing.T) {
	cases := []struct {
		name           string
		button         int
		presserID      string
		moveBot        bool
		isMove         bool
		isPresserAdmin bool
		isDemoted      bool
		wantReply      func(locale *messages.Locale) string
		wantCalls      []string
	}{
		{name: "the requester confirms", presserID: commandtest.CallerID, wantCalls: []string{"cancel skip votes", "suspend"}},
		{name: "the requester confirms a move", presserID: commandtest.CallerID, isMove: true, wantCalls: []string{"suspend", "join " + commandtest.VoiceChannelID}},
		{name: "another user presses", presserID: commandtest.OtherID, wantReply: func(locale *messages.Locale) string { return locale.Errors.AdminOnly }},
		{name: "another admin presses", presserID: commandtest.OtherID, isPresserAdmin: true, wantReply: func(locale *messages.Locale) string { return locale.Errors.AdminOnly }},
		{name: "the requester lost admin", presserID: commandtest.CallerID, isDemoted: true, wantReply: func(locale *messages.Locale) string { return locale.Errors.AdminOnly }},
		{name: "the requester cancels", button: 1, presserID: commandtest.CallerID, wantReply: func(locale *messages.Locale) string { return locale.Voice.ConfirmCancelled }},
		{name: "the bot moved", presserID: commandtest.CallerID, moveBot: true, wantReply: func(locale *messages.Locale) string { return locale.Voice.ConfirmStale }},
	}

	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			name, handle := "leave", commandtest.Handler(voice.HandleLeave)
			if testCase.isMove {
				name, handle = "switchvc", voice.HandleSwitchVC
			}
			fixture, probe, customIDs := createAdminPrompt(t, name, handle)
			guild, _ := fixture.Session.State.Guild(commandtest.GuildID)
			if testCase.moveBot {
				for _, voiceState := range guild.VoiceStates {
					if voiceState.UserID == botID {
						voiceState.ChannelID = commandtest.VoiceChannelID
					}
				}
			}
			if testCase.isDemoted {
				guild.OwnerID = ""
			}

			_, arguments, _ := strings.Cut(customIDs[testCase.button], ":")
			press := discordtest.ComponentInteraction(commandtest.GuildID, customIDs[testCase.button], discordtest.Member(commandtest.GuildID, testCase.presserID))
			if testCase.isPresserAdmin {
				press.Member.Permissions = discordgo.PermissionAdministrator
			}
			voice.HookConfirmVoiceAction(fixture.Session, press, strings.Split(arguments, ":"))

			wantCalls(t, probe, testCase.wantCalls...)
			if testCase.wantReply != nil {
				wantText(t, discordtest.EmbedText(commandtest.LastReply(t, fixture.Requests())), testCase.wantReply(messages.T(commandtest.GuildID)))
			}
		})
	}
}

func invokeSwitchTo(t *testing.T, fixture *commandtest.Fixture, channelID string) string {
	t.Helper()

	if err := fixture.Session.State.ChannelAdd(&discordgo.Channel{ID: channelID, GuildID: commandtest.GuildID, Type: discordgo.ChannelTypeGuildVoice}); err != nil {
		t.Fatalf("failed to seed the channel: %v", err)
	}
	if err := fixture.Session.State.MemberAdd(discordtest.Member(commandtest.GuildID, commandtest.CallerID)); err != nil {
		t.Fatalf("failed to seed the caller: %v", err)
	}

	interaction := discordtest.SlashInteraction(commandtest.GuildID, "switchvc", discordtest.Member(commandtest.GuildID, commandtest.CallerID),
		&discordgo.ApplicationCommandInteractionDataOption{Name: "channel", Type: discordgo.ApplicationCommandOptionChannel, Value: channelID})
	if err := voice.HandleSwitchVC(fixture.Session, interaction); err != nil {
		t.Errorf("the handler returned %v, want nil", err)
	}
	return discordtest.EmbedText(commandtest.LastReply(t, fixture.Requests()))
}

func TestSwitchVCRefusesWithoutConnect(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, "")

	reply := invokeSwitchTo(t, fixture, otherChannelID)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.ConnectDenied)
	wantCalls(t, probe)
}

func TestSwitchVCAllowsConnect(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, "")
	guild, err := fixture.Session.State.Guild(commandtest.GuildID)
	if err != nil {
		t.Fatalf("failed to read the seeded guild: %v", err)
	}
	guild.Roles = append(guild.Roles, &discordgo.Role{ID: commandtest.GuildID, Permissions: discordgo.PermissionVoiceConnect})

	reply := invokeSwitchTo(t, fixture, otherChannelID)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.SwitchSuccessTitle)
	wantCalls(t, probe, "suspend", "join "+otherChannelID)
}
