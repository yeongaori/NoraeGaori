package youtube

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/internal/config"
	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"
)

// StreamPipe represents a streaming yt-dlp process
// Uses pointers and careful resource management for 1000+ concurrent servers
type StreamPipe struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	ctx       context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
	closeMu   sync.Mutex
	stderrDone chan struct{} // Ensures stderr goroutine cleanup
}

// Read implements io.Reader
func (sp *StreamPipe) Read(p []byte) (n int, err error) {
	return sp.stdout.Read(p)
}

// Close terminates the yt-dlp process and cleans up resources
// Safe to call multiple times (idempotent)
func (sp *StreamPipe) Close() error {
	// Ensure we only close once (prevent double-close panics)
	if !sp.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	sp.closeMu.Lock()
	defer sp.closeMu.Unlock()

	logger.Debugf("[StreamPipe] Closing stream pipe")

	// Cancel context first to signal goroutines
	sp.cancel()

	// Close pipes (safe to close multiple times)
	if sp.stdout != nil {
		sp.stdout.Close()
	}
	if sp.stderr != nil {
		sp.stderr.Close()
	}

	// Wait for stderr goroutine to finish (prevents goroutine leak)
	if sp.stderrDone != nil {
		select {
		case <-sp.stderrDone:
			logger.Debugf("[StreamPipe] stderr goroutine finished")
		case <-time.After(2 * time.Second):
			logger.Warnf("[StreamPipe] stderr goroutine did not finish in time")
		}
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		if sp.cmd != nil && sp.cmd.Process != nil {
			done <- sp.cmd.Wait()
		} else {
			done <- nil
		}
	}()

	select {
	case <-time.After(5 * time.Second):
		// Force kill if not exited
		if sp.cmd != nil && sp.cmd.Process != nil {
			logger.Warnf("[StreamPipe] Force killing yt-dlp process")
			sp.cmd.Process.Kill()
			// Wait a bit more for kill to complete
			<-done
		}
		return fmt.Errorf("yt-dlp process did not exit gracefully")
	case err := <-done:
		logger.Debugf("[StreamPipe] yt-dlp process exited")
		return err
	}
}

// GetStreamPipe creates a direct streaming pipe from yt-dlp
// This eliminates the need to fetch the URL separately and re-download
// seekTime is in milliseconds - if > 0, will start streaming from that position
func GetStreamPipe(url string, sponsorBlock bool, bitrate int, seekTime int) (*StreamPipe, error) {
	// Check circuit breaker
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[StreamPipe] Circuit breaker open: %v", err)
		return nil, err
	}

	audioFormat := GetOptimalAudioFormat(bitrate)
	logger.Debugf("[StreamPipe] Creating stream pipe for: %s (SponsorBlock: %v, Format: %s)", url, sponsorBlock, audioFormat)

	// Create context without timeout to support videos of any length
	// Previous timeout (10 minutes) was causing long videos to stop prematurely
	// ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	ctx, cancel := context.WithCancel(context.Background())

	// Build yt-dlp command to output directly to stdout
	args := []string{
		"--no-warnings",
		"--no-playlist",
		"--js-runtimes", "node",
		"--format", audioFormat,
		"--output", "-", // Output to stdout
	}

	// Add download speed limit from config
	// Discord upload is ~0.064 Mbps (negligible), so we use almost full limit for download
	cfg := config.GetConfig()
	if cfg != nil && cfg.MaxDownloadSpeedMbps > 0 {
		// Convert Mbps to MB/s for yt-dlp (Mbps / 8 = MB/s)
		// Subtract 0.1 Mbps to account for Discord upload and overhead
		downloadLimitMbps := cfg.MaxDownloadSpeedMbps - 0.1
		if downloadLimitMbps < 0.1 {
			downloadLimitMbps = 0.1 // Minimum 0.1 Mbps
		}
		downloadLimitMBs := downloadLimitMbps / 8.0
		rateLimitStr := fmt.Sprintf("%.2fM", downloadLimitMBs)
		args = append(args, "--limit-rate", rateLimitStr)
		logger.Debugf("[StreamPipe] Applying download rate limit: %s MB/s (%.1f Mbps)", rateLimitStr, downloadLimitMbps)
	}

	// Add SponsorBlock if enabled
	if sponsorBlock {
		args = append(args,
			"--sponsorblock-mark", "all",
			"--sponsorblock-remove", "sponsor,selfpromo,interaction,intro,outro",
		)
	}

	// Add seek time if resuming playback
	if seekTime > 0 {
		seekSeconds := float64(seekTime) / 1000.0
		downloadSection := fmt.Sprintf("*%.1f-inf", seekSeconds)
		args = append(args, "--download-sections", downloadSection)
		logger.Infof("[StreamPipe] Seeking to %.1fs using --download-sections %s", seekSeconds, downloadSection)
	}

	args = append(args, url)

	// Create command
	cmd := exec.CommandContext(ctx, ytdlpUpdater.GetBinaryPath(), args...)

	// Get stdout pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Capture stderr for error logging
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start yt-dlp process
	if err := cmd.Start(); err != nil {
		cancel()
		ytCircuitBreaker.recordFailure(err)
		return nil, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	// Create done channel for stderr goroutine
	stderrDone := make(chan struct{})

	// Log stderr in background (with proper cleanup signaling)
	go func() {
		defer close(stderrDone) // Signal completion to prevent goroutine leak
		defer stderrPipe.Close()

		// Use small buffer on stack (no heap allocation)
		buf := make([]byte, 512)
		for {
			select {
			case <-ctx.Done():
				// Context cancelled, exit immediately
				return
			default:
				n, err := stderrPipe.Read(buf)
				if n > 0 {
					// Only log if there's actual content (avoid spam)
					if n > 1 { // More than just newline
						logger.Debugf("[StreamPipe stderr] %s", string(buf[:n]))
					}
				}
				if err != nil {
					// EOF or error, exit
					return
				}
			}
		}
	}()

	logger.Infof("[StreamPipe] Started yt-dlp streaming process for: %s", url)

	// Record success in circuit breaker (process started successfully)
	ytCircuitBreaker.recordSuccess()

	return &StreamPipe{
		cmd:        cmd,
		stdout:     stdout,
		stderr:     stderrPipe,
		ctx:        ctx,
		cancel:     cancel,
		stderrDone: stderrDone,
	}, nil
}
