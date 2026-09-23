package discord

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/discordtest"
)

func interactionWithToken(ic *discordgo.InteractionCreate) *discordgo.InteractionCreate {
	ic.ID = "111"
	ic.AppID = "app"
	ic.Token = "token"
	return ic
}

const menuGuildID = "dropdown-menu-guild"

func testMenuOptions() []discordgo.SelectMenuOption {
	return []discordgo.SelectMenuOption{
		{Label: "Off", Value: "off", Default: true},
		{Label: "All", Value: "all"},
	}
}

func registerTestMenu(t *testing.T, key string, apply func(string) (*discordgo.MessageEmbed, error)) {
	t.Helper()

	RegisterDropdownMenu(key, func(string) DropdownMenu {
		return DropdownMenu{Label: "Repeat", Current: "Off", Options: testMenuOptions(), Apply: apply}
	})
	t.Cleanup(func() { dropdownMenus.Delete(key) })
}

func trackingApply(picked *[]string) func(string) (*discordgo.MessageEmbed, error) {
	return func(value string) (*discordgo.MessageEmbed, error) {
		*picked = append(*picked, value)
		return &discordgo.MessageEmbed{Title: "applied " + value}, nil
	}
}

func pickInteraction(guildID, customID string, values ...string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionMessageComponent,
			GuildID: guildID,
			Data:    discordgo.MessageComponentInteractionData{CustomID: customID, Values: values},
		},
	}
}

func TestDropdownMenuComponentsUseTheSettingKey(t *testing.T) {
	_, components := renderDropdownMenu(menuGuildID, "repeat", &DropdownMenu{Options: testMenuOptions()})
	if len(components) != 1 {
		t.Fatalf("built %d rows, want 1", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 1 {
		t.Fatalf("row is %T, want one ActionsRow holding one select", components[0])
	}
	menu, ok := row.Components[0].(discordgo.SelectMenu)
	if !ok {
		t.Fatalf("row holds %T, want discordgo.SelectMenu", row.Components[0])
	}

	if menu.CustomID != dropdownMenuPrefix+"repeat" {
		t.Errorf("custom ID = %q, want %q", menu.CustomID, dropdownMenuPrefix+"repeat")
	}
	if menu.Placeholder == "" {
		t.Error("the dropdown has no placeholder")
	}
	if len(menu.Options) != 2 || !menu.Options[0].Default || menu.Options[1].Default {
		t.Errorf("options %+v did not keep their values and default flags", menu.Options)
	}
}

func TestDropdownMenuEmbedShowsTheCurrentValue(t *testing.T) {
	embed, _ := renderDropdownMenu(menuGuildID, "repeat", &DropdownMenu{Label: "Repeat", Current: "Off"})

	if embed.Title != "Repeat" {
		t.Errorf("title = %q, want the setting label", embed.Title)
	}
	if !strings.Contains(embed.Description, "Off") {
		t.Errorf("description %q does not show the current value", embed.Description)
	}
}

func TestDropdownPickIgnoresForeignAndInvalidPicks(t *testing.T) {
	var picked []string
	registerTestMenu(t, "test_ignore", trackingApply(&picked))
	customID := dropdownMenuPrefix + "test_ignore"

	notComponent := pickInteraction(menuGuildID, customID, "all")
	notComponent.Type = discordgo.InteractionApplicationCommand

	for name, ic := range map[string]*discordgo.InteractionCreate{
		"no guild":         pickInteraction("", customID, "all"),
		"another panel":    pickInteraction(menuGuildID, "settings_pick_token", "all"),
		"unregistered key": pickInteraction(menuGuildID, dropdownMenuPrefix+"test_missing", "all"),
		"no value":         pickInteraction(menuGuildID, customID),
		"unoffered value":  pickInteraction(menuGuildID, customID, "single"),
		"not a component":  notComponent,
	} {
		if _, _, isApplied := applyDropdownPick(ic); isApplied {
			t.Errorf("a pick from %s was applied", name)
		}
	}

	if len(picked) != 0 {
		t.Errorf("ignored picks applied %v", picked)
	}
}

func TestDropdownPickAppliesEveryPickWithoutAnyPriorState(t *testing.T) {
	var picked []string
	registerTestMenu(t, "test_reuse", trackingApply(&picked))
	customID := dropdownMenuPrefix + "test_reuse"

	for _, value := range []string{"all", "off", "all"} {
		key, failure, isApplied := applyDropdownPick(pickInteraction(menuGuildID, customID, value))
		if !isApplied || failure != nil {
			t.Fatalf("pick %q = (applied %v, failure %v), want applied without failure", value, isApplied, failure)
		}
		if key != "test_reuse" {
			t.Errorf("pick %q resolved key %q", value, key)
		}
	}

	if strings.Join(picked, ",") != "all,off,all" {
		t.Errorf("Apply ran with %v, want every pick in order", picked)
	}
}

func TestDropdownPickReturnsTheFailureEmbed(t *testing.T) {
	registerTestMenu(t, "test_failure", func(string) (*discordgo.MessageEmbed, error) {
		return &discordgo.MessageEmbed{Title: "failed"}, errors.New("database is locked")
	})

	_, failure, isApplied := applyDropdownPick(pickInteraction(menuGuildID, dropdownMenuPrefix+"test_failure", "all"))
	if !isApplied || failure == nil || failure.Title != "failed" {
		t.Errorf("pick = (failure %v, applied %v), want the failure embed", failure, isApplied)
	}
}

func TestRespondDropdownMenuRejectsAnUnregisteredKey(t *testing.T) {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: menuGuildID}}

	if err := RespondDropdownMenu(nil, ic, "test_missing"); err == nil {
		t.Error("an unregistered dropdown was sent")
	}
}

func TestHandleDropdownMenuPickIgnoresUnrelatedComponents(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	HandleDropdownMenuPick(session, pickInteraction(menuGuildID, "settings_pick_token", "on"))
	HandleDropdownMenuPick(session, pickInteraction(menuGuildID, dropdownMenuPrefix+"test_missing", "on"))

	if sent := requests(); len(sent) != 0 {
		t.Errorf("sent %v, want unrelated and unregistered picks ignored", sent)
	}
}

func TestRespondDropdownMenuSendsTheMenuForASlashCommand(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	var picked []string
	registerTestMenu(t, "test_send", trackingApply(&picked))

	ic := interactionWithToken(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{Type: discordgo.InteractionApplicationCommand, GuildID: menuGuildID},
	})
	if err := RespondDropdownMenu(session, ic, "test_send"); err != nil {
		t.Fatalf("RespondDropdownMenu returned %v, want nil", err)
	}

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want only the interaction reply", len(sent))
	}
	if sent[0].Method != "POST" || sent[0].Path != "/api/interactions/111/token/callback" {
		t.Errorf("first request = %s %s, want the interaction callback", sent[0].Method, sent[0].Path)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "type"); got != float64(discordgo.InteractionResponseChannelMessageWithSource) {
		t.Errorf("reply type = %v, want a channel message", got)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 0, "components", 0, "custom_id"); got != dropdownMenuPrefix+"test_send" {
		t.Errorf("dropdown custom ID = %v, want %q", got, dropdownMenuPrefix+"test_send")
	}
}

func TestSendEmbedWithComponentsRepliesWithoutFetchingTheMessage(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	ic := interactionWithToken(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{Type: discordgo.InteractionApplicationCommand, GuildID: menuGuildID},
	})

	if message, err := SendEmbedWithComponents(session, ic, &discordgo.MessageEmbed{}, nil); err != nil || message != nil {
		t.Fatalf("SendEmbedWithComponents = (%v, %v), want no message and no error", message, err)
	}

	sent := requests()
	if len(sent) != 1 || sent[0].Path != "/api/interactions/111/token/callback" {
		t.Errorf("sent %v, want only the interaction callback", sent)
	}
}

func TestSendEmbedWithComponentsSendsTextCommandRepliesToTheChannel(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: menuGuildID, Token: "message_111_222"}}

	if _, err := SendEmbedWithComponents(session, ic, &discordgo.MessageEmbed{}, nil); err == nil {
		t.Error("a text command without a responder returned no error")
	}

	responder := &MessageResponse{Session: session, ChannelID: "222", OriginalMsgID: "111"}
	defer RegisterResponder(ic.Token, responder)()
	message, err := SendEmbedWithComponents(session, ic, &discordgo.MessageEmbed{}, nil)
	if err != nil || message == nil {
		t.Fatalf("SendEmbedWithComponents = (%v, %v), want the sent channel message", message, err)
	}
	if responder.Message != message {
		t.Error("the sent reply was not kept for later edits")
	}

	sent := requests()
	if len(sent) != 1 || sent[0].Method != "POST" || sent[0].Path != "/channels/222/messages" {
		t.Fatalf("sent %v, want exactly one channel message and no interaction lookup", sent)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "message_reference", "message_id"); got != "111" {
		t.Errorf("the reply references %v, want the command message 111", got)
	}
}

func TestHandleDropdownMenuPickRedrawsTheMenuInPlace(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	current := "off"
	RegisterDropdownMenu("test_redraw", func(string) DropdownMenu {
		return DropdownMenu{
			Label:   "Repeat",
			Current: current,
			Options: []discordgo.SelectMenuOption{
				{Label: "Off", Value: "off", Default: current == "off"},
				{Label: "All", Value: "all", Default: current == "all"},
			},
			Apply: func(value string) (*discordgo.MessageEmbed, error) {
				current = value
				return nil, nil
			},
		}
	})
	t.Cleanup(func() { dropdownMenus.Delete("test_redraw") })

	HandleDropdownMenuPick(session, interactionWithToken(pickInteraction(menuGuildID, dropdownMenuPrefix+"test_redraw", "all")))

	sent := requests()
	if len(sent) != 1 || sent[0].Path != "/api/interactions/111/token/callback" {
		t.Fatalf("sent %v, want one interaction callback", sent)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "type"); got != float64(discordgo.InteractionResponseUpdateMessage) {
		t.Errorf("reply type = %v, want an in-place message update", got)
	}
	if got, _ := discordtest.JSONAt(t, sent[0].Body, "data", "embeds", 0, "description").(string); !strings.Contains(got, "all") {
		t.Errorf("redrawn description %q does not show the new value", got)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "components", 0, "components", 0, "options", 1, "default"); got != true {
		t.Errorf("the picked option is not preselected after the redraw (default = %v)", got)
	}
}

func TestHandleDropdownMenuPickRepliesPrivatelyWhenApplyFails(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	registerTestMenu(t, "test_private_failure", func(string) (*discordgo.MessageEmbed, error) {
		return &discordgo.MessageEmbed{Title: "failed"}, errors.New("database is locked")
	})

	HandleDropdownMenuPick(session, interactionWithToken(pickInteraction(menuGuildID, dropdownMenuPrefix+"test_private_failure", "all")))

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want one private reply", len(sent))
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "type"); got != float64(discordgo.InteractionResponseChannelMessageWithSource) {
		t.Errorf("reply type = %v, want a new message", got)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "flags"); got != float64(discordgo.MessageFlagsEphemeral) {
		t.Errorf("reply flags = %v, want ephemeral", got)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "embeds", 0, "title"); got != "failed" {
		t.Errorf("reply title = %v, want the failure embed", got)
	}
}

func TestDropdownRepliesSurviveDiscordRejectingThem(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusBadRequest))
	var picked []string
	registerTestMenu(t, "test_rejected", trackingApply(&picked))
	registerTestMenu(t, "test_rejected_failure", func(string) (*discordgo.MessageEmbed, error) {
		return &discordgo.MessageEmbed{Title: "failed"}, errors.New("database is locked")
	})

	command := interactionWithToken(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{Type: discordgo.InteractionApplicationCommand, GuildID: menuGuildID},
	})
	if err := RespondDropdownMenu(session, command, "test_rejected"); err == nil {
		t.Error("a rejected menu reply returned no error")
	}

	HandleDropdownMenuPick(session, interactionWithToken(pickInteraction(menuGuildID, dropdownMenuPrefix+"test_rejected", "all")))
	HandleDropdownMenuPick(session, interactionWithToken(pickInteraction(menuGuildID, dropdownMenuPrefix+"test_rejected_failure", "all")))

	if got := len(requests()); got != 3 {
		t.Errorf("sent %d requests, want one attempt per reply", got)
	}
	if len(picked) != 1 {
		t.Errorf("the pick was applied %d times, want once even though the redraw was rejected", len(picked))
	}
}

func TestDropdownMenuStringsAreLocalized(t *testing.T) {
	t.Cleanup(func() {
		if err := messages.LoadLocale("en"); err != nil {
			t.Errorf("failed to restore the English locale: %v", err)
		}
	})

	for _, lang := range []string{"en", "ko"} {
		if err := messages.LoadLocale(lang); err != nil {
			t.Fatalf("failed to load %s: %v", lang, err)
		}
		strs := messages.T().DropdownMenu
		if !strings.Contains(strs.Current, "%s") {
			t.Errorf("%s dropdown_menu.current %q has no %%s for the value", lang, strs.Current)
		}
		if strs.Placeholder == "" {
			t.Errorf("%s dropdown_menu.placeholder is empty", lang)
		}
	}
}
