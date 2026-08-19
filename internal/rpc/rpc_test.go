package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func resetRPCState(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		runningMu.Lock()
		stopChan = nil
		stopOnce = nil
		running = false
		runningMu.Unlock()
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
	runningMu.Lock()
	running = true
	stopChan = make(chan bool, 1)
	stopOnce = &sync.Once{}
	runningMu.Unlock()
}

func TestLoadConfigCreatesDefaultWhenMissing(t *testing.T) {
	configPath := isolatedConfigDir(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned %v, want nil", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("the default config file was not created: %v", err)
	}

	want := DefaultConfig()
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
	var written Config
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

	cfg, err := LoadConfig()
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

	if _, err := LoadConfig(); err == nil {
		t.Error("LoadConfig returned nil, want an error rather than a silent default")
	}
}

func TestDefaultConfigIsUsable(t *testing.T) {
	cfg := DefaultConfig()

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
		if _, ok := ActivityTypeMap[activity.Type]; !ok {
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

	if len(ActivityTypeMap) != len(want) {
		t.Errorf("got %d activity types, want %d", len(ActivityTypeMap), len(want))
	}
	for name, activityType := range want {
		if got, ok := ActivityTypeMap[name]; !ok || got != activityType {
			t.Errorf("ActivityTypeMap[%q] = (%v, %v), want (%v, true)", name, got, ok, activityType)
		}
	}
}

func TestResolveActivityNamePassesThroughPlainNames(t *testing.T) {
	for _, name := range []string{"Music", "", "activity_default_1", "lang"} {
		if got := resolveActivityName(name); got != name {
			t.Errorf("resolveActivityName(%q) = %q, want it unchanged", name, got)
		}
	}
}

func TestResolveActivityNameFallsThroughOnUnknownKey(t *testing.T) {
	if got := resolveActivityName("lang.nonexistent"); got != "lang.nonexistent" {
		t.Errorf("got %q, want the input returned unchanged", got)
	}
}

func TestResolveActivityNameMapsEachLocaleKey(t *testing.T) {
	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("failed to load the English locale: %v", err)
	}

	rpc := messages.T().RPC
	cases := map[string]string{
		"lang.activity_default_1": rpc.ActivityDefault1,
		"lang.activity_default_2": rpc.ActivityDefault2,
		"lang.activity_default_3": rpc.ActivityDefault3,
		"lang.activity_default_4": rpc.ActivityDefault4,
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

		if got := resolveActivityName(key); got != want {
			t.Errorf("resolveActivityName(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestStopIsIdempotent(t *testing.T) {
	resetRPCState(t)
	simulateRunningLoop()

	Stop()
	Stop()
	Stop()
}

func TestStopClosesTheChannelOnce(t *testing.T) {
	resetRPCState(t)
	simulateRunningLoop()

	runningMu.Lock()
	ch := stopChan
	runningMu.Unlock()

	Stop()

	select {
	case <-ch:
	default:
		t.Error("the stop channel was not closed")
	}
}

func TestStopWithoutStartIsNoop(t *testing.T) {
	resetRPCState(t)

	runningMu.Lock()
	stopChan = nil
	stopOnce = nil
	running = false
	runningMu.Unlock()

	Stop()
}

func TestUpdateRPCReturnsWhenDisabled(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{"RPC_ENABLED": false, "activities": [{"name": "x", "type": "Playing"}]}`))

	UpdateRPC(nil)

	runningMu.Lock()
	defer runningMu.Unlock()
	if running {
		t.Error("running was left true after an early return")
	}
}

func TestUpdateRPCReturnsWithoutActivities(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{"RPC_ENABLED": true, "activities": []}`))

	UpdateRPC(nil)

	runningMu.Lock()
	defer runningMu.Unlock()
	if running {
		t.Error("running was left true after an early return")
	}
}

func TestUpdateRPCReturnsOnBadConfig(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte("{not json"))

	UpdateRPC(nil)

	runningMu.Lock()
	defer runningMu.Unlock()
	if running {
		t.Error("running was left true after a config failure")
	}
}

func TestUpdateRPCRefusesConcurrentLoops(t *testing.T) {
	resetRPCState(t)
	configPath := isolatedConfigDir(t)
	writeConfig(t, configPath, []byte(`{"RPC_ENABLED": true, "activities": [{"name": "x", "type": "Playing"}]}`))

	simulateRunningLoop()

	runningMu.Lock()
	first := stopChan
	runningMu.Unlock()

	UpdateRPC(nil)

	runningMu.Lock()
	defer runningMu.Unlock()
	if stopChan != first {
		t.Error("a second UpdateRPC replaced the stop channel of the running loop")
	}
}
