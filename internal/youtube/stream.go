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
	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"
)

type StreamPipe struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	ctx       context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
	closeMu   sync.Mutex
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

	logger.Debugf("[StreamPipe] Closing stream pipe")

	
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
			logger.Debugf("[StreamPipe] stderr goroutine finished")
		case <-time.After(2 * time.Second):
			logger.Warnf("[StreamPipe] stderr goroutine did not finish in time")
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
			logger.Warnf("[StreamPipe] Force killing yt-dlp process")
			sp.cmd.Process.Kill()
			
			<-done
		}
		return fmt.Errorf("yt-dlp process did not exit gracefully")
	case err := <-done:
		logger.Debugf("[StreamPipe] yt-dlp process exited")
		return err
	}
}

func GetStreamPipe(url string, sponsorBlock bool, bitrate int, seekTime int) (*StreamPipe, error) {
	
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[StreamPipe] Circuit breaker open: %v", err)
		return nil, err
	}

	audioFormat := GetOptimalAudioFormat(bitrate)
	logger.Debugf("[StreamPipe] Creating stream pipe for: %s (SponsorBlock: %v, Format: %s)", url, sponsorBlock, audioFormat)

	
	
	
	ctx, cancel := context.WithCancel(context.Background())

	
	args := []string{
		"--no-warnings",
		"--no-playlist",
		"--format", audioFormat,
		"--output", "-", 
	}

	if rt := ytdlpUpdater.GetJsRuntime(); rt != "" {
		args = append(args, "--js-runtimes", rt)
	}

	
	
	cfg := config.GetConfig()
	if cfg != nil && cfg.MaxDownloadSpeedMbps > 0 {
		
		
		downloadLimitMbps := cfg.MaxDownloadSpeedMbps - 0.1
		if downloadLimitMbps < 0.1 {
			downloadLimitMbps = 0.1 
		}
		downloadLimitMBs := downloadLimitMbps / 8.0
		rateLimitStr := fmt.Sprintf("%.2fM", downloadLimitMBs)
		args = append(args, "--limit-rate", rateLimitStr)
		logger.Debugf("[StreamPipe] Applying download rate limit: %s MB/s (%.1f Mbps)", rateLimitStr, downloadLimitMbps)
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
		logger.Debugf("[StreamPipe] Seeking to %.1fs using --download-sections %s", seekSeconds, downloadSection)
	}

	args = append(args, url)

	binaryPath := ytdlpUpdater.GetBinaryPath()
	if _, err := os.Stat(binaryPath); err != nil {
		cancel()
		logger.Errorf("[StreamPipe] yt-dlp binary missing at %s: %v", binaryPath, err)
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
				if n > 0 {
					
					if n > 1 { 
						logger.Debugf("[StreamPipe stderr] %s", string(buf[:n]))
					}
				}
				if err != nil {
					
					return
				}
			}
		}
	}()

	logger.Debugf("[StreamPipe] Started yt-dlp streaming process for: %s", url)

	
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
