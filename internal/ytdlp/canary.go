package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os/exec"
	"time"

	"noraegaori/internal/dependency"
	"noraegaori/internal/logger"
)

const canaryAudioFormat = "bestaudio/best"

var fixedCanaryIDs = []string{
	"jNQXAC9IVRw",
	"BaW_jenozKc",
}

func isFixedCanaryID(candidate string) bool {
	for _, fixed := range fixedCanaryIDs {
		if fixed == candidate {
			return true
		}
	}
	return false
}

func isVideoID(candidate string) bool {
	if len(candidate) != videoIDLength {
		return false
	}
	for _, r := range candidate {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
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

	if len(versionmanager.state.CanaryRing) > 0 {
		ring := make([]string, len(versionmanager.state.CanaryRing))
		copy(ring, versionmanager.state.CanaryRing)
		rand.Shuffle(len(ring), func(i, j int) { ring[i], ring[j] = ring[j], ring[i] }) // #nosec G404

		count := canaryTestCount
		if count > len(ring) {
			count = len(ring)
		}
		ids = append(ids, ring[:count]...)
	}

	return append(ids, fixedCanaryIDs...)
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
	logger.Debugf("Running canary for %s with %d video(s)", version, len(ids))

	var (
		networkCount      int
		inconclusiveCount int
		hardFailCount     int
		ringFailCount     int
		lastFailure       string
	)

	for _, id := range ids {
		result := versionmanager.testExtraction(binaryPath, id)
		switch {
		case result.success:
			logger.Infof("Canary PASSED for %s", version)
			return true, false
		case result.network:
			logger.Debugf("Canary network error for %s: %s", version, result.errMsg)
			networkCount++
		case result.inconclusive:
			logger.Debugf("Canary inconclusive for %s (not a binary problem): %s", version, result.errMsg)
			inconclusiveCount++
		default:
			logger.Debugf("Canary video %s failed for %s: %s", id, version, result.errMsg)
			hardFailCount++
			lastFailure = result.errMsg
			if !isFixedCanaryID(id) {
				ringFailCount++
				if ringFailCount >= ringFailureQuorum {
					logger.Warnf("Canary FAILED for %s: %d recently played videos failed: %s", version, ringFailCount, lastFailure)
					return false, false
				}
			}
		}
	}

	if hardFailCount > 0 {
		logger.Warnf("Canary FAILED for %s (%d/%d videos): %s", version, hardFailCount, len(ids), lastFailure)
		return false, false
	}

	if networkCount > 0 && inconclusiveCount == 0 {

		logger.Warnf("All canary tests for %s hit network errors; version stays pending", version)
		return false, true
	}

	logger.Warnf("Canary inconclusive for %s — no testable videos but no evidence of binary breakage", version)
	return true, false
}

func (versionmanager *VersionManager) testExtraction(binaryPath, videoID string) canaryResult {
	if !isVideoID(videoID) {
		return canaryResult{videoID: videoID, inconclusive: true, errMsg: "not a video ID"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	args := []string{"-f", canaryAudioFormat, "--dump-json", "--no-playlist", "--no-warnings", url}

	if rt := dependency.JsRuntimeArg(); rt != "" {
		args = append([]string{"--js-runtimes", rt}, args...)
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...) // #nosec G204
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
	streamURL := info.URL
	if streamURL == "" && len(info.Formats) > 0 {
		streamURL = info.Formats[len(info.Formats)-1].URL
	}
	if streamURL == "" {
		return canaryResult{videoID: videoID, errMsg: "extractor returned no id, formats, or url"}
	}

	if probeErr := probeStreamReachable(ctx, streamURL); probeErr != nil {
		if IsNetworkError(probeErr.Error()) || ctx.Err() != nil {
			return canaryResult{videoID: videoID, network: true, errMsg: probeErr.Error()}
		}
		return canaryResult{videoID: videoID, errMsg: probeErr.Error()}
	}

	return canaryResult{videoID: videoID, success: true}
}

func probeStreamReachable(ctx context.Context, streamURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("stream URL returned HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, 512)
	if _, err := io.ReadFull(resp.Body, buf); err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("stream URL produced no data: %w", err)
	}
	return nil
}
