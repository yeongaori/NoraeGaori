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

// VersionState represents the lifecycle state of a yt-dlp version
type VersionState string

const (
	StatePending     VersionState = "pending"
	StateVerified    VersionState = "verified"
	StateActive      VersionState = "active"
	StateBlacklisted VersionState = "blacklisted"

	rollbackThreshold   = 3
	rollbackWindow      = 30 * time.Minute
	stableSuccessCount  = 10
	stalePendingTimeout = 48 * time.Hour
	canaryRingSize      = 10
	canaryTestCount     = 3
	fixedCanaryID       = "dQw4w9WgXcQ"
	versionDataFile     = "data/ytdlp_versions.json"
)

// ErrorRecord tracks a non-definitive extraction error for a specific video
type ErrorRecord struct {
	VideoID string    `json:"video_id"`
	Time    time.Time `json:"time"`
}

// VersionEntry represents a single yt-dlp version on disk
type VersionEntry struct {
	Path          string        `json:"path"`
	State         VersionState  `json:"state"`
	Successes     int           `json:"successes"`
	LastSuccess   time.Time     `json:"last_success,omitempty"`
	Errors        []ErrorRecord `json:"errors,omitempty"`
	BlacklistedAt time.Time     `json:"blacklisted_at,omitempty"`
	RegisteredAt  time.Time     `json:"registered_at"`
}

// persistedState is the JSON structure saved to disk
type persistedState struct {
	ActiveVersion   string                   `json:"active_version"`
	LastGitHubCheck time.Time                `json:"last_github_check"`
	Versions        map[string]*VersionEntry `json:"versions"`
	CanaryRing      []string                 `json:"canary_ring"`
}

// VersionManager manages multiple yt-dlp versions with health tracking
type VersionManager struct {
	state persistedState
	mu    sync.RWMutex
}

// package-level singleton, nil until InitVersionManager is called
var versionMgr *VersionManager

// InitVersionManager creates and loads the global VersionManager.
// Call once from main() before starting the bot.
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

// GetVersionManager returns the global VersionManager singleton.
// Returns nil if InitVersionManager has not been called.
func GetVersionManager() *VersionManager {
	return versionMgr
}

// load reads persisted state from disk
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
	logger.Infof("[yt-dlp] Loaded state: active=%s, %d versions tracked", state.ActiveVersion, len(state.Versions))
	return nil
}

// persist writes current state to disk. Must be called with lock held.
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

// RegisterVersion adds a new version entry with state=pending
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
	logger.Infof("[yt-dlp] Registered version %s at %s", version, path)
}

// SetVersionState updates the state of a version
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
	logger.Infof("[yt-dlp] Version %s -> %s", version, state)
}

// GetActiveVersion returns the currently active version string
func (versionmanager *VersionManager) GetActiveVersion() string {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()
	return versionmanager.state.ActiveVersion
}

// SetActiveVersion sets the active version directly (used for migration)
func (versionmanager *VersionManager) SetActiveVersion(version string) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	// Demote old active version to verified
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

// GetVersionState returns the state of a version and whether it exists
func (versionmanager *VersionManager) GetVersionState(version string) (VersionState, bool) {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()
	entry, ok := versionmanager.state.Versions[version]
	if !ok {
		return "", false
	}
	return entry.State, true
}

// GetLastGitHubCheck returns when GitHub was last checked
func (versionmanager *VersionManager) GetLastGitHubCheck() time.Time {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()
	return versionmanager.state.LastGitHubCheck
}

// SetLastGitHubCheck updates the GitHub check timestamp
func (versionmanager *VersionManager) SetLastGitHubCheck(t time.Time) {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()
	versionmanager.state.LastGitHubCheck = t
	versionmanager.persist()
}

// addToCanaryRing adds a video ID to the ring buffer (must hold lock)
func (versionmanager *VersionManager) addToCanaryRing(videoID string) {
	// Avoid duplicates
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

// getCanaryIDs returns the fixed canary + up to canaryTestCount random IDs from the ring
func (versionmanager *VersionManager) getCanaryIDs() []string {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()

	ids := []string{fixedCanaryID}

	if len(versionmanager.state.CanaryRing) == 0 {
		return ids
	}

	// Pick up to canaryTestCount random IDs from the ring
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

// SaveSuccess saves a successful extraction for the given version.
// Adds the videoID to the canary ring buffer.
// Triggers cleanup after stableSuccessCount successes on the active version.
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

	// Trigger cleanup when active version proves stable
	if version == versionmanager.state.ActiveVersion && entry.Successes == stableSuccessCount {
		logger.Infof("[yt-dlp] Active version %s reached %d successes, running cleanup", version, stableSuccessCount)
		versionmanager.cleanupOldVersions()
	}

	versionmanager.persist()
}

// SaveError saves a non-definitive extraction error for the given version.
// Skips definitive unavailability errors and network errors.
// Deduplicates by videoID within the rollback window.
func (versionmanager *VersionManager) SaveError(version, videoID string, errMsg string) {
	// Filter out errors that aren't yt-dlp's fault
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

	// Prune old errors outside the window
	fresh := make([]ErrorRecord, 0, len(entry.Errors))
	for _, e := range entry.Errors {
		if e.Time.After(cutoff) {
			fresh = append(fresh, e)
		}
	}
	entry.Errors = fresh

	// Deduplicate by videoID within the window
	if videoID != "" {
		for _, e := range entry.Errors {
			if e.VideoID == videoID {
				return // Already recorded for this video in this window
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

// shouldRollback checks if the active version has enough recent errors to trigger rollback.
// Must be called with at least a read lock held.
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

// selectBestVersion returns the latest non-blacklisted version with at least 1 production success.
// Must be called with at least a read lock held.
func (versionmanager *VersionManager) selectBestVersion() string {
	var candidates []string
	for ver, entry := range versionmanager.state.Versions {
		if entry.State != StateBlacklisted && entry.Successes > 0 && ver != versionmanager.state.ActiveVersion {
			candidates = append(candidates, ver)
		}
	}

	if len(candidates) == 0 {
		return versionmanager.state.ActiveVersion // No fallback available, stay on current
	}

	// Sort descending (newest first) — yt-dlp versions are date-based (YYYY.MM.DD)
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	return candidates[0]
}

// ActiveBinaryPath returns the path to the active yt-dlp binary.
// On each call, checks for pending promotions and rollback conditions.
func (versionmanager *VersionManager) ActiveBinaryPath() string {
	versionmanager.mu.Lock()
	defer versionmanager.mu.Unlock()

	// Check if a verified version should be promoted
	versionmanager.tryPromoteVerified()

	// Check if active version needs rollback
	if versionmanager.shouldRollback() {
		best := versionmanager.selectBestVersion()
		if best != versionmanager.state.ActiveVersion {
			logger.Warnf("[yt-dlp] Rolling back from %s to %s", versionmanager.state.ActiveVersion, best)

			// Blacklist the current active version
			if entry, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
				entry.State = StateBlacklisted
				entry.BlacklistedAt = time.Now()
			}

			// Activate the fallback
			if entry, ok := versionmanager.state.Versions[best]; ok {
				entry.State = StateActive
			}
			versionmanager.state.ActiveVersion = best
			versionmanager.persist()
		}
	}

	// Return path for active version
	if entry, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
		return entry.Path
	}

	// Fallback: no version manager state, use legacy path
	return GetLegacyBinaryPath()
}

// tryPromoteVerified promotes the newest verified version to active if it's newer
// than the current active version. Must be called with write lock held.
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

	// Demote old active
	if old, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
		if old.State == StateActive {
			old.State = StateVerified
		}
	}

	// Promote new
	versionmanager.state.Versions[bestVerified].State = StateActive
	versionmanager.state.ActiveVersion = bestVerified
	versionmanager.persist()
}

// cleanupOldVersions removes versions that are no longer needed.
// Protected: active version + fallback (latest non-active with successes).
// Deletable: blacklisted, superseded verified, stale pending, old working below fallback.
// Must be called with write lock held.
func (versionmanager *VersionManager) cleanupOldVersions() {
	active := versionmanager.state.ActiveVersion
	fallback := versionmanager.selectBestVersion()

	var toDelete []string
	for ver, entry := range versionmanager.state.Versions {
		// Never delete active or fallback
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
			// Superseded verified: never activated, a newer version is already active
			if ver < active {
				shouldDelete = true
				reason = "superseded verified"
			}
		case StatePending:
			// Stale pending: canary never completed after 48h
			if time.Since(entry.RegisteredAt) > stalePendingTimeout {
				shouldDelete = true
				reason = "stale pending"
			}
		default:
			// Old working versions below the fallback
			if entry.Successes > 0 && ver < fallback {
				shouldDelete = true
				reason = "old working below fallback"
			}
		}

		if shouldDelete {
			toDelete = append(toDelete, ver)
			logger.Infof("[yt-dlp] Marking %s for cleanup: %s", ver, reason)
		}
	}

	for _, ver := range toDelete {
		entry := versionmanager.state.Versions[ver]

		// Remove directory from disk
		dir := filepath.Dir(entry.Path)
		if err := os.RemoveAll(dir); err != nil {
			logger.Warnf("[yt-dlp] Failed to remove directory %s: %v", dir, err)
		} else {
			logger.Infof("[yt-dlp] Removed version directory: %s", dir)
		}

		// Remove from registry
		delete(versionmanager.state.Versions, ver)
	}

	if len(toDelete) > 0 {
		versionmanager.persist()
		logger.Infof("[yt-dlp] Cleanup complete: removed %d version(s), %d remaining", len(toDelete), len(versionmanager.state.Versions))
	}
}

// canaryResult represents the outcome of a single canary test
type canaryResult struct {
	videoID string
	success bool
	network bool // true if failure was a network error
	errMsg  string
}

// RunCanary smoke-tests a version against known-good video IDs.
// Returns: true if canary passed, false if failed.
// On network errors, returns false but the version should stay pending (not blacklisted).
// The caller must check the second return value (networkError) to distinguish.
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
	logger.Infof("[yt-dlp] Running canary for %s with %d video(s)", version, len(ids))

	for _, id := range ids {
		result := versionmanager.testExtraction(binaryPath, id)
		if !result.success {
			if result.network {
				logger.Warnf("[yt-dlp] Canary network error for %s (video %s): %s", version, id, result.errMsg)
				return false, true
			}
			logger.Warnf("[yt-dlp] Canary FAILED for %s (video %s): %s", version, id, result.errMsg)
			return false, false
		}
		logger.Debugf("[yt-dlp] Canary passed for %s (video %s)", version, id)
	}

	logger.Infof("[yt-dlp] Canary PASSED for %s", version)
	return true, false
}

// testExtraction runs yt-dlp --dump-json on a video ID and checks for valid output
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
		// Try to get stderr for better error info
		if exitErr, ok := err.(*exec.ExitError); ok {
			errMsg = string(exitErr.Stderr)
		}

		if IsNetworkError(errMsg) || ctx.Err() != nil {
			return canaryResult{videoID: videoID, success: false, network: true, errMsg: errMsg}
		}
		return canaryResult{videoID: videoID, success: false, network: false, errMsg: errMsg}
	}

	// Verify the output is valid JSON with a URL field
	var info struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return canaryResult{videoID: videoID, success: false, network: false, errMsg: "invalid JSON output"}
	}
	if info.URL == "" {
		return canaryResult{videoID: videoID, success: false, network: false, errMsg: "empty stream URL in output"}
	}

	return canaryResult{videoID: videoID, success: true}
}
