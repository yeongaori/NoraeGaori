package player

import (
	"fmt"
	"math"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func SetVolume(guildID string, volume float64) error {

	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		return fmt.Errorf("volume must be a valid number")
	}

	if volume < 0 || volume > 1000 {
		return fmt.Errorf("volume must be between 0 and 1000")
	}

	if err := queue.SetVolume(guildID, volume); err != nil {
		return err
	}

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.Volume = volume / 100.0
	player.mu.Unlock()

	logger.Debugf("Set volume to %g%% for guild: %s", volume, guildID)
	return nil
}

func rampVolumeBeforeStop(guildID string) {
	if enabled, err := queue.GetFadeOnStop(guildID); err == nil && enabled {
		rampVolumeDown(guildID, 1)
		return
	}

	player := GetPlayer(guildID)
	player.mu.Lock()
	fadingIn := player.FadingIn
	player.mu.Unlock()
	if !fadingIn {
		return
	}
	if fadeOut, err := queue.GetFadeOut(guildID); err == nil && fadeOut {
		rampVolumeDown(guildID, 1)
	}
}

func rampVolumeDown(guildID string, seconds float64) {
	player := GetPlayer(guildID)

	player.mu.Lock()
	if player.Ramping {
		player.mu.Unlock()
		for {
			time.Sleep(20 * time.Millisecond)
			player.mu.Lock()
			ramping := player.Ramping
			player.mu.Unlock()
			if !ramping {
				return
			}
		}
	}
	playing := player.Playing
	paused := player.Paused
	start := player.Volume
	if !playing || paused || start <= 0 {
		player.mu.Unlock()
		return
	}
	player.Ramping = true
	player.mu.Unlock()

	steps := int(seconds * framesPerSecond)
	if steps < 1 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		p := float64(i) / float64(steps)
		player.mu.Lock()
		player.Volume = start * qsinOut(p)
		player.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}

	player.mu.Lock()
	player.Volume = 0
	player.Ramping = false
	player.mu.Unlock()
}
