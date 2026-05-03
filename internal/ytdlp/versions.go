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

// fixedCanaryIDs are stable, non-age-restricted YouTube videos used as the
// baseline canary set. The first ID is "Me at the zoo" — the first ever
// YouTube video, public and not age-gated. The second is yt-dlp's own
// canonical test video used in their own CI suite.
var fixedCanaryIDs = []string{
	"jNQXAC9IVRw",
	"BaW_jenozKc",
}

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
	logger.Debugf("[yt-dlp] Registered version %s at %s", version, path)
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
	logger.Debugf("[yt-dlp] Version %s -> %s", version, state)
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

// HasUsableBinary returns true if any registered version with a settled
// non-broken state has its binary file present on disk and runs --version
// successfully. Pending versions are excluded because their viability is
// unknown; Blacklisted versions are excluded because they are known bad.
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

// ProvisionallyActivate points ActiveVersion at the given version with state
// StateProvisional. Used by the last-resort path when no version has passed
// canary but the bot still needs a binary. Real-traffic SaveSuccess and
// rollback handle correctness from here. Crucially, StateProvisional is NOT
// StateBlacklisted — subsequent boots will not re-trigger the fallback chain
// for this version, avoiding the per-boot retry storm.
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

// getCanaryIDs returns the fixed canary set + up to canaryTestCount random IDs from the ring
func (versionmanager *VersionManager) getCanaryIDs() []string {
	versionmanager.mu.RLock()
	defer versionmanager.mu.RUnlock()

	ids := make([]string, 0, len(fixedCanaryIDs)+canaryTestCount)
	ids = append(ids, fixedCanaryIDs...)

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

	// Promote a Provisional active version to Active once real traffic has
	// proved the canary's negative verdict wrong.
	if entry.State == StateProvisional && version == versionmanager.state.ActiveVersion && entry.Successes >= stableSuccessCount {
		entry.State = StateActive
		logger.Infof("[yt-dlp] Provisional version %s promoted to Active after %d real successes", version, entry.Successes)
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

	// Return path for active version, but only if the binary actually exists
	// on disk. A missing binary here means something deleted it out of band
	// (manual cleanup, container volume reset). Fall back to the legacy path
	// rather than handing out a phantom path that fails opaquely at exec time.
	if entry, ok := versionmanager.state.Versions[versionmanager.state.ActiveVersion]; ok {
		if _, err := os.Stat(entry.Path); err == nil {
			return entry.Path
		}
		logger.Errorf("[yt-dlp] Active binary %s missing on disk; falling back to legacy path", entry.Path)
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
			logger.Debugf("[yt-dlp] Marking %s for cleanup: %s", ver, reason)
		}
	}

	for _, ver := range toDelete {
		entry := versionmanager.state.Versions[ver]

		// Remove directory from disk
		dir := filepath.Dir(entry.Path)
		if err := os.RemoveAll(dir); err != nil {
			logger.Warnf("[yt-dlp] Failed to remove directory %s: %v", dir, err)
		} else {
			logger.Debugf("[yt-dlp] Removed version directory: %s", dir)
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
	videoID      string
	success      bool
	network      bool // failure was a transient network error
	inconclusive bool // failure was a definitive video-unavailability (not a binary problem)
	errMsg       string
}

// RunCanary smoke-tests a version against known-good video IDs.
// Returns (passed, networkError):
//   - passed=true  → canary OK (or all tests inconclusive — no evidence of binary breakage)
//   - passed=false, networkError=true  → all tests hit network errors; version stays pending
//   - passed=false, networkError=false → at least one concrete binary failure; caller should blacklist
//
// A single concrete success short-circuits to passed=true regardless of other results.
// A single concrete failure (non-network, non-inconclusive) short-circuits to passed=false.
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

	// No concrete success and no concrete failure. All results were either
	// transient network errors or per-video unavailability — no evidence of
	// a broken binary either way.
	if networkCount > 0 && inconclusiveCount == 0 {
		// All-network: stay pending, retry later.
		logger.Warnf("[yt-dlp] All canary tests for %s hit network errors; version stays pending", version)
		return false, true
	}

	// All tests inconclusive (or a mix with no network-only set). The binary
	// hasn't been proven good, but we have no evidence of breakage. Pass with
	// a warning; real-traffic SaveError will catch real regressions.
	logger.Warnf("[yt-dlp] Canary inconclusive for %s — no testable videos but no evidence of binary breakage", version)
	return true, false
}

// testExtraction runs yt-dlp --dump-json on a video ID and checks for valid output.
// Distinguishes three failure modes:
//   - network: transient connection problems (caller should retry later)
//   - inconclusive: the video itself isn't extractable (age-gated, geo-blocked,
//     deleted, login-required); not a binary problem, try a different video
//   - concrete failure: anything else (genuine binary breakage)
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

	// Validate JSON and accept any of the three indicators that the YouTube
	// extractor responded sensibly: matching id, non-empty formats, or non-empty
	// top-level url. The top-level url field is downstream of format selection
	// and isn't always populated even on healthy extractions.
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
