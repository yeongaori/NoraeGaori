package command

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/testutil/discordtest"
)

func autocompleteInteraction(name string, member *discordgo.Member, user *discordgo.User, options ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:      discordtest.InteractionID,
			AppID:   discordtest.InteractionAppID,
			Token:   discordtest.InteractionToken,
			Type:    discordgo.InteractionApplicationCommandAutocomplete,
			GuildID: "guild",
			Member:  member,
			User:    user,
			Data:    discordgo.ApplicationCommandInteractionData{Name: name, Options: options},
		},
	}
}

func registerAutocompleteProbe(t *testing.T, name string, choiceCount int) *AutocompleteRequest {
	t.Helper()

	received := &AutocompleteRequest{}
	registerTestCommand(t, &Command{
		Name: name,
		AutocompleteHandler: func(request AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
			*received = request
			choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, choiceCount)
			for index := range choiceCount {
				choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: fmt.Sprint(index), Value: fmt.Sprint(index)})
			}
			return choices
		},
	})
	return received
}

func focusedQuery(value any, optionType discordgo.ApplicationCommandOptionType) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: "query", Type: optionType, Value: value, Focused: true}
}

func TestAutocompleteHandsTheFocusedQueryToTheCommand(t *testing.T) {
	received := registerAutocompleteProbe(t, "probecomplete", 30)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	member := &discordgo.Member{User: &discordgo.User{ID: "member-user"}}

	HandleAutocomplete(session, autocompleteInteraction("probecomplete", member, nil, focusedQuery("abc", discordgo.ApplicationCommandOptionString)))

	want := AutocompleteRequest{GuildID: "guild", UserID: "member-user", CommandName: "probecomplete", Query: "abc"}
	if *received != want {
		t.Errorf("the handler received %+v, want %+v", *received, want)
	}

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want one autocomplete answer", len(sent))
	}
	if got := discordtest.ResponseType(&sent[0]); got != discordgo.InteractionApplicationCommandAutocompleteResult {
		t.Errorf("response type = %d, want an autocomplete result", got)
	}
	choices, _ := discordtest.JSONAt(t, sent[0].Body, "data", "choices").([]any)
	if len(choices) != MaxAutocompleteChoices {
		t.Fatalf("answered with %d choices, want %d", len(choices), MaxAutocompleteChoices)
	}
	for index, choice := range choices {
		if name, _ := choice.(map[string]any)["name"].(string); name != fmt.Sprint(index) {
			t.Errorf("choice %d is %q, want the handler's choices kept in order from the first", index, name)
		}
	}
}

func TestAutocompleteUsesTheDirectMessageUser(t *testing.T) {
	received := registerAutocompleteProbe(t, "probedm", 1)
	session, _ := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	HandleAutocomplete(session, autocompleteInteraction("probedm", nil, &discordgo.User{ID: "dm-user"}))

	if received.UserID != "dm-user" {
		t.Errorf("user ID = %q, want the direct message user", received.UserID)
	}
}

func TestFocusedStringOptionOnlyReadsAFocusedText(t *testing.T) {
	unfocused := &discordgo.ApplicationCommandInteractionDataOption{Name: "query", Type: discordgo.ApplicationCommandOptionString, Value: "ignored"}

	for name, check := range map[string]struct {
		options []*discordgo.ApplicationCommandInteractionDataOption
		want    string
	}{
		"nothing focused":            {[]*discordgo.ApplicationCommandInteractionDataOption{unfocused}, ""},
		"a focused number option":    {[]*discordgo.ApplicationCommandInteractionDataOption{focusedQuery(float64(3), discordgo.ApplicationCommandOptionInteger)}, ""},
		"a focused text of a number": {[]*discordgo.ApplicationCommandInteractionDataOption{focusedQuery(float64(3), discordgo.ApplicationCommandOptionString)}, ""},
		"a focused text":             {[]*discordgo.ApplicationCommandInteractionDataOption{nil, unfocused, focusedQuery("abc", discordgo.ApplicationCommandOptionString)}, "abc"},
	} {
		if got := focusedStringOption(check.options); got != check.want {
			t.Errorf("%s: focusedStringOption = %q, want %q", name, got, check.want)
		}
	}
}

func TestInteractionUserIDPrefersTheMember(t *testing.T) {
	for name, check := range map[string]struct {
		member *discordgo.Member
		user   *discordgo.User
		want   string
	}{
		"a guild member":        {&discordgo.Member{User: &discordgo.User{ID: "member"}}, &discordgo.User{ID: "user"}, "member"},
		"a direct message user": {nil, &discordgo.User{ID: "user"}, "user"},
		"a member without user": {&discordgo.Member{}, nil, ""},
	} {
		ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Member: check.member, User: check.user}}
		if got := interactionUserID(ic); got != check.want {
			t.Errorf("%s: interactionUserID = %q, want %q", name, got, check.want)
		}
	}
}
