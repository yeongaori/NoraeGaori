package rpc_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/rpc"
)

func resetRPCState(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		rpc.HookRunningMu.Lock()
		*rpc.HookStopChan = nil
		*rpc.HookStopOnce = nil
		*rpc.HookRunning = false
		rpc.HookRunningMu.Unlock()
	})
}

func isolatedConfigDir(t *testing.T) string {
	t.Helper()

	t.Chdir(t.TempDir())
	return filepath.Join("config", "rpcConfig.json")
}

func writeConfig(t *testing.T, path string, body []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create the config directory: %v", err)
	}
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatalf("failed to write the config file: %v", err)
	}
}

func simulateRunningLoop() {
	rpc.HookRunningMu.Lock()
	*rpc.HookRunning = true
	*rpc.HookStopChan = make(chan bool, 1)
	*rpc.HookStopOnce = &sync.Once{}
	rpc.HookRunningMu.Unlock()
}

func TestLoadConfigCreatesDefaultWhenMissing(t *testing.T) {
	configPath := isolatedConfigDir(t)

	cfg, err := rpc.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned %v, want nil", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("the default config file was not created: %v", err)
	}

	want := rpc.DefaultConfig()
	if cfg.RPCEnabled != want.RPCEnabled || cfg.RPCIntervalSeconds != want.RPCIntervalSeconds {
		t.Errorf("got %+v, want the defaults %+v", cfg, want)
	}
	if len(cfg.Activities) != len(want.Activities) {
		t.Errorf("got %d activities, want %d", len(cfg.Activities), len(want.Activities))
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read the created config: %v", err)
	}
	var written rpc.Config
	if err := json.Unmarshal(data, &written); err != nil {
		t.Errorf("the created config is not valid JSON: %v", err)
	}
}

func TestLoadConfigReadsExistingFile(t *testing.T) {
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": false,
		"RPC_INTERVAL_SECONDS": 90,
		"LOG_RPC_CHANGES": true,
		"RANDOMIZE_RPC": false,
		"activities": [{"name": "Testing", "type": "Watching"}]
	}`))

	cfg, err := rpc.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned %v, want nil", err)
	}

	if cfg.RPCEnabled {
		t.Error("got RPCEnabled true, want the value from the file")
	}
	if cfg.RPCIntervalSeconds != 90 {
		t.Errorf("got interval %d, want 90", cfg.RPCIntervalSeconds)
	}
	if !cfg.LogRPCChanges {
		t.Error("got LogRPCChanges false, want true")
	}
	if len(cfg.Activities) != 1 || cfg.Activities[0].Name != "Testing" {
		t.Errorf("got activities %+v, want the single activity from the file", cfg.Activities)
	}
}

func TestLoadConfigRejectsMalformedJSON(t *testing.T) {
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte("{not json"))

	if _, err := rpc.LoadConfig(); err == nil {
		t.Error("LoadConfig returned nil, want an error rather than a silent default")
	}
}

func TestDefaultConfigIsUsable(t *testing.T) {
	cfg := rpc.DefaultConfig()

	if !cfg.RPCEnabled {
		t.Error("the default config disables RPC")
	}
	if cfg.RPCIntervalSeconds <= 0 {
		t.Errorf("got interval %d, want a positive value", cfg.RPCIntervalSeconds)
	}
	if len(cfg.Activities) == 0 {
		t.Fatal("the default config has no activities")
	}

	for _, activity := range cfg.Activities {
		if _, ok := rpc.ActivityTypeMap[activity.Type]; !ok {
			t.Errorf("default activity %q has type %q, which is not in ActivityTypeMap", activity.Name, activity.Type)
		}
	}
}

func TestActivityTypeMapCoversDiscordTypes(t *testing.T) {
	want := map[string]discordgo.ActivityType{
		"Playing":   discordgo.ActivityTypeGame,
		"Streaming": discordgo.ActivityTypeStreaming,
		"Listening": discordgo.ActivityTypeListening,
		"Watching":  discordgo.ActivityTypeWatching,
		"Custom":    discordgo.ActivityTypeCustom,
		"Competing": discordgo.ActivityTypeCompeting,
	}

	if len(rpc.ActivityTypeMap) != len(want) {
		t.Errorf("got %d activity types, want %d", len(rpc.ActivityTypeMap), len(want))
	}
	for name, activityType := range want {
		if got, ok := rpc.ActivityTypeMap[name]; !ok || got != activityType {
			t.Errorf("ActivityTypeMap[%q] = (%v, %v), want (%v, true)", name, got, ok, activityType)
		}
	}
}

func TestResolveActivityNamePassesThroughPlainNames(t *testing.T) {
	for _, name := range []string{"Music", "", "activity_default_1", "lang"} {
		if got := rpc.HookResolveActivityName(name); got != name {
			t.Errorf("resolveActivityName(%q) = %q, want it unchanged", name, got)
		}
	}
}

func TestResolveActivityNameFallsThroughOnUnknownKey(t *testing.T) {
	if got := rpc.HookResolveActivityName("lang.nonexistent"); got != "lang.nonexistent" {
		t.Errorf("got %q, want the input returned unchanged", got)
	}
}

func TestResolveActivityNameMapsEachLocaleKey(t *testing.T) {
	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("failed to load the English locale: %v", err)
	}

	rpcStrings := messages.T().RPC
	cases := map[string]string{
		"lang.activity_default_1": rpcStrings.ActivityDefault1,
		"lang.activity_default_2": rpcStrings.ActivityDefault2,
		"lang.activity_default_3": rpcStrings.ActivityDefault3,
		"lang.activity_default_4": rpcStrings.ActivityDefault4,
	}

	seen := make(map[string]bool)
	for key, want := range cases {
		if want == "" {
			t.Fatalf("the locale has no value for %s, so this test cannot detect a mapping swap", key)
		}
		if seen[want] {
			t.Fatalf("locale value %q is used twice, so this test cannot detect a mapping swap", want)
		}
		seen[want] = true

		if got := rpc.HookResolveActivityName(key); got != want {
			t.Errorf("resolveActivityName(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestStopIsIdempotent(t *testing.T) {
	resetRPCState(t)
	simulateRunningLoop()

	rpc.Stop()
	rpc.Stop()
	rpc.Stop()
}

func TestStopClosesTheChannelOnce(t *testing.T) {
	resetRPCState(t)
	simulateRunningLoop()

	rpc.HookRunningMu.Lock()
	ch := *rpc.HookStopChan
	rpc.HookRunningMu.Unlock()

	rpc.Stop()

	select {
	case <-ch:
	default:
		t.Error("the stop channel was not closed")
	}
}

func TestStopWithoutStartIsNoop(t *testing.T) {
	resetRPCState(t)

	rpc.HookRunningMu.Lock()
	*rpc.HookStopChan = nil
	*rpc.HookStopOnce = nil
	*rpc.HookRunning = false
	rpc.HookRunningMu.Unlock()

	rpc.Stop()
}

func TestUpdateRPCReturnsWhenDisabled(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{"RPC_ENABLED": false, "activities": [{"name": "x", "type": "Playing"}]}`))

	rpc.UpdateRPC(nil)

	rpc.HookRunningMu.Lock()
	defer rpc.HookRunningMu.Unlock()
	if *rpc.HookRunning {
		t.Error("running was left true after an early return")
	}
}

func TestUpdateRPCReturnsWithoutActivities(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{"RPC_ENABLED": true, "activities": []}`))

	rpc.UpdateRPC(nil)

	rpc.HookRunningMu.Lock()
	defer rpc.HookRunningMu.Unlock()
	if *rpc.HookRunning {
		t.Error("running was left true after an early return")
	}
}

func TestUpdateRPCReturnsOnBadConfig(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte("{not json"))

	rpc.UpdateRPC(nil)

	rpc.HookRunningMu.Lock()
	defer rpc.HookRunningMu.Unlock()
	if *rpc.HookRunning {
		t.Error("running was left true after a config failure")
	}
}

func TestUpdateRPCRefusesConcurrentLoops(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{"RPC_ENABLED": true, "activities": [{"name": "x", "type": "Playing"}]}`))

	simulateRunningLoop()

	rpc.HookRunningMu.Lock()
	first := *rpc.HookStopChan
	rpc.HookRunningMu.Unlock()

	rpc.UpdateRPC(nil)

	rpc.HookRunningMu.Lock()
	defer rpc.HookRunningMu.Unlock()
	if *rpc.HookStopChan != first {
		t.Error("a second UpdateRPC replaced the stop channel of the running loop")
	}
}

func TestLoadConfigReplacesANonPositiveInterval(t *testing.T) {
	for name, interval := range map[string]string{"zero": "0", "negative": "-30"} {
		t.Run(name, func(t *testing.T) {
			configPath := isolatedConfigDir(t)
			writeConfig(t, configPath, []byte(`{
				"RPC_ENABLED": true,
				"RPC_INTERVAL_SECONDS": `+interval+`,
				"activities": [{"name": "Testing", "type": "Playing"}]
			}`))

			cfg, err := rpc.LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig returned an error: %v", err)
			}

			if cfg.RPCIntervalSeconds != rpc.DefaultConfig().RPCIntervalSeconds {
				t.Errorf("got interval %d, want the default %d", cfg.RPCIntervalSeconds, rpc.DefaultConfig().RPCIntervalSeconds)
			}
		})
	}
}

func TestLoadConfigKeepsAPositiveInterval(t *testing.T) {
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": true,
		"RPC_INTERVAL_SECONDS": 45,
		"activities": [{"name": "Testing", "type": "Playing"}]
	}`))

	cfg, err := rpc.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}

	if cfg.RPCIntervalSeconds != 45 {
		t.Errorf("got interval %d, want 45", cfg.RPCIntervalSeconds)
	}
}

func TestUpdateRPCSurvivesAConfigWithoutAnInterval(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{
		"RPC_ENABLED": true,
		"activities": [{"name": "Testing", "type": "Playing"}]
	}`))

	cfg, err := rpc.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}

	ticker := time.NewTicker(time.Duration(cfg.RPCIntervalSeconds) * time.Second)
	ticker.Stop()
}

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
