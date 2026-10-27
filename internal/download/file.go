package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"

	"noraegaori/internal/config"
	"noraegaori/internal/logger"
)

var downloadStallTimeout = 2 * time.Minute

func rateLimitBytesPerSecond() float64 {
	mbps := config.DefaultMaxDownloadSpeedMbps
	if cfg := config.GetConfig(); cfg != nil && cfg.MaxDownloadSpeedMbps > 0 {
		mbps = cfg.MaxDownloadSpeedMbps
	}
	return mbps * 1000 * 1000 / 8
}

var (
	showProgress = logger.Progress
	endProgress  = logger.EndProgress
)

func openDownload(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download returned status: %d", resp.StatusCode)
	}
	return resp, nil
}

func copyThrottled(ctx context.Context, body io.Reader, sink io.Writer, stall *time.Timer) error {
	rateLimit := rateLimitBytesPerSecond()
	buffer := make([]byte, 16*1024)
	chunkDelay := time.Duration(float64(len(buffer)) / rateLimit * float64(time.Second))

	for {
		n, err := body.Read(buffer)
		if n > 0 {
			stall.Reset(downloadStallTimeout)
			if _, writeErr := sink.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to file: %w", writeErr)
			}

			time.Sleep(chunkDelay)
		}
		if err == io.EOF {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("the mirror sent nothing for %v, so the download was abandoned", downloadStallTimeout)
		}
		if err != nil {
			return fmt.Errorf("download interrupted: %w", err)
		}
	}
}

func SaveFile(url, destination string) (string, error) {
	name := path.Base(url)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stall := time.AfterFunc(downloadStallTimeout, cancel)
	defer stall.Stop()

	resp, err := openDownload(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	out, err := os.Create(destination) // #nosec G304
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	totalSize := resp.ContentLength
	if totalSize > 0 {
		logger.Infof("Downloading %s (%.1f MB)", name, float64(totalSize)/1000/1000)
	} else {
		logger.Infof("Downloading %s", name)
	}
	defer endProgress()

	hasher := sha256.New()
	progress := &ProgressWriter{Total: totalSize, Report: func(percent int) {
		showProgress(percent, fmt.Sprintf("Downloading %s: %d%%", name, percent))
	}}
	sink := io.MultiWriter(out, hasher, progress)

	if err := copyThrottled(ctx, resp.Body, sink, stall); err != nil {
		return "", err
	}

	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("failed to flush file: %w", err)
	}

	logger.Infof("Downloaded %s", name)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
