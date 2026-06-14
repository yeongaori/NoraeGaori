package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"noraegaori/pkg/logger"
)

type VersionState string

const (
	StatePending     VersionState = "pending"
	StateVerified    VersionState = "verified"
	StateActive      VersionState = "active"
	StateProvisional VersionState = "provisional"
	StateBlacklisted VersionState = "blacklisted"

	rollbackThreshold   = 3
	rollbackWindow      = 30 * time.Minute
	stableSuccessCount  = 10
	stalePendingTimeout = 48 * time.Hour
	canaryRingSize      = 10
	canaryTestCount     = 3
	versionDataFile     = "data/ytdlp_versions.json"
)

var fixedCanaryIDs = []string{
	"jNQXAC9IVRw",
	"BaW_jenozKc",
}

type ErrorRecord struct {
	VideoID string    `json:"video_id"`
	Time    time.Time `json:"time"`
}

type VersionEntry struct {
	Path          string        `json:"path"`
	State         VersionState  `json:"state"`
	Successes     int           `json:"successes"`
	LastSuccess   time.Time     `json:"last_success,omitempty"`
	Errors        []ErrorRecord `json:"errors,omitempty"`
	BlacklistedAt time.Time     `json:"blacklisted_at,omitempty"`
	RegisteredAt  time.Time     `json:"registered_at"`
}

type persistedState struct {
	ActiveVersion   string                   `json:"active_version"`
	LastGitHubCheck time.Time                `json:"last_github_check"`
	Versions        map[string]*VersionEntry `json:"versions"`
	CanaryRing      []string                 `json:"canary_ring"`
}

type VersionManager struct {
	state persistedState
	mu    sync.RWMutex
}

var versionMgr *VersionManager

func InitVersionManager() error {
	versionmanager := &VersionManager{
		state: persistedState{
			Versions:   make(map[string]*VersionEntry),
			CanaryRing: []string{},
		},
	}

	if err := versionmanager.load(); err != nil {
		logger.Debugf("[yt-dlp] No existing state, starting fresh: %v", err)
	}

	versionMgr = versionmanager
	return nil
}

func GetVersionManager() *VersionManager {
	return versionMgr
}

func (versionmanager *VersionManager) load() error {
	data, err := os.ReadFile(versionDataFile)
	if err != nil {
		return err
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	if state.Versions == nil {
		state.Versions = make(map[string]*VersionEntry)
	}
	if state.CanaryRing == nil {
		state.CanaryRing = []string{}
	}

	versionmanager.state = state
	logger.Debugf("[yt-dlp] Loaded state: active=%s, %d versions tracked", state.ActiveVersion, len(state.Versions))
	return nil
}

func (versionmanager *VersionManager) persist() {
	if err := os.MkdirAll(filepath.Dir(versionDataFile), 0755); err != nil {
		logger.Errorf("[yt-dlp] Failed to create data dir: %v", err)
		return
	}

	data, err := json.MarshalIndent(versionmanager.state, "", "  ")
	if err != nil {
		logger.Errorf("[yt-dlp] Failed to marshal state: %v", err)
		return
	}

	tmpFile := versionDataFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		logger.Errorf("[yt-dlp] Failed to write temp state: %v", err)
		return
	}
	if err := os.Rename(tmpFile, versionDataFile); err != nil {
		logger.Errorf("[yt-dlp] Failed to rename state file: %v", err)
	}
}

func (versionmanager *VersionManager) RegisterVersion(version, path string) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	if _, exists := versionmanager.state.Versions[version]; exists {
		logger.Warnf("[yt-dlp] Version %s already registered, skipping", version)
		return
	}

	versionmanager.state.Versions[version] = &VersionEntry{
		Path:         path,
		State:        StatePending,
		RegisteredAt: time.Now(),
	}
	versionmanager.persist()
	logger.Debugf("[yt-dlp] Registered version %s at %s", version, path)
}

func (versionmanager *VersionManager) SetVersionState(version string, state VersionState) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		return
	}

	entry.State = state
	if state == StateBlacklisted {
		entry.BlacklistedAt = time.Now()
	}
	versionmanager.persist()
	logger.Debugf("[yt-dlp] Version %s -> %s", version, state)
}

func (versionmanager *VersionManager) GetActiveVersion() string {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()
	return versionmanager.state.ActiveVersion
}

func (versionmanager *VersionManager) SetActiveVersion(version string) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	if old, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok && versionmanager.state.ActiveVersion != version {
		if old.State == StateActive {
			old.State = StateVerified
		}
	}

	if entry, ok := versionmanager.state.Versions[version]; ok {
		entry.State = StateActive
	}
	versionmanager.state.ActiveVersion = version
	versionmanager.persist()
}

func (versionmanager *VersionManager) GetVersionState(version string) (VersionState, bool) {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()
	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		return "", false
	}
	return entry.State, true
}

func (versionmanager *VersionManager) HasUsableBinary() bool {
	versionmanager.mu.RLock()
	candidates := make([]string, 0, len(versionmanager.state.Versions))
	for _, entry := range versionmanager.state.Versions {
		switch entry.State {
		case StateVerified, StateActive, StateProvisional:
			if _, err := os.Stat(entry.Path); err == nil {
				candidates = append(candidates, entry.Path)
			}
		}
	}
	versionmanager.mu.RUnlock()

	for _, path := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := exec.CommandContext(ctx, path, "--version").Run()
		cancel()
		if err == nil {
			return true
		}
		logger.Warnf("[yt-dlp] Binary at %s failed --version check: %v", path, err)
	}
	return false
}

func (versionmanager *VersionManager) ProvisionallyActivate(version, path string) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		entry = &VersionEntry{
			Path:         path,
			RegisteredAt: time.Now(),
		}
		versionmanager.state.Versions[version] = entry
	} else if entry.Path == "" {
		entry.Path = path
	}

	entry.State = StateProvisional
	entry.BlacklistedAt = time.Time{}

	versionmanager.state.ActiveVersion = version
	versionmanager.persist()
	logger.Warnf("[yt-dlp] Provisionally activated %s — canary did not pass; trusting based on real traffic", version)
}

func (versionmanager *VersionManager) GetLastGitHubCheck() time.Time {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()
	return versionmanager.state.LastGitHubCheck
}

func (versionmanager *VersionManager) SetLastGitHubCheck(t time.Time) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()
	versionmanager.state.LastGitHubCheck = t
	versionmanager.persist()
}

func (versionmanager *VersionManager) addToCanaryRing(videoID string) {

	for _, id := range versionmanager.state.CanaryRing {
		if id == videoID {
			return
		}
	}
	versionmanager.state.CanaryRing = append(versionmanager.state.CanaryRing, videoID)
	if len(versionmanager.state.CanaryRing) > canaryRingSize {
		versionmanager.state.CanaryRing = versionmanager.state.CanaryRing[len(versionmanager.state.CanaryRing)-canaryRingSize:]
	}
}

func (versionmanager *VersionManager) getCanaryIDs() []string {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()

	ids := make([]string, 0, len(fixedCanaryIDs)+canaryTestCount)
	ids = append(ids, fixedCanaryIDs...)

	if len(versionmanager.state.CanaryRing) == 0 {
		return ids
	}

	ring := make([]string, len(versionmanager.state.CanaryRing))
	copy(ring, versionmanager.state.CanaryRing)
	rand.Shuffle(len(ring), func(i, j int) { ring[i], ring[j] = ring[j], ring[i] })

	count := canaryTestCount
	if count > len(ring) {
		count = len(ring)
	}
	ids = append(ids, ring[:count]...)
	return ids
}

func (versionmanager *VersionManager) SaveSuccess(version, videoID string) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		return
	}

	entry.Successes++
	entry.LastSuccess = time.Now()

	if videoID != "" {
		versionmanager.addToCanaryRing(videoID)
	}

	if entry.State == StateProvisional && version == versionmanager.state.ActiveVersion && entry.Successes >= stableSuccessCount {
		entry.State = StateActive
		logger.Infof("[yt-dlp] Provisional version %s promoted to Active after %d real successes", version, entry.Successes)
	}

	if version == versionmanager.state.ActiveVersion && entry.Successes == stableSuccessCount {
		logger.Infof("[yt-dlp] Active version %s reached %d successes, running cleanup", version, stableSuccessCount)
		versionmanager.cleanupOldVersions()
	}

	versionmanager.persist()
}

func (versionmanager *VersionManager) SaveError(version, videoID string, errMsg string) {

	if IsDefinitiveUnavailableError(errMsg) || IsNetworkError(errMsg) {
		return
	}

	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		return
	}

	now := time.Now()
	cutoff := now.Add(-rollbackWindow)

	fresh := make([]ErrorRecord, 0, len(entry.Errors))
	for _, e := range entry.Errors {
		if e.Time.After(cutoff) {
			fresh = append(fresh, e)
		}
	}
	entry.Errors = fresh

	if videoID != "" {
		for _, e := range entry.Errors {
			if e.VideoID == videoID {
				return
			}
		}
	}

	entry.Errors = append(entry.Errors, ErrorRecord{
		VideoID: videoID,
		Time:    now,
	})

	logger.Warnf("[yt-dlp] Saved error for version %s (video: %s), %d errors in window", version, videoID, len(entry.Errors))
	versionmanager.persist()
}

func (versionmanager *VersionManager) shouldRollback() bool {
	entry, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]
	if !ok {
		return false
	}

	cutoff := time.Now().Add(-rollbackWindow)
	recentErrors := 0
	for _, e := range entry.Errors {
		if e.Time.After(cutoff) {
			recentErrors++
		}
	}

	return recentErrors >= rollbackThreshold
}

func (versionmanager *VersionManager) selectBestVersion() string {
	var candidates []string
	for ver, entry := range versionmanager.state.Versions {
		if entry.State != StateBlacklisted && entry.Successes > 0 && ver != versionmanager.state.ActiveVersion {
			candidates = append(candidates, ver)
		}
	}

	if len(candidates) == 0 {
		return versionmanager.state.ActiveVersion
	}

	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	return candidates[0]
}

func (versionmanager *VersionManager) ActiveBinaryPath() string {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	versionmanager.tryPromoteVerified()

	if versionmanager.shouldRollback() {
		best := versionmanager.selectBestVersion()
		if best != versionmanager.state.ActiveVersion {
			logger.Warnf("[yt-dlp] Rolling back from %s to %s", versionmanager.state.ActiveVersion, best)

			if entry, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
				entry.State = StateBlacklisted
				entry.BlacklistedAt = time.Now()
			}

			if entry, ok := versionmanager.state.Versions[best]; ok {
				entry.State = StateActive
			}
			versionmanager.state.ActiveVersion = best
			versionmanager.persist()
		}
	}

	if entry, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
		if _, err := os.Stat(entry.Path); err == nil {
			return entry.Path
		}
		logger.Errorf("[yt-dlp] Active binary %s missing on disk; falling back to legacy path", entry.Path)
	}

	return GetLegacyBinaryPath()
}

func (versionmanager *VersionManager) tryPromoteVerified() {
	var bestVerified string
	for ver, entry := range versionmanager.state.Versions {
		if entry.State == StateVerified {
			if bestVerified == "" || ver > bestVerified {
				bestVerified = ver
			}
		}
	}

	if bestVerified == "" || bestVerified <= versionmanager.state.ActiveVersion {
		return
	}

	logger.Infof("[yt-dlp] Promoting verified version %s to active (was %s)", bestVerified, versionmanager.state.ActiveVersion)

	if old, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
		if old.State == StateActive {
			old.State = StateVerified
		}
	}

	versionmanager.state.Versions[bestVerified].State = StateActive
	versionmanager.state.ActiveVersion = bestVerified
	versionmanager.persist()
}

func (versionmanager *VersionManager) cleanupOldVersions() {
	active := versionmanager.state.ActiveVersion
	fallback := versionmanager.selectBestVersion()

	var toDelete []string
	for ver, entry := range versionmanager.state.Versions {

		if ver == active || ver == fallback {
			continue
		}

		shouldDelete := false
		reason := ""

		switch entry.State {
		case StateBlacklisted:
			shouldDelete = true
			reason = "blacklisted"
		case StateVerified:

			if ver < active {
				shouldDelete = true
				reason = "superseded verified"
			}
		case StatePending:

			if time.Since(entry.RegisteredAt) > stalePendingTimeout {
				shouldDelete = true
				reason = "stale pending"
			}
		default:

			if entry.Successes > 0 && ver < fallback {
				shouldDelete = true
				reason = "old working below fallback"
			}
		}

		if shouldDelete {
			toDelete = append(toDelete, ver)
			logger.Debugf("[yt-dlp] Marking %s for cleanup: %s", ver, reason)
		}
	}

	for _, ver := range toDelete {
		entry := versionmanager.state.Versions[ver]

		dir := filepath.Dir(entry.Path)
		if err := os.RemoveAll(dir); err != nil {
			logger.Warnf("[yt-dlp] Failed to remove directory %s: %v", dir, err)
		} else {
			logger.Debugf("[yt-dlp] Removed version directory: %s", dir)
		}

		delete(versionmanager.state.Versions, ver)
	}

	if len(toDelete) > 0 {
		versionmanager.persist()
		logger.Infof("[yt-dlp] Cleanup complete: removed %d version(s), %d remaining", len(toDelete), len(versionmanager.state.Versions))
	}
}

type canaryResult struct {
	videoID      string
	success      bool
	network      bool
	inconclusive bool
	errMsg       string
}

func (versionmanager *VersionManager) RunCanary(version string) (passed bool, networkError bool) {
	versionmanager.mu.RLock()
	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		versionmanager.mu.RUnlock()
		return false, false
	}
	binaryPath := entry.Path
	versionmanager.mu.RUnlock()

	ids := versionmanager.getCanaryIDs()
	logger.Debugf("[yt-dlp] Running canary for %s with %d video(s)", version, len(ids))

	var (
		networkCount      int
		inconclusiveCount int
	)

	for _, id := range ids {
		result := versionmanager.testExtraction(binaryPath, id)
		switch {
		case result.success:
			logger.Infof("[yt-dlp] Canary PASSED for %s", version)
			return true, false
		case result.network:
			logger.Debugf("[yt-dlp] Canary network error for %s: %s", version, result.errMsg)
			networkCount++
		case result.inconclusive:
			logger.Debugf("[yt-dlp] Canary inconclusive for %s (not a binary problem): %s", version, result.errMsg)
			inconclusiveCount++
		default:
			logger.Warnf("[yt-dlp] Canary FAILED for %s: %s", version, result.errMsg)
			return false, false
		}
	}

	if networkCount > 0 && inconclusiveCount == 0 {

		logger.Warnf("[yt-dlp] All canary tests for %s hit network errors; version stays pending", version)
		return false, true
	}

	logger.Warnf("[yt-dlp] Canary inconclusive for %s — no testable videos but no evidence of binary breakage", version)
	return true, false
}

func (versionmanager *VersionManager) testExtraction(binaryPath, videoID string) canaryResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	args := []string{"--dump-json", "--no-playlist", "--no-warnings", url}

	if rt := GetJsRuntime(); rt != "" {
		args = append([]string{"--js-runtimes", rt}, args...)
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	output, err := cmd.Output()

	if err != nil {
		errMsg := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			errMsg = string(exitErr.Stderr)
		}

		if IsNetworkError(errMsg) || ctx.Err() != nil {
			return canaryResult{videoID: videoID, network: true, errMsg: errMsg}
		}
		if IsDefinitiveUnavailableError(errMsg) {
			return canaryResult{videoID: videoID, inconclusive: true, errMsg: errMsg}
		}
		return canaryResult{videoID: videoID, errMsg: errMsg}
	}

	var info struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Formats []struct {
			URL string `json:"url"`
		} `json:"formats"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return canaryResult{videoID: videoID, errMsg: "invalid JSON output"}
	}
	if info.ID == videoID || len(info.Formats) > 0 || info.URL != "" {
		return canaryResult{videoID: videoID, success: true}
	}
	return canaryResult{videoID: videoID, errMsg: "extractor returned no id, formats, or url"}
}
