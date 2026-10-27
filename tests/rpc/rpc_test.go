package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("the default config file was not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("got mode %v, want 0600 so only the bot user can read it", info.Mode().Perm())
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

	for i := range cfg.Activities {
		activity := &cfg.Activities[i]
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

	rpcStrings := &messages.T().RPC
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
