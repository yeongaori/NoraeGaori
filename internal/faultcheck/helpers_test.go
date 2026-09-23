//go:build faults

package faultcheck

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord/command"
)

func TestOwnerRotationCyclesThroughTheOwners(t *testing.T) {
	if got := OwnerRotation(5, "a", "b"); !slices.Equal(got, []string{"a", "b", "a", "b", "a"}) {
		t.Errorf("OwnerRotation = %v", got)
	}
	if got := OwnerRotation(2); len(got) != 2 || got[0] != "" {
		t.Errorf("OwnerRotation without owners = %v, want two empty owners", got)
	}
}

func TestArgumentsCoverEveryChoiceAndTheExtras(t *testing.T) {
	cmd := &command.Command{
		Name: "remove",
		Options: []*discordgo.ApplicationCommandOption{
			{Name: "mode", Type: discordgo.ApplicationCommandOptionString, Choices: []*discordgo.ApplicationCommandOptionChoice{{Value: "on"}, {Value: "off"}}},
			{Name: "page", Type: discordgo.ApplicationCommandOptionInteger},
			{Name: "query", Type: discordgo.ApplicationCommandOptionString},
		},
	}

	got := Arguments(cmd)
	want := [][]string{nil, {"on", "1", "1"}, {"off", "1", "1"}, {"on", "2", "1"}, {"all"}, {"1-2"}}
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Errorf("Arguments = %v, want %v", got, want)
	}

	if got := Arguments(&command.Command{Name: "status"}); len(got) != 1 || got[0] != nil {
		t.Errorf("Arguments without options = %v, want only the empty set", got)
	}
}

func TestCommandsLeaveOutAudioCommands(t *testing.T) {
	command.RegisterCommand(&command.Command{Name: "play"})
	command.RegisterCommand(&command.Command{Name: "faultcheckprobe"})

	names := make([]string, 0)
	for _, cmd := range Commands() {
		names = append(names, cmd.Name)
	}
	if slices.Contains(names, "play") || !slices.Contains(names, "faultcheckprobe") {
		t.Errorf("Commands = %v, want the probe without play", names)
	}
	if !slices.IsSorted(names) {
		t.Errorf("Commands = %v, want them sorted by name", names)
	}
}

func TestTargetsFindComponentsAndModalFields(t *testing.T) {
	message := map[string]any{
		"type": float64(discordgo.InteractionResponseChannelMessageWithSource),
		"data": map[string]any{
			"components": []any{
				map[string]any{"components": []any{
					map[string]any{"custom_id": "pick:1", "options": []any{map[string]any{"value": "a"}, map[string]any{"value": "b"}}},
					map[string]any{"custom_id": "page:2"},
					map[string]any{"url": "https://example.invalid"},
				}},
			},
		},
	}
	targets := Targets(message)
	if len(targets) != 2 || targets[0].CustomID != "pick:1" || !slices.Equal(targets[0].Values, []string{"a", "b"}) || targets[1].CustomID != "page:2" {
		t.Errorf("message targets = %+v", targets)
	}

	modal := map[string]any{
		"type": float64(discordgo.InteractionResponseModal),
		"data": map[string]any{
			"custom_id": "form:1",
			"components": []any{
				map[string]any{"components": []any{map[string]any{"custom_id": "text"}}},
				map[string]any{"component": map[string]any{"custom_id": "choice", "options": []any{map[string]any{"value": "x"}}}},
			},
		},
	}
	modalTargets := Targets(modal)
	wantFields := []Field{{CustomID: "text", Value: "1"}, {CustomID: "choice", Value: "x", IsSelect: true}}
	if len(modalTargets) != 1 || !modalTargets[0].IsModal || modalTargets[0].CustomID != "form:1" || !slices.Equal(modalTargets[0].Fields, wantFields) {
		t.Errorf("modal targets = %+v", modalTargets)
	}
}

func TestReportPrintsSortedFailuresAndTotals(t *testing.T) {
	report := &Report{}
	report.Pass()
	report.Fail(Failure{Step: "b", Detail: "second"})
	report.Fail(Failure{Step: "a", Detail: "first"})

	if !report.HasFailures() {
		t.Fatal("the report did not count its failures")
	}

	var output strings.Builder
	if err := report.Print(&output); err != nil {
		t.Fatalf("Print returned %v", err)
	}
	text := output.String()
	if strings.Index(text, "first") > strings.Index(text, "second") || !strings.Contains(text, "1 checks passed, 2 failed") {
		t.Errorf("report output = %q", text)
	}
}

func TestGuardReportsErrorsPanicsAndTimeouts(t *testing.T) {
	failure := errors.New("failed")
	if outcome := Guard(time.Second, func() error { return failure }); !errors.Is(outcome.Err, failure) || outcome.Crash() != "" {
		t.Errorf("error outcome = %+v", outcome)
	}
	if outcome := Guard(time.Second, func() error { panic("boom") }); !strings.Contains(outcome.Crash(), "panicked: boom") {
		t.Errorf("panic outcome = %q", outcome.Crash())
	}
	if outcome := Guard(10*time.Millisecond, func() error { time.Sleep(time.Second); return nil }); !outcome.IsTimeout || outcome.Crash() == "" {
		t.Errorf("timeout outcome = %+v", outcome)
	}
}
