package discord

import (
	"net/http"
	"slices"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/testutil/discordtest"
)

func routeInteraction(interactionType discordgo.InteractionType, guildID, customID string, values ...string) *discordgo.InteractionCreate {
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: interactionType, GuildID: guildID}}
	if interactionType == discordgo.InteractionModalSubmit {
		ic.Data = discordgo.ModalSubmitInteractionData{CustomID: customID}
	} else {
		ic.Data = discordgo.MessageComponentInteractionData{CustomID: customID, Values: values}
	}
	return ic
}

func registerTestRoute(t *testing.T, prefix string) *[][]string {
	t.Helper()

	var calls [][]string
	RegisterComponentRoute(prefix, func(_ *discordgo.Session, _ *discordgo.InteractionCreate, arguments []string) {
		calls = append(calls, arguments)
	})
	t.Cleanup(func() { componentRoutes.Delete(prefix) })
	return &calls
}

func TestComponentIDJoinsThePrefixAndArguments(t *testing.T) {
	if got := ComponentID("settings_modal", "admin", "mixing", "fadein_duration"); got != "settings_modal:admin:mixing:fadein_duration" {
		t.Errorf("ComponentID = %q", got)
	}
	if got := ComponentID("queue_mix"); got != "queue_mix" {
		t.Errorf("ComponentID without arguments = %q, want the bare prefix", got)
	}
}

func TestComponentRoutesReceiveTheirArguments(t *testing.T) {
	calls := registerTestRoute(t, "test_route")

	for _, ic := range []*discordgo.InteractionCreate{
		routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test_route:1:two:3"),
		routeInteraction(discordgo.InteractionModalSubmit, menuGuildID, "test_route"),
		routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test_route:"),
		routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test_route::1"),
	} {
		if !HandleComponentRoute(nil, ic) {
			t.Errorf("interaction type %v was not routed", ic.Type)
		}
	}

	if len(*calls) != 4 {
		t.Fatalf("the route ran %d times, want 4", len(*calls))
	}
	if got := (*calls)[0]; !slices.Equal(got, []string{"1", "two", "3"}) {
		t.Errorf("the component route received %q, want every argument", got)
	}
	if (*calls)[1] != nil {
		t.Errorf("the modal route received %v, want no arguments", (*calls)[1])
	}
	if got := (*calls)[2]; !slices.Equal(got, []string{""}) {
		t.Errorf("a trailing separator gave %q, want one empty argument", got)
	}
	if got := (*calls)[3]; !slices.Equal(got, []string{"", "1"}) {
		t.Errorf("a doubled separator gave %q, want an empty argument before 1", got)
	}
}

func TestComponentRoutesIgnoreUnknownPrefixesMissingGuildsAndCommands(t *testing.T) {
	calls := registerTestRoute(t, "test_ignored")

	command := routeInteraction(discordgo.InteractionApplicationCommand, menuGuildID, "")
	command.Data = discordgo.ApplicationCommandInteractionData{Name: "test_ignored"}

	for name, ic := range map[string]*discordgo.InteractionCreate{
		"an unknown prefix":            routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test_unknown:1"),
		"a prefix without a separator": routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test_ignored_1"),
		"no guild":                     routeInteraction(discordgo.InteractionMessageComponent, "", "test_ignored:1"),
		"a command":                    command,
	} {
		if HandleComponentRoute(nil, ic) {
			t.Errorf("%s was routed", name)
		}
	}

	if len(*calls) != 0 {
		t.Errorf("ignored interactions ran the route with %v", *calls)
	}
}

func TestInteractionsWithoutDataAreIgnoredSafely(t *testing.T) {
	calls := registerTestRoute(t, "test_empty")

	for _, interactionType := range []discordgo.InteractionType{discordgo.InteractionMessageComponent, discordgo.InteractionModalSubmit} {
		ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: interactionType, GuildID: menuGuildID}}

		if HandleComponentRoute(nil, ic) {
			t.Errorf("interaction type %v without data was routed", interactionType)
		}
		if _, isSelected := SelectedValue(ic); isSelected {
			t.Errorf("interaction type %v without data had a selected value", interactionType)
		}
		if _, hasPage := PageArgument(ic, []string{"1"}, 1); hasPage {
			t.Errorf("interaction type %v without data had a page", interactionType)
		}
		if _, isModal := ModalComponents(ic); isModal {
			t.Errorf("interaction type %v without data had modal components", interactionType)
		}
	}

	if len(*calls) != 0 {
		t.Errorf("interactions without data ran the route with %v", *calls)
	}
}

func TestModalComponentsOnlyReadModalSubmissions(t *testing.T) {
	modal := routeInteraction(discordgo.InteractionModalSubmit, menuGuildID, "test:1")
	modal.Data = discordgo.ModalSubmitInteractionData{
		CustomID:   "test:1",
		Components: []discordgo.MessageComponent{&discordgo.TextInput{CustomID: "field", Value: "80"}},
	}

	components, isModal := ModalComponents(modal)
	if !isModal || len(components) != 1 {
		t.Fatalf("ModalComponents = (%v, %v), want the one submitted field", components, isModal)
	}
	if field, isText := components[0].(*discordgo.TextInput); !isText || field.Value != "80" {
		t.Errorf("the submitted field is %+v, want the value 80", components[0])
	}
	if _, isModal := ModalComponents(routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test:1")); isModal {
		t.Error("a component interaction was read as a modal submission")
	}
}

func TestViewArgumentsRoundTrip(t *testing.T) {
	if ViewArgument(true) != "admin" || ViewArgument(false) != "member" {
		t.Errorf("views = %q and %q, want admin and member as already posted in custom IDs", ViewArgument(true), ViewArgument(false))
	}
	for _, isAdmin := range []bool{true, false} {
		parsed, isValid := ParseViewArgument(ViewArgument(isAdmin))
		if !isValid || parsed != isAdmin {
			t.Errorf("the admin=%v view parsed as (%v, %v)", isAdmin, parsed, isValid)
		}
	}
	if _, isValid := ParseViewArgument("owner"); isValid {
		t.Error("an unknown view was accepted")
	}
}

func TestSelectedValueAndPageArgumentOnlyReadComponents(t *testing.T) {
	pick := routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test:2", "first", "second")
	empty := routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test:2")
	modal := routeInteraction(discordgo.InteractionModalSubmit, menuGuildID, "test:2")

	if value, isSelected := SelectedValue(pick); !isSelected || value != "first" {
		t.Errorf("SelectedValue = (%q, %v), want the first value", value, isSelected)
	}
	for name, ic := range map[string]*discordgo.InteractionCreate{"no values": empty, "a modal": modal} {
		if _, isSelected := SelectedValue(ic); isSelected {
			t.Errorf("SelectedValue found a value for %s", name)
		}
	}

	if page, hasPage := PageArgument(pick, []string{"admin", "2"}, 2); !hasPage || page != 2 {
		t.Errorf("PageArgument = (%d, %v), want page 2", page, hasPage)
	}
	for name, check := range map[string]struct {
		ic        *discordgo.InteractionCreate
		arguments []string
		count     int
	}{
		"a modal":         {modal, []string{"2"}, 1},
		"the wrong count": {pick, []string{"2"}, 2},
		"no arguments":    {pick, nil, 0},
		"a word":          {pick, []string{"two"}, 1},
	} {
		if _, hasPage := PageArgument(check.ic, check.arguments, check.count); hasPage {
			t.Errorf("PageArgument accepted %s", name)
		}
	}
}

func TestUpdateComponentMessageClearsMissingComponents(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	ic := interactionWithToken(routeInteraction(discordgo.InteractionMessageComponent, menuGuildID, "test_route:1"))

	if err := UpdateComponentMessage(session, ic, &discordgo.MessageEmbed{Title: "page"}, nil); err != nil {
		t.Fatalf("UpdateComponentMessage returned %v", err)
	}

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want one interaction callback", len(sent))
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "type"); got != float64(discordgo.InteractionResponseUpdateMessage) {
		t.Errorf("reply type = %v, want an in-place update", got)
	}
	if components, ok := discordtest.JSONAt(t, sent[0].Body, "data", "components").([]any); !ok || len(components) != 0 {
		t.Errorf("components = %v, want an empty list that clears the old ones", components)
	}
}
