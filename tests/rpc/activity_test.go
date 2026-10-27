package rpc_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/rpc"
)

type mockStatusUpdater struct {
	err           error
	activityNames []string
}

func (m *mockStatusUpdater) UpdateStatusComplex(data discordgo.UpdateStatusData) error {
	for _, activity := range data.Activities {
		m.activityNames = append(m.activityNames, activity.Name)
	}
	return m.err
}

func newTestUpdater(status rpc.HookStatusUpdater, activities ...rpc.Activity) *rpc.HookUpdater {
	return rpc.HookBuildUpdater(rpc.HookUpdaterFields{
		Session: status,
		Cfg:     &rpc.Config{Activities: activities},
	})
}

func TestUpdateStaysMarkedFailingWhileTheGatewayIsGone(t *testing.T) {
	status := &mockStatusUpdater{err: discordgo.ErrWSNotFound}
	activities := newTestUpdater(status, rpc.Activity{Name: "Testing", Type: "Playing"})

	activities.HookUpdateActivity()
	if !*activities.HookIsFailing() {
		t.Fatal("the first failed update did not mark the updater as failing")
	}

	activities.HookUpdateActivity()
	activities.HookUpdateActivity()

	if !*activities.HookIsFailing() {
		t.Error("a repeated failure cleared the failing state")
	}
	if len(status.activityNames) != 3 {
		t.Errorf("got %d status updates, want 3", len(status.activityNames))
	}
}

func TestUpdateClearsFailingStateOnRecovery(t *testing.T) {
	status := &mockStatusUpdater{err: discordgo.ErrWSNotFound}
	activities := newTestUpdater(status, rpc.Activity{Name: "Testing", Type: "Playing"})

	activities.HookUpdateActivity()
	if !*activities.HookIsFailing() {
		t.Fatal("the first failed update did not mark the updater as failing")
	}

	status.err = nil
	activities.HookUpdateActivity()

	if *activities.HookIsFailing() {
		t.Error("a successful update did not clear the failing state")
	}
}

func TestUpdateRotatesActivitiesInOrder(t *testing.T) {
	status := &mockStatusUpdater{}
	activities := newTestUpdater(status,
		rpc.Activity{Name: "First", Type: "Playing"},
		rpc.Activity{Name: "Second", Type: "Listening"},
		rpc.Activity{Name: "Third", Type: "Watching"},
	)

	for range 4 {
		activities.HookUpdateActivity()
	}

	want := []string{"First", "Second", "Third", "First"}
	if len(status.activityNames) != len(want) {
		t.Fatalf("got %d status updates, want %d", len(status.activityNames), len(want))
	}
	for index, name := range want {
		if status.activityNames[index] != name {
			t.Errorf("update %d sent %q, want %q", index, status.activityNames[index], name)
		}
	}
}

func TestUpdateSkipsAnInvalidActivityType(t *testing.T) {
	status := &mockStatusUpdater{}
	activities := newTestUpdater(status, rpc.Activity{Name: "Testing", Type: "Dancing"})

	activities.HookUpdateActivity()

	if len(status.activityNames) != 0 {
		t.Errorf("got %d status updates, want none for an invalid activity type", len(status.activityNames))
	}
}

func TestLoadIdentifyPresenceBuildsAResolvedActivity(t *testing.T) {
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": true,
		"RPC_INTERVAL_SECONDS": 30,
		"RANDOMIZE_RPC": false,
		"activities": [{"name": "Testing", "type": "Watching"}]
	}`))

	presence, ok := rpc.LoadIdentifyPresence()
	if !ok {
		t.Fatal("LoadIdentifyPresence reported no presence, so identify would carry an empty one")
	}
	if presence.Game.Name != "Testing" {
		t.Errorf("got activity name %q, want %q", presence.Game.Name, "Testing")
	}
	if presence.Game.Type != discordgo.ActivityTypeWatching {
		t.Errorf("got activity type %d, want %d", presence.Game.Type, discordgo.ActivityTypeWatching)
	}
	if presence.Status != "online" {
		t.Errorf("got status %q, want %q", presence.Status, "online")
	}
}

func TestLoadIdentifyPresenceResolvesLocaleKeys(t *testing.T) {
	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("failed to load the English locale: %v", err)
	}

	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": true,
		"RANDOMIZE_RPC": false,
		"activities": [{"name": "lang.activity_default_1", "type": "Playing"}]
	}`))

	presence, ok := rpc.LoadIdentifyPresence()
	if !ok {
		t.Fatal("LoadIdentifyPresence reported no presence")
	}
	if presence.Game.Name == "lang.activity_default_1" {
		t.Error("the locale key was sent unresolved, so the bot would show a raw key as its activity")
	}
	if presence.Game.Name != messages.T().RPC.ActivityDefault1 {
		t.Errorf("got %q, want the localised name %q", presence.Game.Name, messages.T().RPC.ActivityDefault1)
	}
}

func TestLoadIdentifyPresenceReportsNothingWhenDisabled(t *testing.T) {
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": false,
		"activities": [{"name": "Testing", "type": "Watching"}]
	}`))

	if _, ok := rpc.LoadIdentifyPresence(); ok {
		t.Error("LoadIdentifyPresence returned a presence while RPC is disabled")
	}
}

func TestLoadIdentifyPresenceReportsNothingForAnInvalidType(t *testing.T) {
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": true,
		"RANDOMIZE_RPC": false,
		"activities": [{"name": "Testing", "type": "Dancing"}]
	}`))

	if _, ok := rpc.LoadIdentifyPresence(); ok {
		t.Error("LoadIdentifyPresence returned a presence for an unmapped activity type")
	}
}

func TestUpdateActivityKeepsThePositionAfterAFailedSend(t *testing.T) {
	status := &mockStatusUpdater{err: errors.New("send failed")}
	activities := newTestUpdater(status,
		rpc.Activity{Name: "First", Type: "Playing"},
		rpc.Activity{Name: "Second", Type: "Listening"},
		rpc.Activity{Name: "Third", Type: "Watching"},
	)

	activities.HookUpdateActivity()
	status.err = nil
	activities.HookUpdateActivity()

	want := []string{"First", "First"}
	if len(status.activityNames) != len(want) {
		t.Fatalf("got %d status updates, want %d", len(status.activityNames), len(want))
	}
	for index, name := range want {
		if status.activityNames[index] != name {
			t.Errorf("update %d sent %q, want %q: a failed send must not consume its activity", index, status.activityNames[index], name)
		}
	}
}

func TestUpdateActivityAdvancesPastAnInvalidType(t *testing.T) {
	status := &mockStatusUpdater{}
	activities := newTestUpdater(status,
		rpc.Activity{Name: "First", Type: "Playing"},
		rpc.Activity{Name: "Second", Type: "Dancing"},
		rpc.Activity{Name: "Third", Type: "Watching"},
	)

	activities.HookUpdateActivity()
	activities.HookUpdateActivity()
	activities.HookUpdateActivity()

	want := []string{"First", "Third"}
	if len(status.activityNames) != len(want) {
		t.Fatalf("got %d status updates, want %d", len(status.activityNames), len(want))
	}
	for index, name := range want {
		if status.activityNames[index] != name {
			t.Errorf("update %d sent %q, want %q: a misconfigured activity must not jam the rotation", index, status.activityNames[index], name)
		}
	}
}

func TestUpdateActivityKeepsTheIndexParkedWhenRandomized(t *testing.T) {
	status := &mockStatusUpdater{}
	activities := rpc.HookBuildUpdater(rpc.HookUpdaterFields{
		Session: status,
		Cfg: &rpc.Config{
			RandomizeRPC: true,
			Activities:   []rpc.Activity{{Name: "Testing", Type: "Playing"}},
		},
	})

	activities.HookUpdateActivity()
	activities.HookUpdateActivity()

	if *activities.HookCurrentIndex() != 0 {
		t.Errorf("currentIndex = %d, want 0: a randomized rotation must not advance it", *activities.HookCurrentIndex())
	}
}

func TestUpdateActivityStaysQuietWhileReconnecting(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "test.log")
	logger.SetLogFile(logPath)
	defer logger.SetLogFile("")

	status := &mockStatusUpdater{err: discordgo.ErrWSNotFound}
	activities := newTestUpdater(status, rpc.Activity{Name: "Testing", Type: "Playing"})

	activities.HookUpdateActivity()

	written, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read the log file: %v", err)
	}

	if strings.Contains(string(written), "Failed to update RPC") {
		t.Errorf("a reconnect window was logged as a warning:\n%s", written)
	}
}

func TestUpdateActivityWarnsOnARealFailure(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "test.log")
	logger.SetLogFile(logPath)
	defer logger.SetLogFile("")

	status := &mockStatusUpdater{err: errors.New("rate limited")}
	activities := newTestUpdater(status, rpc.Activity{Name: "Testing", Type: "Playing"})

	activities.HookUpdateActivity()

	written, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read the log file: %v", err)
	}

	if !strings.Contains(string(written), "Failed to update RPC") {
		t.Errorf("a real failure was not logged as a warning:\n%s", written)
	}
}
