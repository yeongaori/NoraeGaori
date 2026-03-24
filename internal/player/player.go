package player

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"
	"sync"
	"time"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"

	"github.com/bwmarrin/discordgo"
)

// ErrQueueEmpty is returned when skip causes the queue to become empty
var ErrQueueEmpty = errors.New("queue is empty after skip")

const (
	channels     = 2                // Stereo
	frameRate    = 48000            // 48kHz
	frameSize    = 960              // 20ms frame at 48kHz
	maxRetries   = 3                // Maximum retry attempts for failed songs
	lockTimeout  = 30 * time.Second // Play lock timeout
	stallTimeout = 30 * time.Second // Stream stall detection timeout
)

// PlayerCommand represents a command to the player
type PlayerCommand struct {
	Type    string // "play", "skip", "stop", "pause", "resume"
	Session *discordgo.Session
	GuildID string
	Done    chan error // Channel to signal completion
}

// GuildPlayer represents a music player for a guild
type GuildPlayer struct {
	GuildID          string
	VoiceConn        *discordgo.VoiceConnection
	VoiceChannelID   string // Track channel ID separately (not in VoiceConnection in new API)
	Playing          bool
	Paused           bool
	Loading          bool
	TogglingNorm     bool // Signal to restart FFmpeg for normalization change
	Volume           float64
	StopChan         chan struct{}
	PlaybackDone     chan struct{}      // Signaled when playback terminates
	CommandChan      chan PlayerCommand // Channel for player commands
	QuitChan         chan struct{}      // Channel to stop command processor
	PlaybackStart    time.Time
	mu               sync.Mutex
	processorRunning bool
}

var (
	// Guild players map
	players   = make(map[string]*GuildPlayer)
	playersMu sync.RWMutex

	// Play function locks per guild
	playLocks   = make(map[string]*sync.Mutex)
	playLocksMu sync.Mutex

	// Global loading messages map (keyed by guild ID)
	loadingMessages   = make(map[string]*discordgo.Message)
	loadingMessagesMu sync.RWMutex

	// Global reconnect messages map (keyed by guild ID)
	reconnectMessages   = make(map[string]*discordgo.Message)
	reconnectMessagesMu sync.RWMutex

	// Pre-cache storage (kept for backward compatibility with precache.go)
	// TODO: Migrate to Song-integrated pre-cache
	preCacheStore   = make(map[string]*PreCache)
	preCacheStoreMu sync.RWMutex

	// Playback retry tracking (map-based because Song objects are recreated from DB each iteration)
	playbackRetries   = make(map[string]int) // key: "guildID:songURL"
	playbackRetriesMu sync.Mutex

	// Callback for when song starts playing
	onSongStartCallback func(guildID string)
	callbackMu          sync.RWMutex
)

// PreCache represents pre-cached data for a song
type PreCache struct {
	Data       []byte // Pre-cached audio data (for PCM strategies)
	StreamURL  string // Pre-cached stream URL
	SongID     int
	Timestamp  time.Time
	CancelFunc context.CancelFunc
}

// acquirePlayLock gets or creates a play lock for a guild
func acquirePlayLock(guildID string) *sync.Mutex {
	playLocksMu.Lock()
	defer playLocksMu.Unlock()

	if _, exists := playLocks[guildID]; !exists {
		playLocks[guildID] = &sync.Mutex{}
	}
	return playLocks[guildID]
}

// GetPlayer gets or creates a player for a guild
func GetPlayer(guildID string) *GuildPlayer {
	playersMu.Lock()
	defer playersMu.Unlock()

	if player, exists := players[guildID]; exists {
		// Check if processor is still running (keep lock while restarting to prevent race)
		player.mu.Lock()
		running := player.processorRunning
		if !running {
			// Processor died, need to restart it
			logger.Warnf("[GetPlayer] Processor not running for guild %s, restarting", guildID)

			// Recreate channels under lock to prevent race condition
			player.CommandChan = make(chan PlayerCommand, 10)
			player.QuitChan = make(chan struct{})

			// Start processor (processorRunning set here under lock to prevent race)
			player.processorRunning = true
			go player.processCommands()
		}
		player.mu.Unlock()

		return player
	}

	player := &GuildPlayer{
		GuildID:          guildID,
		Playing:          false,
		Paused:           false,
		Loading:          false,
		Volume:           1.0,
		StopChan:         make(chan struct{}),
		PlaybackDone:     make(chan struct{}, 1),       // Buffered to prevent blocking
		CommandChan:      make(chan PlayerCommand, 10), // Buffered channel for commands
		QuitChan:         make(chan struct{}),
		processorRunning: true,
	}
	players[guildID] = player

	// Start command processor goroutine
	go player.processCommands()

	return player
}

// SetLoadingMessage stores a loading message for a guild
func SetLoadingMessage(guildID string, msg *discordgo.Message) {
	loadingMessagesMu.Lock()
	defer loadingMessagesMu.Unlock()
	loadingMessages[guildID] = msg
	logger.Debugf("[LoadingMessage] Stored loading message for guild: %s", guildID)
}

// GetLoadingMessage retrieves a loading message for a guild
func GetLoadingMessage(guildID string) *discordgo.Message {
	loadingMessagesMu.RLock()
	defer loadingMessagesMu.RUnlock()
	return loadingMessages[guildID]
}

// DeleteLoadingMessage removes a loading message for a guild
func DeleteLoadingMessage(guildID string) {
	loadingMessagesMu.Lock()
	defer loadingMessagesMu.Unlock()
	delete(loadingMessages, guildID)
	logger.Debugf("[LoadingMessage] Deleted loading message for guild: %s", guildID)
}

// setReconnectMessage stores a reconnect message for a guild
func setReconnectMessage(guildID string, msg *discordgo.Message) {
	reconnectMessagesMu.Lock()
	defer reconnectMessagesMu.Unlock()
	reconnectMessages[guildID] = msg
}

// getReconnectMessage retrieves a reconnect message for a guild
func getReconnectMessage(guildID string) *discordgo.Message {
	reconnectMessagesMu.RLock()
	defer reconnectMessagesMu.RUnlock()
	return reconnectMessages[guildID]
}

// deleteReconnectMessage removes a reconnect message for a guild
func deleteReconnectMessage(guildID string) {
	reconnectMessagesMu.Lock()
	defer reconnectMessagesMu.Unlock()
	delete(reconnectMessages, guildID)
}

// DeletePlayer removes a player for a guild
func DeletePlayer(guildID string) {
	playersMu.Lock()
	player, exists := players[guildID]
	if !exists {
		playersMu.Unlock()
		return
	}
	delete(players, guildID)
	playersMu.Unlock()

	// Signal command processor to stop
	close(player.QuitChan)

	// Close command channel to fail any pending commands
	close(player.CommandChan)

	// Clean up retry tracking for this guild
	clearRetryCountsForGuild(guildID)

	logger.Debugf("[DeletePlayer] Stopped command processor for guild: %s", guildID)
}

// SetOnSongStartCallback sets a callback to be called when a song starts playing
func SetOnSongStartCallback(callback func(guildID string)) {
	callbackMu.Lock()
	defer callbackMu.Unlock()
	onSongStartCallback = callback
}

// callOnSongStart calls the registered callback if it exists
func callOnSongStart(guildID string) {
	callbackMu.RLock()
	callback := onSongStartCallback
	callbackMu.RUnlock()

	if callback != nil {
		callback(guildID)
	}
}

// JoinVoice joins a voice channel
func JoinVoice(session *discordgo.Session, guildID, channelID string) (*discordgo.VoiceConnection, error) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	// If already connected to this channel, return existing connection
	if player.VoiceConn != nil && player.VoiceChannelID == channelID {
		return player.VoiceConn, nil
	}

	// Check if session already has a voice connection for this guild
	// This can happen if the player state was reset but the bot is still in voice
	// Since player.VoiceConn is nil (checked above), this is an orphaned/stale connection
	// (e.g., from a bot restart) — always disconnect and rejoin fresh
	session.RLock()
	existingVC, exists := session.VoiceConnections[guildID]
	session.RUnlock()
	if exists && existingVC != nil {
		logger.Infof("[Voice] Found stale session voice connection, disconnecting for guild: %s", guildID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		existingVC.Disconnect(ctx)
		cancel()
	}

	// Disconnect from old channel if connected (player level)
	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
	}

	// Join new voice channel with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vc, err := session.ChannelVoiceJoin(ctx, guildID, channelID, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	// Wait for voice connection to be ready with timeout
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if vc.Status == discordgo.VoiceConnectionStatusReady {
				logger.Infof("[Voice] Voice connection ready for guild: %s", guildID)
				player.VoiceConn = vc
				player.VoiceChannelID = channelID
				return vc, nil
			}
		case <-vc.Dead:
			return nil, fmt.Errorf("voice connection died: %v", vc.Err)
		case <-timeout:
			ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			vc.Disconnect(ctx2)
			cancel2()
			return nil, fmt.Errorf("timeout waiting for voice")
		}
	}
}

// LeaveVoice leaves the voice channel
func LeaveVoice(guildID string) error {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := player.VoiceConn.Disconnect(ctx); err != nil {
			return fmt.Errorf("failed to disconnect: %w", err)
		}
		player.VoiceConn = nil
		player.VoiceChannelID = ""
		logger.Infof("[Voice] Left voice channel in guild: %s", guildID)
	}

	return nil
}

// processCommands processes player commands sequentially using channels
func (p *GuildPlayer) processCommands() {
	defer func() {
		// Panic recovery to prevent goroutine death
		if r := recover(); r != nil {
			logger.Errorf("[CommandProcessor] Panic recovered for guild %s: %v", p.GuildID, r)
		}

		p.mu.Lock()
		p.processorRunning = false
		p.mu.Unlock()
		logger.Debugf("[CommandProcessor] Stopped for guild: %s", p.GuildID)
	}()

	logger.Debugf("[CommandProcessor] Started for guild: %s", p.GuildID)

	for {
		select {
		case cmd, ok := <-p.CommandChan:
			if !ok {
				// Channel closed, exit processor
				logger.Debugf("[CommandProcessor] CommandChan closed for guild: %s", p.GuildID)
				return
			}

			logger.Debugf("[CommandProcessor] Received %s command for guild: %s", cmd.Type, p.GuildID)

			// Process command with panic recovery
			func() {
				var err error
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("command panic: %v", r)
						logger.Errorf("[CommandProcessor] Command %s panicked for guild %s: %v", cmd.Type, p.GuildID, r)
					}

					logger.Debugf("[CommandProcessor] Command %s completed for guild %s with error: %v", cmd.Type, p.GuildID, err)

					// Send result back through Done channel
					if cmd.Done != nil {
						select {
						case cmd.Done <- err:
						default:
							logger.Warnf("[CommandProcessor] Could not send result for %s command in guild %s", cmd.Type, p.GuildID)
						}
						close(cmd.Done)
					}
				}()

				switch cmd.Type {
				case "play":
					err = playInternal(cmd.Session, cmd.GuildID)
				case "skip":
					logger.Debugf("[CommandProcessor] Processing skip command for guild: %s", p.GuildID)
					err = skipInternal(cmd.Session, cmd.GuildID)
				case "stop":
					err = stopInternal(cmd.GuildID)
				case "pause":
					err = pauseInternal(cmd.GuildID)
				case "resume":
					err = resumeInternal(cmd.Session, cmd.GuildID)
				default:
					err = fmt.Errorf("unknown command type: %s", cmd.Type)
				}
			}()

		case <-p.QuitChan:
			// Graceful shutdown requested
			logger.Debugf("[CommandProcessor] Quit signal received for guild: %s", p.GuildID)
			return
		}
	}
}

// Play queues a play command (non-blocking, uses channels)
func Play(session *discordgo.Session, guildID string) error {
	player := GetPlayer(guildID)

	cmd := PlayerCommand{
		Type:    "play",
		Session: session,
		GuildID: guildID,
	}

	// Send command to queue (with panic recovery for closed channel)
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[Play] Recovered from panic (channel likely closed) for guild %s: %v", guildID, r)
		}
	}()

	select {
	case player.CommandChan <- cmd:
		// Command queued successfully - playInternal blocks for entire playback,
		// so we just return nil after queuing since the command processor handles it
		return nil
	default:
		// Channel full
		logger.Warnf("[Play] Command queue full for guild %s", guildID)
		return fmt.Errorf("command queue full, please try again")
	}
}

// playInternal is the actual play implementation (called by command processor)
// Uses a loop to continue playing songs without re-acquiring the lock
func playInternal(session *discordgo.Session, guildID string) error {
	// Acquire play lock with timeout - using TryLock pattern to avoid goroutine leaks
	lock := acquirePlayLock(guildID)

	// Try to acquire lock with timeout using a goroutine-safe pattern
	lockAcquired := make(chan bool, 1)
	unlockChan := make(chan struct{})

	go func() {
		lock.Lock()
		select {
		case lockAcquired <- true:
			// Lock acquired and caller notified
			<-unlockChan // Wait for signal to unlock
			lock.Unlock()
		default:
			// Timeout already occurred, unlock immediately
			lock.Unlock()
		}
	}()

	select {
	case <-lockAcquired:
		// Successfully acquired lock
		defer close(unlockChan) // Signal goroutine to unlock
	case <-time.After(lockTimeout):
		logger.Warnf("[Play] Lock timeout for guild: %s", guildID)
		return fmt.Errorf("play lock timeout")
	}

	logger.Debugf("[Play] Lock acquired for guild: %s", guildID)

	// Use a loop to play songs continuously without releasing/reacquiring the lock
	for {
		result := playSingleSong(session, guildID)
		switch result {
		case playContinue:
			// Continue to next song in the loop
			continue
		case playStop:
			// Stop playback (queue empty, stopped by user, etc.)
			return nil
		case playError:
			// An error occurred that should be returned
			return fmt.Errorf("playback error")
		}
	}
}

// playResult represents the result of playing a single song
type playResult int

const (
	playContinue playResult = iota // Continue to next song
	playStop                       // Stop playback
	playError                      // Error occurred
)

// playSingleSong plays a single song and returns whether to continue, stop, or error
func playSingleSong(session *discordgo.Session, guildID string) playResult {
	// Get queue
	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		logger.Errorf("[Play] Failed to get queue: %v", err)
		sendLeavingMessage(session, guildID, "error")
		stopInternal(guildID)
		return playStop
	}

	if q == nil || len(q.Songs) == 0 {
		logger.Debugf("[Play] Queue is empty for guild: %s", guildID)
		sendLeavingMessage(session, guildID, "empty")
		// Cleanup - call stopInternal directly to avoid deadlock
		// (we're already inside the command processor, so using Stop() would cause timeout)
		if err := stopInternal(guildID); err != nil {
			logger.Errorf("[Play] Failed to cleanup: %v", err)
		}
		return playStop
	}

	player := GetPlayer(guildID)
	song := q.Songs[0]

	// Initialize/update player volume from database
	player.mu.Lock()
	player.Volume = float64(q.Volume) / 100.0
	// Reset stop channel for new playback cycle (close-based broadcast requires fresh channel)
	player.StopChan = make(chan struct{})
	player.mu.Unlock()
	logger.Debugf("[Play] Set initial volume to %.0f%% (%.2f) for guild: %s", q.Volume, player.Volume, guildID)

	// Ensure voice connection (reconnect if dead)
	needsReconnect := false
	if player.VoiceConn == nil {
		needsReconnect = true
	} else {
		// Check if existing connection is dead
		select {
		case <-player.VoiceConn.Dead:
			logger.Warnf("[Play] Detected dead voice connection, will reconnect for guild: %s", guildID)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			player.VoiceConn.Disconnect(ctx)
			cancel()
			player.VoiceConn = nil
			needsReconnect = true
		default:
			// Connection is alive
		}
	}

	if needsReconnect {
		vc, err := JoinVoice(session, guildID, q.VoiceChannelID)
		if err != nil {
			logger.Errorf("[Play] Failed to join voice: %v", err)
			return playStop
		}
		player.VoiceConn = vc
		logger.Infof("[Play] Voice connection established for guild: %s", guildID)
	}

	player.mu.Lock()
	player.Loading = true
	player.Playing = false
	player.Paused = false // Clear paused state
	player.mu.Unlock()

	// Update database states
	if err := queue.SetPaused(guildID, false); err != nil {
		logger.Errorf("[Play] Failed to clear paused state: %v", err)
	}
	if err := queue.SetLoading(guildID, true); err != nil {
		logger.Errorf("[Play] Failed to set loading state: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[Play] Failed to set playing state: %v", err)
	}

	logger.Infof("[Play] Starting playback: %s", song.Title)

	// Get voice channel bitrate for optimal audio quality
	voiceChannelBitrate := 0
	if q.VoiceChannelID != "" {
		channel, err := session.Channel(q.VoiceChannelID)
		if err == nil && channel != nil {
			voiceChannelBitrate = channel.Bitrate
			logger.Debugf("[Play] Voice channel bitrate: %d bps (%d kbps)", voiceChannelBitrate, voiceChannelBitrate/1000)
		} else {
			logger.Warnf("[Play] Could not get voice channel info for bitrate: %v", err)
		}
	}

	// Get stream URL (check pre-cache first, then fetch from YouTube)
	song.SetState(queue.SongStateLoading)
	var streamURL string
	var streamErr error
	if cached := GetCachedStreamURL(guildID, song.ID); cached != "" {
		streamURL = cached
		logger.Infof("[Play] Using pre-cached stream URL for: %s", song.Title)
	} else {
		streamURL, streamErr = youtube.GetStreamURL(song.URL, q.SponsorBlock, voiceChannelBitrate)
	}
	if streamErr != nil {
		logger.Errorf("[Play] Failed to get stream URL: %v", streamErr)

		// Check if we were stopped while loading (e.g., skip command)
		select {
		case <-player.StopChan:
			logger.Debugf("[Play] Stop signal received during stream URL fetch, stopping: %s", song.Title)
			return playStop
		default:
		}

		shouldRetry := handlePlaybackError(session, guildID, song, streamErr)
		if shouldRetry {
			// Drain any stale stop signals before retrying
			select {
			case <-player.StopChan:
				logger.Debugf("[Play] Drained stale stop signal before retry for: %s", song.Title)
				return playStop
			default:
			}
			time.Sleep(2 * time.Second)
			return playContinue // Retry the same song
		}
		// Remove failed song and try next
		if err := queue.RemoveFirstSong(guildID); err != nil {
			logger.Errorf("[Play] Failed to remove failed song: %v", err)
		}
		return playContinue // Try next song
	}

	// Check if song was removed while loading (e.g., by skip command)
	qRecheck, err := queue.GetQueue(guildID, false)
	if err != nil || qRecheck == nil || len(qRecheck.Songs) == 0 {
		logger.Debugf("[Play] Queue empty after loading, song was likely skipped: %s", song.Title)
		return playStop
	}
	// Verify this is still the same song
	if qRecheck.Songs[0].ID != song.ID {
		logger.Debugf("[Play] Song changed while loading (was: %s, now: %s), restarting", song.Title, qRecheck.Songs[0].Title)
		return playContinue // Play the new song
	}

	// Update loading message if it exists (stored globally by guild ID)
	loadingMsg := GetLoadingMessage(guildID)
	if loadingMsg != nil {
		nowPlayingEmbed := messages.CreateSongEmbed(
			messages.ColorSuccess,
			messages.T().Player.PlaybackStarted,
			"",
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)

		_, err := session.ChannelMessageEditEmbed(loadingMsg.ChannelID, loadingMsg.ID, nowPlayingEmbed)
		if err != nil {
			logger.Debugf("[Play] Failed to update loading message: %v", err)
		}

		// Clean up loading message
		DeleteLoadingMessage(guildID)
	} else if q.ShowStartedTrack {
		// Send "Now Playing" message if enabled and no loading message to update
		embed := messages.CreateSongEmbed(
			messages.ColorSuccess,
			messages.T().Player.NowPlaying,
			"",
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)
		session.ChannelMessageSendEmbed(q.TextChannelID, embed)
	}

	// Update reconnect message if resuming after a stream stall
	if reconnectMsg := getReconnectMessage(guildID); reconnectMsg != nil {
		reconnectedEmbed := messages.CreateSongEmbed(
			messages.ColorSuccess,
			messages.T().Player.StreamReconnectedTitle,
			messages.T().Player.StreamReconnectedDesc,
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)
		session.ChannelMessageEditEmbed(reconnectMsg.ChannelID, reconnectMsg.ID, reconnectedEmbed)
		deleteReconnectMessage(guildID)
	}

	// Create audio stream (loop to support restart on normalization toggle)
	seekTime := song.SeekTime
	normalization := q.Normalization
	for {
		logger.Debugf("[Play] Calling playAudio for: %s (seekTime: %d, volume: %g, normalization: %v)", song.Title, seekTime, q.Volume, normalization)
		err := playAudio(player, song, streamURL, seekTime, q.Volume, normalization)
		if err == nil {
			break
		}

		// Check if it was a normalization toggle restart
		player.mu.Lock()
		toggling := player.TogglingNorm
		if toggling {
			player.TogglingNorm = false
			// Calculate current position for seamless restart
			seekTime = int(time.Since(player.PlaybackStart).Milliseconds())
			player.StopChan = make(chan struct{})
		}
		player.mu.Unlock()

		if toggling {
			// Re-read normalization from DB
			newNorm, err := queue.GetNormalization(guildID)
			if err != nil {
				logger.Warnf("[Play] Failed to get normalization state, using previous: %v", err)
			} else {
				normalization = newNorm
			}
			logger.Infof("[Play] Restarting FFmpeg for normalization toggle at %dms: %s", seekTime, song.Title)
			continue
		}

		// Check if playback was stopped by user (skip/stop command)
		if err.Error() == "playback stopped by user" {
			logger.Debugf("[Play] Playback stopped by user for: %s", song.Title)
			// Don't remove song - skip/stop command already handled it
			return playStop
		}

		// Calculate current playback position for crash recovery
		// PlaybackStart is adjusted to account for initial seek, so time.Since gives absolute position
		player.mu.Lock()
		currentPosition := int(time.Since(player.PlaybackStart).Milliseconds())
		player.mu.Unlock()

		// Only update seek time if we made progress (avoid resetting to 0 on immediate failures)
		if currentPosition > song.SeekTime+1000 { // At least 1 second of progress
			song.SeekTime = currentPosition
			logger.Infof("[Play] Crash recovery: will resume from position %dms for: %s", currentPosition, song.Title)
			// Update seek time in database
			if err := queue.UpdateSongSeekTime(guildID, song.ID, currentPosition); err != nil {
				logger.Warnf("[Play] Failed to update seek time in database: %v", err)
			}
		}

		// Check if it's a stream stall - notify user about reconnection
		isStreamStall := strings.Contains(err.Error(), "stream stalled")
		if isStreamStall {
			sendReconnectMessage(session, guildID, song)
		}

		// Check if it's a voice connection error - need to clear dead connection
		isVoiceError := strings.Contains(err.Error(), "voice connection")
		if isVoiceError {
			logger.Warnf("[Play] Voice connection error detected, clearing dead connection for guild: %s", guildID)
			player.mu.Lock()
			if player.VoiceConn != nil {
				// Disconnect dead connection
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				player.VoiceConn.Disconnect(ctx)
				cancel()
				player.VoiceConn = nil
				player.VoiceChannelID = ""
			}
			player.mu.Unlock()
		}

		logger.Errorf("[Play] Playback error: %v", err)
		shouldRetry := handlePlaybackError(session, guildID, song, err)
		if shouldRetry {
			// Drain any stale stop signals before retrying
			select {
			case <-player.StopChan:
				logger.Debugf("[Play] Drained stale stop signal before retry for: %s", song.Title)
				return playStop
			default:
			}
			// For voice errors, wait a bit longer to allow reconnection
			if isVoiceError {
				logger.Infof("[Play] Waiting 3 seconds before reconnecting voice for guild: %s", guildID)
				time.Sleep(3 * time.Second)
			} else {
				time.Sleep(2 * time.Second)
			}
			return playContinue // Retry from saved position (will auto-reconnect voice)
		}
		// Remove failed song and try next
		if err := queue.RemoveFirstSong(guildID); err != nil {
			logger.Errorf("[Play] Failed to remove failed song: %v", err)
		}
		return playContinue // Try next song
	} // end for (playAudio restart loop)
	logger.Debugf("[Play] playAudio completed successfully for: %s", song.Title)

	// Song finished successfully
	player.mu.Lock()
	player.Playing = false
	player.mu.Unlock()

	// Clear retry tracking
	song.ResetRetry()
	song.SetState(queue.SongStateCompleted)
	clearRetryCount(guildID, song.URL)

	// Update database state
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[Play] Failed to clear playing state after song finish: %v", err)
	}

	// Reload queue to get current repeat setting
	q, err = queue.GetQueue(guildID, false)
	if err != nil {
		logger.Errorf("[Play] Failed to reload queue for repeat check: %v", err)
	}

	// Store song info for repeat mode before removing
	repeatMode := queue.RepeatOff
	if q != nil {
		repeatMode = q.RepeatMode
	}
	shouldRepeat := repeatMode != queue.RepeatOff && !song.IsLive
	var repeatSong *queue.Song
	if shouldRepeat {
		repeatSong = &queue.Song{
			URL:            song.URL,
			Title:          song.Title,
			Duration:       song.Duration,
			Thumbnail:      song.Thumbnail,
			Uploader:       song.Uploader,
			RequestedByID:  song.RequestedByID,
			RequestedByTag: song.RequestedByTag,
			IsLive:         song.IsLive,
		}
	} else {
		logger.Debugf("[Play] Repeat check: q=%v, repeatMode=%d, song.IsLive=%v", q != nil, repeatMode, song.IsLive)
	}

	// Remove finished song first (before re-adding for repeat to avoid duplicate check)
	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[Play] Failed to remove finished song: %v", err)
	}

	// Handle repeat mode - add song back after removing
	if shouldRepeat {
		if repeatMode == queue.RepeatSingle {
			// Single: re-add to beginning so it plays immediately next
			logger.Infof("[Play] Single repeat, re-adding song to front: %s", repeatSong.Title)
			if err := queue.AddSong(guildID, repeatSong, 0); err != nil {
				logger.Errorf("[Play] Failed to re-add song for single repeat: %v", err)
			}
		} else {
			// All: re-add to end of queue
			logger.Infof("[Play] Queue repeat, re-adding song to end: %s", repeatSong.Title)
			if err := queue.AddSong(guildID, repeatSong, -1); err != nil {
				logger.Errorf("[Play] Failed to re-add song for queue repeat: %v", err)
			}
		}
	}

	// Start pre-caching next song
	go PreCacheNext(guildID, voiceChannelBitrate)

	// Continue to next song in the loop
	return playContinue
}

// playAudio streams audio to Discord
func playAudio(player *GuildPlayer, song *queue.Song, streamURL string, seekTime int, volume float64, normalization bool) error {
	logger.Debugf("[playAudio] Entered function for guild: %s", player.GuildID)

	// Capture stop channel locally so goroutines reference this specific channel
	// even if player.StopChan is later reset for the next playback cycle
	stopCh := player.StopChan

	// Signal PlaybackDone when this function exits (for skip/stop to wait on)
	defer func() {
		select {
		case player.PlaybackDone <- struct{}{}:
			logger.Debugf("[playAudio] Signaled PlaybackDone for guild: %s", player.GuildID)
		default:
			// Channel full, drain it first then signal
			select {
			case <-player.PlaybackDone:
			default:
			}
			select {
			case player.PlaybackDone <- struct{}{}:
			default:
			}
		}
	}()

	player.mu.Lock()
	player.Playing = true
	player.Loading = false
	player.PlaybackStart = time.Now().Add(-time.Duration(seekTime) * time.Millisecond)
	guildID := player.GuildID
	player.mu.Unlock()

	// Mark song as playing
	song.StartPlayback()

	// Update database states
	if err := queue.SetPlaying(guildID, true); err != nil {
		logger.Errorf("[playAudio] Failed to set playing state: %v", err)
	}
	if err := queue.SetLoading(guildID, false); err != nil {
		logger.Errorf("[playAudio] Failed to set loading state: %v", err)
	}

	logger.Debugf("[playAudio] Set playing state for guild: %s", guildID)

	// Call callback when song starts (clears skip votes) - only on first attempt, not retries
	playbackRetriesMu.Lock()
	retries := playbackRetries[retryKey(guildID, song.URL)]
	playbackRetriesMu.Unlock()
	if retries == 0 {
		callOnSongStart(guildID)
		logger.Debugf("[playAudio] Called onSongStart callback for guild: %s", guildID)
	} else {
		logger.Debugf("[playAudio] Skipping onSongStart callback (retry %d) for guild: %s", retries, guildID)
	}

	// Build FFmpeg command
	logger.Debugf("[playAudio] Building FFmpeg command for guild: %s", guildID)
	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
	}

	// Add seek if needed
	if seekTime > 0 {
		seekSeconds := float64(seekTime) / 1000.0
		args = append(args, "-ss", fmt.Sprintf("%.3f", seekSeconds))
	}

	// Build FFmpeg args — volume is handled in Go for live adjustment
	args = append(args, "-i", streamURL)

	if normalization {
		args = append(args, "-af", "dynaudnorm=framelen=500:gausssize=31:peak=0.95")
	}

	args = append(args,
		"-f", "s16le", // Raw PCM 16-bit little-endian
		"-ar", "48000", // 48kHz sample rate
		"-ac", "2", // Stereo (2 channels)
		"pipe:1",
	)

	logger.Debugf("[playAudio] Creating FFmpeg process for guild: %s", guildID)
	// Create FFmpeg process
	ffmpeg := exec.Command("ffmpeg", args...)
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	logger.Debugf("[playAudio] Starting FFmpeg for guild: %s", guildID)
	if err := ffmpeg.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	logger.Debugf("[playAudio] FFmpeg started, setting voice speaking state for guild: %s", guildID)
	// Start speaking
	logger.Debugf("[playAudio] About to call Speaking(true) for guild: %s", guildID)
	if player.VoiceConn == nil {
		ffmpeg.Process.Kill()
		return fmt.Errorf("voice connection is nil")
	}
	player.VoiceConn.Speaking(true)
	logger.Debugf("[playAudio] Speaking(true) completed for guild: %s", guildID)
	defer func() {
		if player.VoiceConn != nil {
			player.VoiceConn.Speaking(false)
		}
	}()

	// Create Opus encoder (uses native libopus if available, WASM fallback otherwise)
	logger.Debugf("[playAudio] Creating Opus encoder (%s) for guild: %s", GetEncoderType(), guildID)
	opusEncoder, err := NewOpusEncoder(frameRate, channels)
	if err != nil {
		ffmpeg.Process.Kill()
		return fmt.Errorf("failed to create opus encoder: %w", err)
	}

	// Set bitrate for better stability (64kbps stereo)
	if err := opusEncoder.SetBitrate(64000); err != nil {
		logger.Warnf("[playAudio] Failed to set opus bitrate: %v", err)
	}

	// Buffered pipeline: producer buffers raw PCM, consumer applies volume + encodes + sends
	// Volume is applied at send time so changes take effect instantly
	const pcmBufSize = 1500 // ~30 seconds of buffered PCM frames
	pcmChan := make(chan []int16, pcmBufSize)
	encodeErr := make(chan error, 1)

	// Producer: read PCM from FFmpeg and buffer raw samples (no volume/encoding)
	go func() {
		defer close(pcmChan)

		frameCount := 0
		var slowReads int
		var bufferFullCount int

		for {
			if frameCount == 0 {
				logger.Debugf("[playAudio] Reading first PCM frame for guild: %s", guildID)
			}

			pcmBuf := make([]int16, frameSize*channels)

			// Read PCM with stall detection
			readStart := time.Now()
			readErr := make(chan error, 1)
			go func() {
				readErr <- binary.Read(stdout, binary.LittleEndian, &pcmBuf)
			}()

			stallTimer := time.NewTimer(stallTimeout)
			var err error
			select {
			case err = <-readErr:
				stallTimer.Stop()
			case <-stallTimer.C:
				logger.Warnf("[playAudio] Stream stalled: no data for %v after %d frames, guild: %s", stallTimeout, frameCount, guildID)
				ffmpeg.Process.Kill()
				encodeErr <- fmt.Errorf("stream stalled: no data received for %v (after %d frames)", stallTimeout, frameCount)
				return
			case <-stopCh:
				stallTimer.Stop()
				ffmpeg.Process.Kill()
				encodeErr <- fmt.Errorf("playback stopped by user")
				return
			}

			readDuration := time.Since(readStart)
			if readDuration > 100*time.Millisecond {
				slowReads++
				logger.Debugf("[playAudio] Slow FFmpeg read: %v (frame %d, slow reads: %d) guild: %s", readDuration, frameCount, slowReads, guildID)
			}

			if err == io.EOF || err == io.ErrUnexpectedEOF {
				logger.Debugf("[playAudio] Song finished after %d frames for guild: %s", frameCount, guildID)
				ffmpeg.Wait()
				return
			}
			if err != nil {
				logger.Errorf("[playAudio] PCM read error after %d frames for guild: %s: %v", frameCount, guildID, err)
				ffmpeg.Process.Kill()
				encodeErr <- fmt.Errorf("pcm read error: %w", err)
				return
			}

			if frameCount > 0 && frameCount%500 == 0 {
				bufLevel := len(pcmChan)
				logger.Debugf("[playAudio] Buffer: %d/%d frames (%.1fs) | slow reads: %d | buffer full blocks: %d | guild: %s",
					bufLevel, pcmBufSize, float64(bufLevel)*0.02, slowReads, bufferFullCount, guildID)
			}

			select {
			case pcmChan <- pcmBuf:
			case <-stopCh:
				ffmpeg.Process.Kill()
				encodeErr <- fmt.Errorf("playback stopped by user")
				return
			}

			if len(pcmChan) == pcmBufSize {
				bufferFullCount++
			}

			frameCount++
		}
	}()

	// Consumer: apply volume, encode to Opus, send to Discord
	sentFrames := 0
	var consumerSlowSends int
	volumeBuf := make([]int16, frameSize*channels)
	for {
		select {
		case pcmData, ok := <-pcmChan:
			if !ok {
				select {
				case err := <-encodeErr:
					return err
				default:
					if sentFrames == 0 {
						return fmt.Errorf("playback completed with no audio frames sent")
					}
					return nil
				}
			}

			if sentFrames == 0 {
				logger.Debugf("[playAudio] Sending first Opus frame to Discord for guild: %s", guildID)
			}

			bufLevel := len(pcmChan)
			if sentFrames > 0 && sentFrames%500 == 0 {
				logger.Debugf("[playAudio] Consumer: sent %d frames, buffer: %d/%d (%.1fs ahead) | slow sends: %d | guild: %s",
					sentFrames, bufLevel, pcmBufSize, float64(bufLevel)*0.02, consumerSlowSends, guildID)
			}
			if bufLevel == 0 && sentFrames > 50 {
				logger.Debugf("[playAudio] Buffer underrun: Consumer drained buffer at frame %d, guild: %s", sentFrames, guildID)
			}

			player.mu.Lock()
			volumeFactor := player.Volume
			player.mu.Unlock()

			copy(volumeBuf, pcmData)
			for i := 0; i < len(volumeBuf); i++ {
				sample := float64(volumeBuf[i]) * volumeFactor
				if sample > 32767 {
					volumeBuf[i] = 32767
				} else if sample < -32768 {
					volumeBuf[i] = -32768
				} else {
					volumeBuf[i] = int16(sample)
				}
			}

			opusBuffer := make([]byte, 1500)
			opusLen, err := opusEncoder.Encode(volumeBuf, opusBuffer)
			if err != nil {
				logger.Errorf("[playAudio] Opus encoding error: %v", err)
				sentFrames++
				continue
			}
			opusData := opusBuffer[:opusLen]

			sendStart := time.Now()
			select {
			case player.VoiceConn.OpusSend <- opusData:
			case <-player.VoiceConn.Dead:
				logger.Errorf("[playAudio] Voice connection died for guild: %s, error: %v", guildID, player.VoiceConn.Err)
				ffmpeg.Process.Kill()
				return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err)
			case <-stopCh:
				ffmpeg.Process.Kill()
				return fmt.Errorf("playback stopped by user")
			}
			sendDuration := time.Since(sendStart)
			if sendDuration > 50*time.Millisecond {
				consumerSlowSends++
				logger.Warnf("[playAudio] Slow Discord send: %v (frame %d, slow sends: %d) guild: %s", sendDuration, sentFrames, consumerSlowSends, guildID)
			}

			sentFrames++
			if sentFrames == 1 {
				logger.Debugf("[playAudio] First Opus frame sent successfully for guild: %s", guildID)
			}

		case <-player.VoiceConn.Dead:
			logger.Errorf("[playAudio] Voice connection died for guild: %s, error: %v", guildID, player.VoiceConn.Err)
			ffmpeg.Process.Kill()
			return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err)

		case <-stopCh:
			ffmpeg.Process.Kill()
			return fmt.Errorf("playback stopped by user")
		}
	}
}

// isDefinitivePlaybackError checks if an error indicates the video is permanently unavailable
func isDefinitivePlaybackError(errMsg string) bool {
	errorLower := strings.ToLower(errMsg)
	definitivePatterns := []string{
		"video unavailable",
		"not available",
		"private video",
		"deleted video",
		"age-restricted",
		"age restricted",
		"not available in your country",
		"geo",
		"members-only",
		"members only",
		"premium",
		"copyright",
		"blocked",
		"removed by the uploader",
		"account associated with this video has been terminated",
	}
	for _, pattern := range definitivePatterns {
		if strings.Contains(errorLower, pattern) {
			return true
		}
	}
	return false
}


// sendReconnectMessage notifies the user that the stream stalled and we're reconnecting
func sendReconnectMessage(session *discordgo.Session, guildID string, song *queue.Song) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		return
	}

	embed := messages.CreateSongEmbed(
		messages.ColorWarning,
		messages.T().Player.StreamReconnectingTitle,
		messages.T().Player.StreamReconnectingDesc,
		song.Title,
		song.URL,
		song.Uploader,
		song.Duration,
		song.RequestedByTag,
		song.Thumbnail,
	)
	msg, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed)
	if err == nil && msg != nil {
		setReconnectMessage(guildID, msg)
	}
}

// sendSongErrorMessage sends an error embed to the guild's text channel
func sendSongErrorMessage(session *discordgo.Session, guildID string, song *queue.Song, reason string) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		logger.Warnf("[Play] Cannot send error message - no text channel for guild: %s", guildID)
		return
	}

	embed := messages.CreateSongEmbed(
		messages.ColorError,
		messages.T().Player.PlaybackFailedTitle,
		reason,
		song.Title,
		song.URL,
		song.Uploader,
		song.Duration,
		song.RequestedByTag,
		song.Thumbnail,
	)
	session.ChannelMessageSendEmbed(q.TextChannelID, embed)
}

// retryKey returns the map key for tracking retries
func retryKey(guildID, songURL string) string {
	return guildID + ":" + songURL
}

// clearRetryCount removes the retry counter for a guild+song
func clearRetryCount(guildID, songURL string) {
	playbackRetriesMu.Lock()
	delete(playbackRetries, retryKey(guildID, songURL))
	playbackRetriesMu.Unlock()
}

// clearRetryCountsForGuild removes all retry counters for a guild
func clearRetryCountsForGuild(guildID string) {
	prefix := guildID + ":"
	playbackRetriesMu.Lock()
	for key := range playbackRetries {
		if strings.HasPrefix(key, prefix) {
			delete(playbackRetries, key)
		}
	}
	playbackRetriesMu.Unlock()
}

// handlePlaybackError handles playback errors and determines if we should retry or skip
// Returns true if should retry, false if should skip to next song
func handlePlaybackError(session *discordgo.Session, guildID string, song *queue.Song, err error) bool {
	errMsg := err.Error()

	// Check for definitive errors first — no point retrying these
	if isDefinitivePlaybackError(errMsg) {
		reason := errMsg
		logger.Warnf("[Play] Definitive error for song %s in guild %s: %s", song.Title, guildID, reason)
		song.SetState(queue.SongStateFailed)
		sendSongErrorMessage(session, guildID, song, reason)
		clearRetryCount(guildID, song.URL)
		return false // Skip immediately
	}

	// Transient error — use map-based retry tracking
	key := retryKey(guildID, song.URL)
	playbackRetriesMu.Lock()
	retries := playbackRetries[key]
	retries++
	playbackRetries[key] = retries
	playbackRetriesMu.Unlock()

	if retries < maxRetries {
		logger.Warnf("[Play] Retrying song (attempt %d/%d) in guild: %s - %s", retries, maxRetries, guildID, song.Title)
		return true // Retry
	}

	// Max retries exceeded
	song.SetState(queue.SongStateFailed)
	logger.Errorf("[Play] Max retries exceeded for song %s in guild: %s", song.Title, guildID)

	// If there's a reconnect message, edit it to show failure instead of sending a separate error
	if reconnectMsg := getReconnectMessage(guildID); reconnectMsg != nil {
		failedEmbed := messages.CreateSongEmbed(
			messages.ColorError,
			messages.T().Player.StreamReconnectFailedTitle,
			messages.T().Player.StreamReconnectFailedDesc,
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)
		session.ChannelMessageEditEmbed(reconnectMsg.ChannelID, reconnectMsg.ID, failedEmbed)
		deleteReconnectMessage(guildID)
	} else {
		sendSongErrorMessage(session, guildID, song, messages.T().Player.MaxRetriesSkipping)
	}

	clearRetryCount(guildID, song.URL)
	return false // Skip to next song
}

// Pause immediately pauses playback (bypasses command queue for immediate effect)
func Pause(guildID string) error {
	logger.Debugf("[Pause] Pause called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()

	if !player.Playing {
		player.mu.Unlock()
		return fmt.Errorf("not playing")
	}

	// Calculate current position while we have the lock
	elapsed := time.Since(player.PlaybackStart)
	seekTime := int(elapsed.Milliseconds())

	// Drain any stale PlaybackDone signals first
	select {
	case <-player.PlaybackDone:
	default:
	}

	// Close stop channel to broadcast termination to all goroutines
	select {
	case <-player.StopChan:
		logger.Debugf("[Pause] Stop signal already pending for guild: %s", guildID)
	default:
		close(player.StopChan)
		logger.Debugf("[Pause] Stop signal sent for guild: %s", guildID)
	}

	player.Playing = false
	player.Paused = true
	player.mu.Unlock()

	// Wait for playback to actually terminate (with timeout)
	select {
	case <-player.PlaybackDone:
		logger.Debugf("[Pause] Playback terminated for guild: %s", guildID)
	case <-time.After(5 * time.Second):
		logger.Warnf("[Pause] Timeout waiting for playback to terminate for guild: %s", guildID)
	}

	// Get current song and save seek time
	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		currentSong := q.Songs[0]
		_, err = queue.SaveSeekTime(guildID, currentSong.ID, seekTime)
		if err != nil {
			logger.Errorf("[Pause] Failed to save seek time: %v", err)
		}
	}

	// Update database states
	if err := queue.SetPaused(guildID, true); err != nil {
		logger.Errorf("[Pause] Failed to set paused state in database: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[Pause] Failed to clear playing state in database: %v", err)
	}

	// Disconnect from voice
	player.mu.Lock()
	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
		player.VoiceConn = nil
		player.VoiceChannelID = ""
	}
	player.mu.Unlock()

	logger.Infof("[Pause] Paused at %dms for guild: %s", seekTime, guildID)
	return nil
}

// pauseInternal is the actual pause implementation (called by command processor)
func pauseInternal(guildID string) error {
	player := GetPlayer(guildID)
	player.mu.Lock()

	if !player.Playing {
		player.mu.Unlock()
		return fmt.Errorf("not playing")
	}

	// Calculate current position
	elapsed := time.Since(player.PlaybackStart)
	seekTime := int(elapsed.Milliseconds())

	// Close stop channel to broadcast termination to all goroutines
	select {
	case <-player.StopChan:
		logger.Debugf("[pauseInternal] Stop signal already pending for guild: %s", guildID)
	default:
		close(player.StopChan)
		logger.Debugf("[pauseInternal] Stop signal sent for guild: %s", guildID)
	}
	player.Playing = false
	player.Paused = true

	// Disconnect from voice
	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
		player.VoiceConn = nil
		player.VoiceChannelID = ""
	}
	player.mu.Unlock()

	// Get current song and save seek time
	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		currentSong := q.Songs[0]
		_, err = queue.SaveSeekTime(guildID, currentSong.ID, seekTime)
		if err != nil {
			logger.Errorf("[pauseInternal] Failed to save seek time: %v", err)
		}
	}

	// Update database states
	if err := queue.SetPaused(guildID, true); err != nil {
		logger.Errorf("[pauseInternal] Failed to set paused state in database: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[pauseInternal] Failed to clear playing state in database: %v", err)
	}

	logger.Infof("[pauseInternal] Paused at %dms for guild: %s", seekTime, guildID)
	return nil
}

// RestartForNormalization restarts the FFmpeg pipeline to apply normalization changes.
// Unlike pause, this keeps the voice connection alive — only kills FFmpeg.
func RestartForNormalization(guildID string) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if !player.Playing {
		return
	}

	player.TogglingNorm = true
	select {
	case <-player.StopChan:
		// Already closed
	default:
		close(player.StopChan)
	}
	logger.Infof("[RestartForNormalization] Signaled FFmpeg restart for guild: %s", guildID)
}

// Resume queues a resume command (uses channels)
func Resume(session *discordgo.Session, guildID string) error {
	player := GetPlayer(guildID)

	done := make(chan error, 1)
	cmd := PlayerCommand{
		Type:    "resume",
		Session: session,
		GuildID: guildID,
		Done:    done,
	}

	// Send command to queue (with panic recovery for closed channel)
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[Resume] Recovered from panic (channel likely closed) for guild %s: %v", guildID, r)
		}
	}()

	select {
	case player.CommandChan <- cmd:
		// Command queued, wait for result with timeout
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			// Timeout occurred - verify if operation actually succeeded
			player.mu.Lock()
			playing := player.Playing
			paused := player.Paused
			player.mu.Unlock()

			// If playing and not paused, the resume succeeded
			if playing && !paused {
				logger.Infof("[Resume] Command completed successfully after timeout for guild %s", guildID)
				return nil
			}

			logger.Warnf("[Resume] Command timeout - operation failed for guild %s", guildID)
			return fmt.Errorf("resume command timeout")
		}
	default:
		logger.Warnf("[Resume] Command queue full for guild %s", guildID)
		return fmt.Errorf("command queue full, please try again")
	}
}

// resumeInternal is the actual resume implementation (called by command processor)
func resumeInternal(session *discordgo.Session, guildID string) error {
	// Get queue to check if current song is a live stream
	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		song := q.Songs[0]

		// If it's a live stream, check if it's still active
		if song.IsLive {
			active, err := youtube.IsLiveStreamActive(song.URL)
			if err != nil || !active {
				logger.Warnf("[Resume] Live stream ended or unavailable: %s", song.Title)

				// Remove the ended live stream
				if err := queue.RemoveFirstSong(guildID); err != nil {
					logger.Errorf("[Resume] Failed to remove ended live stream: %v", err)
				}

				// Try to play next song
				player := GetPlayer(guildID)
				player.mu.Lock()
				player.Paused = false
				player.mu.Unlock()

				logger.Infof("[Resume] Skipping to next song after ended live stream")
				return playInternal(session, guildID)
			}
		}
	}

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.Paused = false
	player.mu.Unlock()

	// Cancel auto-disconnect timer since we're resuming
	ClearAutoPauseTimer(guildID)

	logger.Infof("[Resume] Resuming playback for guild: %s", guildID)
	return playInternal(session, guildID)
}

// Skip immediately stops current playback and removes the current song
// This bypasses the command processor to allow immediate interruption
func Skip(session *discordgo.Session, guildID string) error {
	logger.Debugf("[Skip] Skip called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {
		// Drain any stale PlaybackDone signals first
		select {
		case <-player.PlaybackDone:
		default:
		}

		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			logger.Debugf("[Skip] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[Skip] Stop signal sent for guild: %s", guildID)
		}
	}
	player.mu.Unlock()

	// Wait for playback to actually terminate (with timeout)
	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[Skip] Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("[Skip] Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		// Mark playback as stopped so Stop() won't wait again
		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		player.mu.Unlock()
	}

	// Clear retry count and reset state for current song
	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		q.Songs[0].ResetRetry()
		logger.Debugf("[Skip] Removing song: %s", q.Songs[0].Title)
	}

	// Remove current song
	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[Skip] Failed to remove song: %v", err)
		return fmt.Errorf("failed to remove song: %w", err)
	}

	logger.Infof("[Skip] Skipped song for guild: %s", guildID)

	// Check if queue is now empty - if so, return ErrQueueEmpty so caller can send message
	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		logger.Infof("[Skip] Queue is empty after skip for guild: %s", guildID)
		return ErrQueueEmpty
	}

	// Start playing next song asynchronously
	// Check if another play operation is already in progress to avoid lock contention
	go func() {
		player := GetPlayer(guildID)
		player.mu.Lock()
		alreadyActive := player.Playing || player.Loading
		player.mu.Unlock()

		if alreadyActive {
			logger.Debugf("[Skip] Play operation already in progress for guild %s, skipping redundant play call", guildID)
			return
		}

		if err := playInternal(session, guildID); err != nil {
			// Only log as error if it's not a lock timeout (which is expected during rapid skips)
			if err.Error() == "play lock timeout" {
				logger.Debugf("[Skip] Play lock timeout for guild %s (expected during rapid skips)", guildID)
			} else {
				logger.Errorf("[Skip] Failed to play next song: %v", err)
			}
		}
	}()

	return nil
}

// SkipTo stops current playback and starts playing the current first song in queue.
// Unlike Skip, it does NOT remove any songs — the caller is responsible for queue manipulation.
func SkipTo(session *discordgo.Session, guildID string) error {
	logger.Debugf("[SkipTo] Called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {
		// Drain any stale PlaybackDone signals first
		select {
		case <-player.PlaybackDone:
		default:
		}

		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			logger.Debugf("[SkipTo] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[SkipTo] Stop signal sent for guild: %s", guildID)
		}
	}
	player.mu.Unlock()

	// Wait for playback to actually terminate (with timeout)
	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[SkipTo] Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("[SkipTo] Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		player.mu.Unlock()
	}

	logger.Infof("[SkipTo] Starting playback of target song for guild: %s", guildID)

	// Start playing the target song (now at position 0)
	go func() {
		player := GetPlayer(guildID)
		player.mu.Lock()
		alreadyActive := player.Playing || player.Loading
		player.mu.Unlock()

		if alreadyActive {
			logger.Debugf("[SkipTo] Play operation already in progress for guild %s", guildID)
			return
		}

		if err := playInternal(session, guildID); err != nil {
			if err.Error() == "play lock timeout" {
				logger.Debugf("[SkipTo] Play lock timeout for guild %s (expected during rapid skips)", guildID)
			} else {
				logger.Errorf("[SkipTo] Failed to play: %v", err)
			}
		}
	}()

	return nil
}

// skipInternal is the actual skip implementation (called by command processor)
func skipInternal(session *discordgo.Session, guildID string) error {
	logger.Debugf("[skipInternal] Called for guild %s", guildID)
	player := GetPlayer(guildID)
	player.mu.Lock()

	wasPlaying := player.Playing
	if wasPlaying {
		// Drain any stale PlaybackDone signals first
		select {
		case <-player.PlaybackDone:
		default:
		}

		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			logger.Debugf("[Skip] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[Skip] Stop signal sent for guild: %s", guildID)
		}
	}

	player.mu.Unlock()

	// Wait for playback to actually terminate (with short timeout)
	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[Skip] Playback terminated for guild: %s", guildID)
		case <-time.After(2 * time.Second):
			logger.Warnf("[Skip] Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		// Mark playback as stopped so Stop() won't wait again
		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		player.mu.Unlock()
	}

	// Clear retry count and reset state for current song
	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		q.Songs[0].ResetRetry()
		logger.Debugf("[skipInternal] Removing song: %s", q.Songs[0].Title)
	}

	// Remove current song
	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[skipInternal] Failed to remove song: %v", err)
		return fmt.Errorf("failed to remove song: %w", err)
	}

	logger.Infof("[skipInternal] Skipped song for guild: %s", guildID)

	// Check if queue is now empty - if so, return ErrQueueEmpty so caller can send message
	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		logger.Infof("[skipInternal] Queue is empty after skip for guild: %s", guildID)
		return ErrQueueEmpty
	}

	// Play next song asynchronously - skip operation is complete once song is removed
	// Check if another play operation is already in progress to avoid lock contention
	go func() {
		player := GetPlayer(guildID)
		player.mu.Lock()
		alreadyActive := player.Playing || player.Loading
		player.mu.Unlock()

		if alreadyActive {
			logger.Debugf("[skipInternal] Play operation already in progress for guild %s, skipping redundant play call", guildID)
			return
		}

		if err := playInternal(session, guildID); err != nil {
			// Only log as error if it's not a lock timeout (which is expected during rapid skips)
			if err.Error() == "play lock timeout" {
				logger.Debugf("[skipInternal] Play lock timeout for guild %s (expected during rapid skips)", guildID)
			} else {
				logger.Errorf("[skipInternal] Failed to play next song: %v", err)
			}
		}
	}()

	return nil
}

// Stop immediately stops playback and cleans up (bypasses command queue for immediate effect)
func Stop(guildID string) error {
	logger.Debugf("[Stop] Stop called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {
		// Drain any stale PlaybackDone signals first
		select {
		case <-player.PlaybackDone:
		default:
		}

		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			logger.Debugf("[Stop] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[Stop] Stop signal sent for guild: %s", guildID)
		}
	}

	player.Playing = false
	player.Paused = false
	player.Loading = false
	player.mu.Unlock()

	// Wait for playback to actually terminate (with timeout)
	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[Stop] Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("[Stop] Timeout waiting for playback to terminate for guild: %s", guildID)
		}
	}

	// Disconnect from voice
	if err := LeaveVoice(guildID); err != nil {
		logger.Errorf("[Stop] Failed to leave voice: %v", err)
	}

	// Clear queue (this will also clear playing/loading/paused states in database)
	if err := queue.DeleteQueue(guildID); err != nil {
		logger.Errorf("[Stop] Failed to delete queue: %v", err)
	}

	// Clear pre-cache
	ClearPreCache(guildID)

	// Delete player
	DeletePlayer(guildID)

	logger.Infof("[Stop] Stopped playback for guild: %s", guildID)
	return nil
}

// stopInternal is the actual stop implementation (called by command processor)
func stopInternal(guildID string) error {
	player := GetPlayer(guildID)
	player.mu.Lock()

	if player.Playing {
		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			logger.Debugf("[Stop] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[Stop] Stop signal sent for guild: %s", guildID)
		}
	}

	player.Playing = false
	player.Paused = false
	player.Loading = false
	player.mu.Unlock()

	// Disconnect from voice
	if err := LeaveVoice(guildID); err != nil {
		logger.Errorf("[Stop] Failed to leave voice: %v", err)
	}

	// Clear queue (this will also clear playing/loading/paused states in database)
	if err := queue.DeleteQueue(guildID); err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	// Clear pre-cache
	ClearPreCache(guildID)

	DeletePlayer(guildID)
	logger.Infof("[Stop] Stopped playback for guild: %s", guildID)
	return nil
}

// SetVolume sets the volume for a guild
func SetVolume(guildID string, volume float64) error {
	// Check for invalid float values
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

	logger.Infof("[Volume] Set volume to %g%% for guild: %s", volume, guildID)
	return nil
}

// GetCurrentPosition returns the current playback position in milliseconds
func GetCurrentPosition(guildID string) int {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if !player.Playing {
		return 0
	}

	elapsed := time.Since(player.PlaybackStart)
	return int(elapsed.Milliseconds())
}

// FormatDuration formats milliseconds to MM:SS or HH:MM:SS
func FormatDuration(ms int) string {
	seconds := ms / 1000
	minutes := seconds / 60
	hours := minutes / 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds%60)
}

// int16ToByte converts int16 slice to byte slice
func int16ToByte(in []int16) []byte {
	out := make([]byte, len(in)*2)
	for i, v := range in {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

// StopAll stops all active players (for graceful shutdown)
// NOTE: This preserves the database queue so it can be resumed after bot restart
func StopAll() {
	playersMu.RLock()
	guildIDs := make([]string, 0, len(players))
	for guildID := range players {
		guildIDs = append(guildIDs, guildID)
	}
	playersMu.RUnlock()

	// Clean up each player without deleting database queue
	for _, guildID := range guildIDs {
		logger.Debugf("[StopAll] Cleaning up player for guild: %s", guildID)
		if err := cleanupForShutdown(guildID); err != nil {
			logger.Errorf("[StopAll] Failed to cleanup guild %s: %v", guildID, err)
		}
		// Clean up pre-cache for this guild
		ClearPreCache(guildID)
	}

	// Clean up any remaining pre-cache entries
	preCacheStoreMu.Lock()
	for key, cache := range preCacheStore {
		if cache.CancelFunc != nil {
			cache.CancelFunc()
		}
		delete(preCacheStore, key)
	}
	preCacheStoreMu.Unlock()

	logger.Info("[StopAll] All players cleaned up and pre-cache cleared")
}

// cleanupForShutdown cleans up player state without deleting database queue
// This is used during bot shutdown to preserve queues for later resumption
func cleanupForShutdown(guildID string) error {
	logger.Debugf("[CleanupForShutdown] Cleaning up guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {
		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			// Already closed
		default:
			close(player.StopChan)
			logger.Debugf("[CleanupForShutdown] Stop signal sent for guild: %s", guildID)
		}
	}

	player.Playing = false
	player.Paused = false
	player.Loading = false
	player.mu.Unlock()

	// Wait briefly for playback to terminate
	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[CleanupForShutdown] Playback terminated for guild: %s", guildID)
		case <-time.After(2 * time.Second):
			logger.Debugf("[CleanupForShutdown] Timeout waiting for playback for guild: %s", guildID)
		}
	}

	// Disconnect from voice without deleting queue
	if err := LeaveVoice(guildID); err != nil {
		logger.Debugf("[CleanupForShutdown] Failed to leave voice for guild %s: %v", guildID, err)
	}

	// Update database to mark as paused (not playing) so it can be resumed
	if wasPlaying || wasLoading {
		if err := queue.SetPlaying(guildID, false); err != nil {
			logger.Debugf("[CleanupForShutdown] Failed to clear playing state for guild %s: %v", guildID, err)
		}
		if err := queue.SetLoading(guildID, false); err != nil {
			logger.Debugf("[CleanupForShutdown] Failed to clear loading state for guild %s: %v", guildID, err)
		}
	}

	// Delete in-memory player (but keep database queue)
	DeletePlayer(guildID)

	logger.Debugf("[CleanupForShutdown] Cleanup complete for guild: %s (queue preserved)", guildID)
	return nil
}

// sendLeavingMessage sends a message to the text channel when the bot leaves voice
func sendLeavingMessage(session *discordgo.Session, guildID, reason string) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		logger.Debugf("[Leave] Cannot send leaving message: no queue or text channel")
		return
	}

	var embed *discordgo.MessageEmbed

	switch reason {
	case "empty":
		embed = &discordgo.MessageEmbed{
			Description: messages.T().Player.LeavingEmptyDesc,
			Color:       messages.ColorInfo,
			Footer: &discordgo.MessageEmbedFooter{
				Text: messages.T().Player.LeavingEmptyFooter,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	case "error":
		embed = &discordgo.MessageEmbed{
			Description: messages.T().Player.LeavingErrorDesc,
			Color:       messages.ColorError,
			Footer: &discordgo.MessageEmbedFooter{
				Text: messages.T().Player.LeavingErrorFooter,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	default:
		embed = &discordgo.MessageEmbed{
			Description: messages.T().Player.LeavingDefaultDesc,
			Color:       messages.ColorInfo,
			Footer: &discordgo.MessageEmbedFooter{
				Text: reason,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	}

	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
		logger.Debugf("[Leave] Failed to send leaving message: %v", err)
	}
}

// ShutdownWorkerPool closes the global worker pool
func ShutdownWorkerPool() {
	// Import worker package dynamically to avoid circular dependency
	// Worker pool will be closed via worker.GetWorkerPool().Close()
	logger.Info("[Shutdown] Worker pool will be closed by worker package")
}
