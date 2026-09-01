package youtube

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/internal/config"
	"noraegaori/internal/logger"
	ytdlpUpdater "noraegaori/internal/ytdlp"
)

type StreamPipe struct {
	cmd        *exec.Cmd
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	ctx        context.Context
	cancel     context.CancelFunc
	closed     atomic.Bool
	closeMu    sync.Mutex
	stderrDone chan struct{}
}

func (sp *StreamPipe) Read(p []byte) (n int, err error) {
	return sp.stdout.Read(p)
}

func (sp *StreamPipe) Close() error {

	if !sp.closed.CompareAndSwap(false, true) {
		return nil
	}

	sp.closeMu.Lock()
	defer sp.closeMu.Unlock()

	logger.Debugf("Closing stream pipe")

	sp.cancel()

	if sp.stdout != nil {
		sp.stdout.Close()
	}
	if sp.stderr != nil {
		sp.stderr.Close()
	}

	if sp.stderrDone != nil {
		select {
		case <-sp.stderrDone:
			logger.Debugf("stderr goroutine finished")
		case <-time.After(2 * time.Second):
			logger.Warnf("stderr goroutine did not finish in time")
		}
	}

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

		if sp.cmd != nil && sp.cmd.Process != nil {
			logger.Warnf("Force killing yt-dlp process")
			if err := sp.cmd.Process.Kill(); err != nil {
				logger.Warnf("Failed to kill yt-dlp process: %v", err)
			}

			<-done
		}
		return fmt.Errorf("yt-dlp process did not exit gracefully")
	case err := <-done:
		logger.Debugf("yt-dlp process exited")
		return err
	}
}

func downloadRateLimitArg() (string, bool) {
	cfg := config.GetConfig()
	if cfg == nil || cfg.MaxDownloadSpeedMbps <= 0 {
		return "", false
	}

	limitMbps := cfg.MaxDownloadSpeedMbps - 0.1
	if limitMbps < 0.1 {
		limitMbps = 0.1
	}

	return fmt.Sprintf("%.2fM", limitMbps/8.0), true
}

func streamPipeArgs(url string, sponsorBlock bool, bitrate, seekTime int) []string {
	audioFormat := GetOptimalAudioFormat(bitrate)
	logger.Debugf("Creating stream pipe for: %s (SponsorBlock: %v, Format: %s)", url, sponsorBlock, audioFormat)

	args := []string{
		"--no-warnings",
		"--no-playlist",
		"--format", audioFormat,
		"--output", "-",
	}

	if rt := ytdlpUpdater.GetJsRuntime(); rt != "" {
		args = append(args, "--js-runtimes", rt)
	}

	if rateLimit, limited := downloadRateLimitArg(); limited {
		args = append(args, "--limit-rate", rateLimit)
		logger.Debugf("Applying download rate limit: %s MB/s", rateLimit)
	}

	if sponsorBlock {
		args = append(args,
			"--sponsorblock-mark", "all",
			"--sponsorblock-remove", "sponsor,selfpromo,interaction,intro,outro",
		)
	}

	if seekTime > 0 {
		seekSeconds := float64(seekTime) / 1000.0
		downloadSection := fmt.Sprintf("*%.1f-inf", seekSeconds)
		args = append(args, "--download-sections", downloadSection)
		logger.Debugf("Seeking to %.1fs using --download-sections %s", seekSeconds, downloadSection)
	}

	return append(args, url)
}

func logStderrUntilClosed(ctx context.Context, stderrPipe io.ReadCloser) chan struct{} {
	stderrDone := make(chan struct{})

	go func() {
		defer close(stderrDone)
		defer stderrPipe.Close()

		buf := make([]byte, 512)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := stderrPipe.Read(buf)
				if n > 1 {
					logger.Debugf("%s", string(buf[:n]))
				}
				if err != nil {
					return
				}
			}
		}
	}()

	return stderrDone
}

func GetStreamPipe(url string, sponsorBlock bool, bitrate int, seekTime int) (*StreamPipe, error) {

	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("Circuit breaker open: %v", err)
		return nil, err
	}

	args := streamPipeArgs(url, sponsorBlock, bitrate, seekTime)

	ctx, cancel := context.WithCancel(context.Background())

	binaryPath := ytdlpUpdater.GetBinaryPath()
	if _, err := os.Stat(binaryPath); err != nil {
		cancel()
		logger.Errorf("yt-dlp binary missing at %s: %v", binaryPath, err)
		return nil, fmt.Errorf("yt-dlp binary unavailable; the updater will retry in the background")
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		ytCircuitBreaker.recordFailure(err)
		saveVersionResult(url, err)
		return nil, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	stderrDone := logStderrUntilClosed(ctx, stderrPipe)

	logger.Debugf("Started yt-dlp streaming process for: %s", url)

	ytCircuitBreaker.recordSuccess()
	saveVersionResult(url, nil)

	return &StreamPipe{
		cmd:        cmd,
		stdout:     stdout,
		stderr:     stderrPipe,
		ctx:        ctx,
		cancel:     cancel,
		stderrDone: stderrDone,
	}, nil
}
