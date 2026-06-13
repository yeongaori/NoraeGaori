package player

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/worker"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"

	"github.com/bwmarrin/discordgo"
)

var ErrQueueEmpty = errors.New("queue is empty after skip")

const (
	channels     = 2
	frameRate    = 48000
	frameSize    = 960
	maxRetries   = 3
	lockTimeout  = 30 * time.Second
	stallTimeout = 30 * time.Second
)

type PlayerCommand struct {
	Type    string
	Session *discordgo.Session
	GuildID string
	Done    chan error
}

type GuildPlayer struct {
	GuildID          string
	VoiceConn        *discordgo.VoiceConnection
	VoiceChannelID   string
	Playing          bool
	Paused           bool
	Loading          bool
	TogglingNorm     bool
	Seeking          bool
	Volume           float64
	StopChan         chan struct{}
	PlaybackDone     chan struct{}
	CommandChan      chan PlayerCommand
	QuitChan         chan struct{}
	PlaybackStart    time.Time
	mu               sync.Mutex
	processorRunning bool
	PendingStream    *PendingStream
	FadingOut        bool
	FadingIn         bool
	Ramping          bool
	AutoMixAdvanced  bool
	FadeInNext       bool
	SeekTargetMs     int
	TrimStartMs      int
	TrimEndMs        int
}

type fadeSettings struct {
	fadeIn       bool
	fadeOut      bool
	autoMix      bool
	crossfade    bool
	trimSilence  bool
	fadeInSec    float64
	fadeOutSec   float64
	crossfadeSec float64
	autoMixBeats int
	repeatMode   int
}

var (
	players   = make(map[string]*GuildPlayer)
	playersMu sync.RWMutex

	playLocks   = make(map[string]*sync.Mutex)
	playLocksMu sync.Mutex

	loadingMessages   = make(map[string]*discordgo.Message)
	loadingMessagesMu sync.RWMutex

	reconnectMessages   = make(map[string]*discordgo.Message)
	reconnectMessagesMu sync.RWMutex

	preCacheStore   = make(map[string]*PreCache)
	preCacheStoreMu sync.RWMutex

	playbackRetries   = make(map[string]int)
	playbackRetriesMu sync.Mutex

	onSongStartCallback func(guildID string)
	callbackMu          sync.RWMutex
)

type PreCache struct {
	StreamURL  string
	SongID     int
	Timestamp  time.Time
	CancelFunc context.CancelFunc
	Analysis   *TrackAnalysis
}

func acquirePlayLock(guildID string) *sync.Mutex {
	playLocksMu.Lock()
	defer playLocksMu.Unlock()

	if _, exists := playLocks[guildID]; !exists {
		playLocks[guildID] = &sync.Mutex{}
	}
	return playLocks[guildID]
}

func GetPlayer(guildID string) *GuildPlayer {
	playersMu.Lock()
	defer playersMu.Unlock()

	if player, exists := players[guildID]; exists {

		player.mu.Lock()
		running := player.processorRunning
		if !running {

			logger.Warnf("[GetPlayer] Processor not running for guild %s, restarting", guildID)

			player.CommandChan = make(chan PlayerCommand, 10)
			player.QuitChan = make(chan struct{})

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
		PlaybackDone:     make(chan struct{}, 1),
		CommandChan:      make(chan PlayerCommand, 10),
		QuitChan:         make(chan struct{}),
		processorRunning: true,
	}
	players[guildID] = player

	go player.processCommands()

	return player
}

func SetLoadingMessage(guildID string, msg *discordgo.Message) {
	loadingMessagesMu.Lock()
	defer loadingMessagesMu.Unlock()
	loadingMessages[guildID] = msg
	logger.Debugf("[LoadingMessage] Stored loading message for guild: %s", guildID)
}

func GetLoadingMessage(guildID string) *discordgo.Message {
	loadingMessagesMu.RLock()
	defer loadingMessagesMu.RUnlock()
	return loadingMessages[guildID]
}

func DeleteLoadingMessage(guildID string) {
	loadingMessagesMu.Lock()
	defer loadingMessagesMu.Unlock()
	delete(loadingMessages, guildID)
	logger.Debugf("[LoadingMessage] Deleted loading message for guild: %s", guildID)
}

func sendNowPlayingMessage(session *discordgo.Session, guildID string, song *queue.Song, q *queue.Queue) {
	loadingMsg := GetLoadingMessage(guildID)
	if loadingMsg != nil {
		nowPlayingEmbed := messages.CreateSongEmbed(
			guildID,
			messages.ColorSuccess,
			messages.T(guildID).Player.PlaybackStarted,
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
			logger.Warnf("[Play] Failed to update loading message: %v", err)
			if q.ShowStartedTrack {
				session.ChannelMessageSendEmbed(q.TextChannelID, nowPlayingEmbed)
			}
		}

		DeleteLoadingMessage(guildID)
	} else if q.ShowStartedTrack {
		embed := messages.CreateSongEmbed(
			guildID,
			messages.ColorSuccess,
			messages.T(guildID).Player.NowPlaying,
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

	if reconnectMsg := getReconnectMessage(guildID); reconnectMsg != nil {
		reconnectedEmbed := messages.CreateSongEmbed(
			guildID,
			messages.ColorSuccess,
			messages.T(guildID).Player.StreamReconnectedTitle,
			messages.T(guildID).Player.StreamReconnectedDesc,
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
}

func setReconnectMessage(guildID string, msg *discordgo.Message) {
	reconnectMessagesMu.Lock()
	defer reconnectMessagesMu.Unlock()
	reconnectMessages[guildID] = msg
}

func getReconnectMessage(guildID string) *discordgo.Message {
	reconnectMessagesMu.RLock()
	defer reconnectMessagesMu.RUnlock()
	return reconnectMessages[guildID]
}

func deleteReconnectMessage(guildID string) {
	reconnectMessagesMu.Lock()
	defer reconnectMessagesMu.Unlock()
	delete(reconnectMessages, guildID)
}

func DeletePlayer(guildID string) {
	playersMu.Lock()
	player, exists := players[guildID]
	if !exists {
		playersMu.Unlock()
		return
	}
	delete(players, guildID)
	playersMu.Unlock()

	close(player.QuitChan)

	close(player.CommandChan)

	clearRetryCountsForGuild(guildID)

	logger.Debugf("[DeletePlayer] Stopped command processor for guild: %s", guildID)
}

func SetOnSongStartCallback(callback func(guildID string)) {
	callbackMu.Lock()
	defer callbackMu.Unlock()
	onSongStartCallback = callback
}

func callOnSongStart(guildID string) {
	callbackMu.RLock()
	callback := onSongStartCallback
	callbackMu.RUnlock()

	if callback != nil {
		callback(guildID)
	}
}

func JoinVoice(session *discordgo.Session, guildID, channelID string) (*discordgo.VoiceConnection, error) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if player.VoiceConn != nil && player.VoiceChannelID == channelID {
		return player.VoiceConn, nil
	}

	session.RLock()
	existingVC, exists := session.VoiceConnections[guildID]
	session.RUnlock()
	if exists && existingVC != nil {
		logger.Infof("[Voice] Found stale session voice connection, disconnecting for guild: %s", guildID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		existingVC.Disconnect(ctx)
		cancel()
	}

	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vc, err := session.ChannelVoiceJoin(ctx, guildID, channelID, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if vc.Status == discordgo.VoiceConnectionStatusReady {
				logger.Debugf("[Voice] Voice connection ready for guild: %s", guildID)
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
		logger.Debugf("[Voice] Left voice channel in guild: %s", guildID)
	}

	return nil
}

func (p *GuildPlayer) processCommands() {
	defer func() {

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

				logger.Debugf("[CommandProcessor] CommandChan closed for guild: %s", p.GuildID)
				return
			}

			logger.Debugf("[CommandProcessor] Received %s command for guild: %s", cmd.Type, p.GuildID)

			func() {
				var err error
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("command panic: %v", r)
						logger.Errorf("[CommandProcessor] Command %s panicked for guild %s: %v", cmd.Type, p.GuildID, r)
					}

					logger.Debugf("[CommandProcessor] Command %s completed for guild %s with error: %v", cmd.Type, p.GuildID, err)

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

			logger.Debugf("[CommandProcessor] Quit signal received for guild: %s", p.GuildID)
			return
		}
	}
}

func Play(session *discordgo.Session, guildID string) error {
	player := GetPlayer(guildID)

	cmd := PlayerCommand{
		Type:    "play",
		Session: session,
		GuildID: guildID,
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[Play] Recovered from panic (channel likely closed) for guild %s: %v", guildID, r)
		}
	}()

	select {
	case player.CommandChan <- cmd:

		return nil
	default:

		logger.Warnf("[Play] Command queue full for guild %s", guildID)
		return fmt.Errorf("command queue full, please try again")
	}
}

func playInternal(session *discordgo.Session, guildID string) error {

	lock := acquirePlayLock(guildID)

	lockAcquired := make(chan bool, 1)
	unlockChan := make(chan struct{})

	go func() {
		lock.Lock()
		select {
		case lockAcquired <- true:

			<-unlockChan
			lock.Unlock()
		default:

			lock.Unlock()
		}
	}()

	select {
	case <-lockAcquired:

		defer close(unlockChan)
	case <-time.After(lockTimeout):
		logger.Warnf("[Play] Lock timeout for guild: %s", guildID)
		return fmt.Errorf("play lock timeout")
	}

	logger.Debugf("[Play] Lock acquired for guild: %s", guildID)

	for {
		result := playSingleSong(session, guildID)
		switch result {
		case playContinue:

			continue
		case playStop:

			return nil
		case playError:

			return fmt.Errorf("playback error")
		}
	}
}

type playResult int

const (
	playContinue playResult = iota
	playStop
	playError
)

func playSingleSong(session *discordgo.Session, guildID string) playResult {

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

		if err := stopInternal(guildID); err != nil {
			logger.Errorf("[Play] Failed to cleanup: %v", err)
		}
		return playStop
	}

	player := GetPlayer(guildID)
	song := q.Songs[0]

	player.mu.Lock()
	player.Volume = float64(q.Volume) / 100.0

	player.StopChan = make(chan struct{})
	player.mu.Unlock()
	logger.Debugf("[Play] Set initial volume to %.0f%% (%.2f) for guild: %s", q.Volume, player.Volume, guildID)

	needsReconnect := false
	if player.VoiceConn == nil {
		needsReconnect = true
	} else {

		select {
		case <-player.VoiceConn.Dead:
			logger.Warnf("[Play] Detected dead voice connection, will reconnect for guild: %s", guildID)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			player.VoiceConn.Disconnect(ctx)
			cancel()
			player.VoiceConn = nil
			needsReconnect = true
		default:

		}
	}

	if needsReconnect {
		vc, err := JoinVoice(session, guildID, q.VoiceChannelID)
		if err != nil {
			logger.Errorf("[Play] Failed to join voice: %v", err)
			return playStop
		}
		player.VoiceConn = vc
		logger.Debugf("[Play] Voice connection established for guild: %s", guildID)
	}

	player.mu.Lock()
	player.Loading = true
	player.Playing = false
	player.Paused = false
	player.AutoMixAdvanced = false
	player.mu.Unlock()

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

	song.SetState(queue.SongStateLoading)

	player.mu.Lock()
	hasPending := player.PendingStream != nil && player.PendingStream.SongID == song.ID && song.SeekTime == 0
	player.mu.Unlock()

	var streamURL string
	var streamErr error
	if hasPending {
		logger.Debugf("[Play] Using handed-off stream for: %s", song.Title)
	} else if cached := GetCachedStreamURL(guildID, song.ID); cached != "" {
		streamURL = cached
		logger.Debugf("[Play] Using pre-cached stream URL for: %s", song.Title)
	} else {
		streamURL, streamErr = youtube.GetStreamURL(song.URL, q.SponsorBlock, voiceChannelBitrate)
	}
	if streamErr != nil {
		logger.Errorf("[Play] Failed to get stream URL: %v", streamErr)

		select {
		case <-player.StopChan:
			logger.Debugf("[Play] Stop signal received during stream URL fetch, stopping: %s", song.Title)
			return playStop
		default:
		}

		shouldRetry := handlePlaybackError(session, guildID, song, streamErr)
		if shouldRetry {

			select {
			case <-player.StopChan:
				logger.Debugf("[Play] Drained stale stop signal before retry for: %s", song.Title)
				return playStop
			default:
			}
			time.Sleep(2 * time.Second)
			return playContinue
		}

		if err := queue.RemoveFirstSong(guildID); err != nil {
			logger.Errorf("[Play] Failed to remove failed song: %v", err)
		}
		return playContinue
	}

	qRecheck, err := queue.GetQueue(guildID, false)
	if err != nil || qRecheck == nil || len(qRecheck.Songs) == 0 {
		logger.Debugf("[Play] Queue empty after loading, song was likely skipped: %s", song.Title)
		return playStop
	}

	if qRecheck.Songs[0].ID != song.ID {
		logger.Debugf("[Play] Song changed while loading (was: %s, now: %s), restarting", song.Title, qRecheck.Songs[0].Title)
		return playContinue
	}

	firstFrameCh := make(chan struct{}, 1)
	var firstFrameOnce sync.Once
	closeFirstFrame := func() { firstFrameOnce.Do(func() { close(firstFrameCh) }) }

	go func() {
		for {
			player.mu.Lock()
			stopCh := player.StopChan
			player.mu.Unlock()
			select {
			case <-firstFrameCh:
				if !hasPending {
					sendNowPlayingMessage(session, guildID, song, q)
				}
				return
			case <-stopCh:
				player.mu.Lock()
				restarting := player.Seeking || player.TogglingNorm || player.StopChan != stopCh
				player.mu.Unlock()
				if restarting {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				if lm := GetLoadingMessage(guildID); lm != nil {
					session.ChannelMessageDelete(lm.ChannelID, lm.ID)
					DeleteLoadingMessage(guildID)
				}
				return
			}
		}
	}()

	seekTime := song.SeekTime
	normalization := q.Normalization
	fade := fadeSettingsFromQueue(q)
	announceNext := func(next *queue.Song) {
		nq, err := queue.GetQueue(guildID, false)
		if err != nil || nq == nil {
			return
		}
		sendNowPlayingMessage(session, guildID, next, nq)
	}
	for {
		logger.Debugf("[Play] Calling playAudio for: %s (seekTime: %d, volume: %g, normalization: %v)", song.Title, seekTime, q.Volume, normalization)
		err := playAudio(player, song, streamURL, seekTime, q.Volume, normalization, voiceChannelBitrate, firstFrameCh, fade, announceNext)
		if err == nil {
			break
		}

		player.mu.Lock()
		toggling := player.TogglingNorm
		seeking := player.Seeking
		if toggling {
			player.TogglingNorm = false

			seekTime = int(time.Since(player.PlaybackStart).Milliseconds())
			player.StopChan = make(chan struct{})
		}
		if seeking {
			player.Seeking = false

			seekTime = player.SeekTargetMs
			song.SeekTime = seekTime
			player.FadeInNext = true
			player.StopChan = make(chan struct{})
		}
		player.mu.Unlock()

		if toggling {

			newNorm, err := queue.GetNormalization(guildID)
			if err != nil {
				logger.Warnf("[Play] Failed to get normalization state, using previous: %v", err)
			} else {
				normalization = newNorm
			}
			logger.Debugf("[Play] Restarting FFmpeg for normalization toggle at %dms: %s", seekTime, song.Title)
			continue
		}

		if seeking {
			if vq, err := queue.GetQueue(guildID, false); err == nil && vq != nil {
				player.mu.Lock()
				player.Volume = vq.Volume / 100.0
				player.mu.Unlock()
				fade = fadeSettingsFromQueue(vq)
			}
			logger.Debugf("[Play] Restarting FFmpeg for seek to %dms: %s", seekTime, song.Title)
			continue
		}

		if err.Error() == "playback stopped by user" {
			logger.Debugf("[Play] Playback stopped by user for: %s", song.Title)

			return playStop
		}

		player.mu.Lock()
		currentPosition := int(time.Since(player.PlaybackStart).Milliseconds())
		player.mu.Unlock()

		if currentPosition > song.SeekTime+1000 {
			song.SeekTime = currentPosition
			logger.Infof("[Play] Crash recovery: will resume from position %dms for: %s", currentPosition, song.Title)

			if err := queue.UpdateSongSeekTime(guildID, song.ID, currentPosition); err != nil {
				logger.Warnf("[Play] Failed to update seek time in database: %v", err)
			}
		}

		isStreamStall := strings.Contains(err.Error(), "stream stalled")
		if isStreamStall {
			sendReconnectMessage(session, guildID, song)
		}

		isVoiceError := strings.Contains(err.Error(), "voice connection")
		if isVoiceError {
			logger.Warnf("[Play] Voice connection error detected, clearing dead connection for guild: %s", guildID)
			player.mu.Lock()
			if player.VoiceConn != nil {

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

			select {
			case <-player.StopChan:
				logger.Debugf("[Play] Drained stale stop signal before retry for: %s", song.Title)
				return playStop
			default:
			}

			if isVoiceError {
				logger.Infof("[Play] Waiting 3 seconds before reconnecting voice for guild: %s", guildID)
				time.Sleep(3 * time.Second)
			} else {
				time.Sleep(2 * time.Second)
			}
			closeFirstFrame()
			return playContinue
		}

		if err := queue.RemoveFirstSong(guildID); err != nil {
			logger.Errorf("[Play] Failed to remove failed song: %v", err)
		}
		closeFirstFrame()
		return playContinue
	}
	closeFirstFrame()
	logger.Debugf("[Play] playAudio completed successfully for: %s", song.Title)

	player.mu.Lock()
	player.Playing = false
	advanced := player.AutoMixAdvanced
	player.AutoMixAdvanced = false
	player.mu.Unlock()

	if advanced {
		go PreCacheNext(guildID, voiceChannelBitrate)
		return playContinue
	}

	song.ResetRetry()
	song.SetState(queue.SongStateCompleted)
	clearRetryCount(guildID, song.URL)

	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[Play] Failed to clear playing state after song finish: %v", err)
	}

	q, err = queue.GetQueue(guildID, false)
	if err != nil {
		logger.Errorf("[Play] Failed to reload queue for repeat check: %v", err)
	}

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

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[Play] Failed to remove finished song: %v", err)
	}

	if shouldRepeat {
		if repeatMode == queue.RepeatSingle {

			logger.Debugf("[Play] Single repeat, re-adding song to front: %s", repeatSong.Title)
			if err := queue.AddSong(guildID, repeatSong, 0); err != nil {
				logger.Errorf("[Play] Failed to re-add song for single repeat: %v", err)
			}
		} else {

			logger.Debugf("[Play] Queue repeat, re-adding song to end: %s", repeatSong.Title)
			if err := queue.AddSong(guildID, repeatSong, -1); err != nil {
				logger.Errorf("[Play] Failed to re-add song for queue repeat: %v", err)
			}
		}
	}

	go PreCacheNext(guildID, voiceChannelBitrate)

	return playContinue
}

func GetTrimRange(guildID string) (int, int) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()
	return player.TrimStartMs, player.TrimEndMs
}

func fadeSettingsFromQueue(q *queue.Queue) fadeSettings {
	return fadeSettings{
		fadeIn:       q.FadeIn,
		fadeOut:      q.FadeOut,
		autoMix:      q.AutoMix,
		crossfade:    q.Crossfade || q.AutoMix,
		trimSilence:  q.TrimSilence || q.AutoMix,
		fadeInSec:    q.FadeInDuration,
		fadeOutSec:   q.FadeOutDuration,
		crossfadeSec: q.CrossfadeDuration,
		autoMixBeats: q.AutoMixBeats,
		repeatMode:   q.RepeatMode,
	}
}

func planFadeOutWindow(totalFrames, sentFrames int, fadeOutSec float64) (int, int) {
	remaining := totalFrames - sentFrames
	frames := int(fadeOutSec * framesPerSecond)
	if frames > remaining {
		frames = remaining
	}
	if frames <= 0 {
		return 0, 0
	}
	return totalFrames - frames, frames
}

func advanceQueueForAutoMix(player *GuildPlayer, song *queue.Song, crossfade *crossfadeState, announceNext func(*queue.Song)) {
	guildID := player.GuildID

	song.ResetRetry()
	song.SetState(queue.SongStateCompleted)
	clearRetryCount(guildID, song.URL)

	q, err := queue.GetQueue(guildID, false)
	if err != nil {
		logger.Errorf("[%s] Failed to load queue for advancement: %v", crossfade.tag, err)
	}

	repeatMode := queue.RepeatOff
	if q != nil {
		repeatMode = q.RepeatMode
	}
	var repeatSong *queue.Song
	if repeatMode != queue.RepeatOff && !song.IsLive {
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
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[%s] Failed to remove finished song: %v", crossfade.tag, err)
		return
	}

	if repeatSong != nil {
		if err := queue.AddSong(guildID, repeatSong, -1); err != nil {
			logger.Errorf("[%s] Failed to re-add song for queue repeat: %v", crossfade.tag, err)
		}
	}

	player.mu.Lock()
	player.AutoMixAdvanced = true
	player.PlaybackStart = time.Now().Add(-time.Duration(crossfade.startOffsetSec * float64(time.Second)))
	player.mu.Unlock()

	callOnSongStart(guildID)

	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return
	}
	next := q.Songs[0]
	if next.ID != crossfade.nextSongID {
		return
	}
	next.StartPlayback()
	if announceNext != nil {
		go announceNext(next)
	}
	logger.Debugf("[%s] advanced queue to song ID %d at crossfade start for guild: %s", crossfade.tag, next.ID, guildID)
}

func opusBitrateFor(channelBitrate int) int {
	bitrate := channelBitrate
	if bitrate < 64000 {
		bitrate = 64000
	}
	if bitrate > 510000 {
		bitrate = 510000
	}
	return bitrate
}

func playAudio(player *GuildPlayer, song *queue.Song, streamURL string, seekTime int, volume float64, normalization bool, bitrate int, firstFrameCh chan<- struct{}, fade fadeSettings, announceNext func(*queue.Song)) error {
	logger.Debugf("[playAudio] Entered function for guild: %s", player.GuildID)

	stopCh := player.StopChan

	defer func() {
		select {
		case player.PlaybackDone <- struct{}{}:
			logger.Debugf("[playAudio] Signaled PlaybackDone for guild: %s", player.GuildID)
		default:

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
	player.FadingOut = false
	player.FadingIn = false
	player.TrimStartMs = 0
	player.TrimEndMs = 0
	player.PlaybackStart = time.Now().Add(-time.Duration(seekTime) * time.Millisecond)
	guildID := player.GuildID
	player.mu.Unlock()

	defer func() {
		player.mu.Lock()
		player.FadingOut = false
		player.FadingIn = false
		player.mu.Unlock()
	}()

	song.StartPlayback()

	if err := queue.SetPlaying(guildID, true); err != nil {
		logger.Errorf("[playAudio] Failed to set playing state: %v", err)
	}
	if err := queue.SetLoading(guildID, false); err != nil {
		logger.Errorf("[playAudio] Failed to set loading state: %v", err)
	}

	logger.Debugf("[playAudio] Set playing state for guild: %s", guildID)

	playbackRetriesMu.Lock()
	retries := playbackRetries[retryKey(guildID, song.URL)]
	playbackRetriesMu.Unlock()
	if retries == 0 {
		callOnSongStart(guildID)
		logger.Debugf("[playAudio] Called onSongStart callback for guild: %s", guildID)
	} else {
		logger.Debugf("[playAudio] Skipping onSongStart callback (retry %d) for guild: %s", retries, guildID)
	}

	go PreCacheNext(guildID, bitrate)

	collectTail := (fade.autoMix || fade.trimSilence) && !song.IsLive

	var stream *audioStream
	resumeMode := false
	frameOffset := 0

	player.mu.Lock()
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		if pending.SongID == song.ID && seekTime == 0 {
			stream = pending.Stream
			resumeMode = true
			frameOffset = pending.FramesConsumed
			offset := time.Duration(pending.FramesConsumed)*20*time.Millisecond +
				time.Duration(pending.StartOffsetSec*float64(time.Second))
			player.mu.Lock()
			player.PlaybackStart = time.Now().Add(-offset)
			if fade.trimSilence {
				player.TrimStartMs = int(pending.StartOffsetSec*1000) + pending.LeadingSkipFrames*20
			}
			player.mu.Unlock()
			logger.Debugf("[playAudio] Resuming handed-off stream for guild: %s", guildID)
		} else {
			pending.Stream.stop()
		}
	}

	baseOffsetMs := seekTime
	if resumeMode {
		baseOffsetMs = int(pending.StartOffsetSec * 1000)
	}

	if stream == nil {
		logger.Debugf("[playAudio] Building FFmpeg command for guild: %s", guildID)
		args := []string{
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
		}

		if seekTime > 0 {
			seekSeconds := float64(seekTime) / 1000.0
			args = append(args, "-ss", fmt.Sprintf("%.3f", seekSeconds))
		}

		args = append(args, "-i", streamURL)

		if normalization {
			args = append(args, "-af", "dynaudnorm=framelen=500:gausssize=31:peak=0.95")
		}

		args = append(args,
			"-f", "s16le",
			"-ar", "48000",
			"-ac", "2",
			"pipe:1",
		)

		var startErr error
		stream, startErr = startAudioStream(args, collectTail)
		if startErr != nil {
			return startErr
		}
	}

	logger.Debugf("[playAudio] FFmpeg started, setting voice speaking state for guild: %s", guildID)

	logger.Debugf("[playAudio] About to call Speaking(true) for guild: %s", guildID)
	if player.VoiceConn == nil {
		stream.stop()
		return fmt.Errorf("voice connection is nil")
	}
	player.VoiceConn.Speaking(true)
	logger.Debugf("[playAudio] Speaking(true) completed for guild: %s", guildID)
	defer func() {
		player.mu.Lock()
		handingOff := player.PendingStream != nil
		player.mu.Unlock()
		if !handingOff && player.VoiceConn != nil {
			player.VoiceConn.Speaking(false)
		}
	}()

	var opusEncoder *OpusEncoder
	if resumeMode && pending.Encoder != nil {
		opusEncoder = pending.Encoder
		logger.Debugf("[playAudio] Reusing Opus encoder from handoff for guild: %s", guildID)
	} else {
		logger.Debugf("[playAudio] Creating Opus encoder (%s) for guild: %s", GetEncoderType(), guildID)
		newEncoder, err := NewOpusEncoder(frameRate, channels)
		if err != nil {
			stream.stop()
			return fmt.Errorf("failed to create opus encoder: %w", err)
		}
		opusEncoder = newEncoder

		opusBitrate := opusBitrateFor(bitrate)
		if err := opusEncoder.SetBitrate(opusBitrate); err != nil {
			logger.Warnf("[playAudio] Failed to set opus bitrate: %v", err)
		} else {
			logger.Debugf("[playAudio] Opus bitrate set to %d bps (channel: %d bps) for guild: %s", opusBitrate, bitrate, guildID)
		}
	}

	sentFrames := 0
	firstFrameSignaled := false
	volumeBuf := make([]int16, frameSize*channels)

	player.mu.Lock()
	fadeInNext := player.FadeInNext
	player.FadeInNext = false
	player.mu.Unlock()

	fadeInFrames := 0
	fadeInStartFrame := 0
	if fade.fadeIn && !resumeMode && (seekTime == 0 || fadeInNext) {
		fadeInFrames = int(fade.fadeInSec * framesPerSecond)
	}

	skipLeading := fade.trimSilence && !resumeMode && seekTime == 0
	skippedLeadingFrames := 0

	endPlanned := false
	fadeOutStartFrame := 0
	fadeOutFrames := 0
	fadingOutSet := false
	fadingInSet := false
	heldFadeInGain := 1.0
	replanAllowed := true
	var endStateAdj *streamEndState
	crossfade := newCrossfadeState()

	for {
		select {
		case pcmData, ok := <-stream.pcmChan:
			if !ok {
				if done, err := crossfade.finishOnDrain(player, stopCh, opusEncoder, &sentFrames); done {
					return err
				}
				select {
				case err := <-stream.errChan:
					return err
				default:
					if sentFrames == 0 {
						if resumeMode && stream.endState.Load() != nil {
							return nil
						}
						return fmt.Errorf("playback completed with no audio frames sent")
					}
					return nil
				}
			}

			if !endPlanned {
				if es := stream.endState.Load(); es != nil {
					endPlanned = true
					go PreCacheNext(guildID, bitrate)
					if nq, qErr := queue.GetQueue(guildID, false); qErr == nil && nq != nil {
						fade = fadeSettingsFromQueue(nq)
					}
					if !song.IsLive {
						if fade.trimSilence && es.silentTailFrames > 0 {
							player.mu.Lock()
							player.TrimEndMs = baseOffsetMs + (es.totalFrames-es.silentTailFrames)*20
							player.mu.Unlock()
						}
						if frameOffset > 0 {
							es = &streamEndState{
								totalFrames:      es.totalFrames - frameOffset,
								analysis:         es.analysis,
								tailStartFrame:   es.tailStartFrame - frameOffset,
								silentTailFrames: es.silentTailFrames,
							}
						}
						endStateAdj = es
						planned := crossfade.plan(player, es, sentFrames, fade, normalization)
						if !planned && fade.fadeOut {
							fadeOutStartFrame, fadeOutFrames = planFadeOutWindow(es.totalFrames-es.silentTailFrames, sentFrames, fade.fadeOutSec)
							logger.Debugf("[Play] Fade-out window planned: start frame %d, %d frames (total %d, sent %d) for guild: %s", fadeOutStartFrame, fadeOutFrames, es.totalFrames, sentFrames, guildID)
						}
					}
				}
			} else if endStateAdj != nil && replanAllowed && !crossfade.armed && !crossfade.cancelled &&
				(fade.autoMix || fade.crossfade) &&
				!(fadeOutFrames > 0 && sentFrames >= fadeOutStartFrame) &&
				sentFrames%50 == 0 {
				go PreCacheNext(guildID, bitrate)
				if crossfade.plan(player, endStateAdj, sentFrames, fade, normalization) {
					if fadeOutFrames > 0 {
						logger.Debugf("[Play] Fade-out window cleared, crossfade armed for guild: %s", guildID)
					}
					fadeOutStartFrame = 0
					fadeOutFrames = 0
				}
			}

			if skipLeading {
				if frameSilent(pcmData) {
					sentFrames++
					skippedLeadingFrames++
					continue
				}
				skipLeading = false
				if skippedLeadingFrames > 0 {
					player.mu.Lock()
					player.PlaybackStart = player.PlaybackStart.Add(-time.Duration(skippedLeadingFrames) * 20 * time.Millisecond)
					player.TrimStartMs = skippedLeadingFrames * 20
					player.mu.Unlock()
					fadeInStartFrame = sentFrames
					logger.Debugf("[Play] Skipped %d leading silent frames (%.1fs) for guild: %s", skippedLeadingFrames, float64(skippedLeadingFrames)/framesPerSecond, guildID)
				}
			}

			if fade.trimSilence && endStateAdj != nil && endStateAdj.silentTailFrames > 0 &&
				!crossfade.armed && sentFrames >= endStateAdj.totalFrames-endStateAdj.silentTailFrames {
				logger.Debugf("[Play] Trimming %d trailing silent frames for guild: %s", endStateAdj.silentTailFrames, guildID)
				return nil
			}

			player.mu.Lock()
			volumeFactor := player.Volume
			ramping := player.Ramping
			player.mu.Unlock()

			gain := 1.0
			fadingIn := fadeInFrames > 0 && sentFrames < fadeInStartFrame+fadeInFrames
			if fadingIn {
				if !ramping {
					heldFadeInGain = qsinIn(float64(sentFrames-fadeInStartFrame) / float64(fadeInFrames))
				}
				gain *= heldFadeInGain
			}
			if fadingIn != fadingInSet {
				fadingInSet = fadingIn
				player.mu.Lock()
				player.FadingIn = fadingIn
				player.mu.Unlock()
			}
			if fadeOutFrames > 0 && sentFrames >= fadeOutStartFrame {
				gain *= qsinOut(float64(sentFrames-fadeOutStartFrame) / float64(fadeOutFrames))
				if !fadingOutSet {
					fadingOutSet = true
					player.mu.Lock()
					player.FadingOut = true
					player.mu.Unlock()
					logger.Debugf("[Play] Fade-out started at frame %d for guild: %s", sentFrames, guildID)
				}
			}

			wasActive := crossfade.active
			if done, err := crossfade.consume(player, stopCh, pcmData, volumeFactor, opusEncoder, &sentFrames); done {
				return err
			} else if crossfade.active {
				if !wasActive {
					advanceQueueForAutoMix(player, song, crossfade, announceNext)
				}
				continue
			}

			if crossfade.cancelled && fadeOutFrames == 0 {
				crossfade.cancelled = false
				replanAllowed = false
				if fade.fadeOut {
					if es := stream.endState.Load(); es != nil {
						fadeOutStartFrame, fadeOutFrames = planFadeOutWindow(es.totalFrames-frameOffset-es.silentTailFrames, sentFrames, fade.fadeOutSec)
						logger.Debugf("[Play] Fade-out window planned after cancel: start frame %d, %d frames for guild: %s", fadeOutStartFrame, fadeOutFrames, guildID)
					}
				}
			}

			copy(volumeBuf, pcmData)
			applyGain(volumeBuf, volumeFactor*gain)

			opusBuffer := make([]byte, 1500)
			opusLen, err := opusEncoder.Encode(volumeBuf, opusBuffer)
			if err != nil {
				logger.Errorf("[playAudio] Opus encoding error: %v", err)
				sentFrames++
				continue
			}
			opusData := opusBuffer[:opusLen]

			select {
			case player.VoiceConn.OpusSend <- opusData:
			case <-player.VoiceConn.Dead:
				stream.stop()
				crossfade.abort()
				return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err)
			case <-stopCh:
				stream.stop()
				crossfade.abort()
				return fmt.Errorf("playback stopped by user")
			}

			sentFrames++
			if !firstFrameSignaled {
				firstFrameSignaled = true
				logger.Debugf("[playAudio] First Opus frame sent successfully for guild: %s", guildID)
				select {
				case firstFrameCh <- struct{}{}:
				default:
				}
			}

		case <-player.VoiceConn.Dead:
			stream.stop()
			crossfade.abort()
			return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err)

		case <-stopCh:
			stream.stop()
			crossfade.abort()
			return fmt.Errorf("playback stopped by user")
		}
	}
}

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

func cleanPlaybackErrorMessage(guildID, errMsg string) string {
	errorLower := strings.ToLower(errMsg)
	t := messages.T(guildID)
	errorMappings := map[string]string{
		"private video":                 t.Player.ErrorPrivateVideo,
		"deleted video":                 t.Player.ErrorDeletedVideo,
		"age-restricted":                t.Player.ErrorAgeRestricted,
		"age restricted":                t.Player.ErrorAgeRestricted,
		"not available in your country": t.Player.ErrorGeoRestricted,
		"geo":                           t.Player.ErrorGeoRestricted,
		"members-only":                  t.Player.ErrorMembersOnly,
		"members only":                  t.Player.ErrorMembersOnly,
		"premium":                       t.Player.ErrorPremiumOnly,
		"copyright":                     t.Player.ErrorCopyright,
		"blocked":                       t.Player.ErrorBlocked,
		"removed by the uploader":       t.Player.ErrorRemovedByUploader,
		"account associated with this video has been terminated": t.Player.ErrorAccountTerminated,
	}
	for pattern, message := range errorMappings {
		if strings.Contains(errorLower, pattern) {
			return message
		}
	}

	if strings.Contains(errorLower, "video unavailable") || strings.Contains(errorLower, "not available") {
		return t.Player.ErrorUnavailable
	}
	return t.Player.ErrorUnavailable
}

func sendReconnectMessage(session *discordgo.Session, guildID string, song *queue.Song) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		return
	}

	embed := messages.CreateSongEmbed(
		guildID,
		messages.ColorWarning,
		messages.T(guildID).Player.StreamReconnectingTitle,
		messages.T(guildID).Player.StreamReconnectingDesc,
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

func sendSongErrorMessage(session *discordgo.Session, guildID string, song *queue.Song, reason string) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		logger.Warnf("[Play] Cannot send error message - no text channel for guild: %s", guildID)
		return
	}

	embed := messages.CreateSongEmbed(
		guildID,
		messages.ColorError,
		messages.T(guildID).Player.PlaybackFailedTitle,
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

func retryKey(guildID, songURL string) string {
	return guildID + ":" + songURL
}

func clearRetryCount(guildID, songURL string) {
	playbackRetriesMu.Lock()
	delete(playbackRetries, retryKey(guildID, songURL))
	playbackRetriesMu.Unlock()
}

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

func handlePlaybackError(session *discordgo.Session, guildID string, song *queue.Song, err error) bool {
	errMsg := err.Error()

	if isDefinitivePlaybackError(errMsg) {
		reason := cleanPlaybackErrorMessage(guildID, errMsg)
		logger.Warnf("[Play] Definitive error for song %s in guild %s: %s", song.Title, guildID, reason)
		song.SetState(queue.SongStateFailed)
		sendSongErrorMessage(session, guildID, song, reason)
		clearRetryCount(guildID, song.URL)
		return false
	}

	key := retryKey(guildID, song.URL)
	playbackRetriesMu.Lock()
	retries := playbackRetries[key]
	retries++
	playbackRetries[key] = retries
	playbackRetriesMu.Unlock()

	if retries < maxRetries {
		logger.Warnf("[Play] Retrying song (attempt %d/%d) in guild: %s - %s", retries, maxRetries, guildID, song.Title)
		return true
	}

	song.SetState(queue.SongStateFailed)
	logger.Errorf("[Play] Max retries exceeded for song %s in guild: %s", song.Title, guildID)

	if reconnectMsg := getReconnectMessage(guildID); reconnectMsg != nil {
		failedEmbed := messages.CreateSongEmbed(
			guildID,
			messages.ColorError,
			messages.T(guildID).Player.StreamReconnectFailedTitle,
			messages.T(guildID).Player.StreamReconnectFailedDesc,
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
		sendSongErrorMessage(session, guildID, song, messages.T(guildID).Player.MaxRetriesSkipping)
	}

	clearRetryCount(guildID, song.URL)
	return false
}

func Pause(guildID string) error {
	logger.Debugf("[Pause] Pause called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()

	if !player.Playing {
		player.mu.Unlock()
		return fmt.Errorf("not playing")
	}

	elapsed := time.Since(player.PlaybackStart)
	seekTime := int(elapsed.Milliseconds())

	select {
	case <-player.PlaybackDone:
	default:
	}

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

	select {
	case <-player.PlaybackDone:
		logger.Debugf("[Pause] Playback terminated for guild: %s", guildID)
	case <-time.After(5 * time.Second):
		logger.Warnf("[Pause] Timeout waiting for playback to terminate for guild: %s", guildID)
	}

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		currentSong := q.Songs[0]
		_, err = queue.SaveSeekTime(guildID, currentSong.ID, seekTime)
		if err != nil {
			logger.Errorf("[Pause] Failed to save seek time: %v", err)
		}
	}

	if err := queue.SetPaused(guildID, true); err != nil {
		logger.Errorf("[Pause] Failed to set paused state in database: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[Pause] Failed to clear playing state in database: %v", err)
	}

	player.mu.Lock()
	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
		player.VoiceConn = nil
		player.VoiceChannelID = ""
	}
	player.mu.Unlock()

	logger.Debugf("[Pause] Paused at %dms for guild: %s", seekTime, guildID)
	return nil
}

func pauseInternal(guildID string) error {
	player := GetPlayer(guildID)
	player.mu.Lock()

	if !player.Playing {
		player.mu.Unlock()
		return fmt.Errorf("not playing")
	}

	elapsed := time.Since(player.PlaybackStart)
	seekTime := int(elapsed.Milliseconds())

	select {
	case <-player.StopChan:
		logger.Debugf("[pauseInternal] Stop signal already pending for guild: %s", guildID)
	default:
		close(player.StopChan)
		logger.Debugf("[pauseInternal] Stop signal sent for guild: %s", guildID)
	}
	player.Playing = false
	player.Paused = true

	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
		player.VoiceConn = nil
		player.VoiceChannelID = ""
	}
	player.mu.Unlock()

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		currentSong := q.Songs[0]
		_, err = queue.SaveSeekTime(guildID, currentSong.ID, seekTime)
		if err != nil {
			logger.Errorf("[pauseInternal] Failed to save seek time: %v", err)
		}
	}

	if err := queue.SetPaused(guildID, true); err != nil {
		logger.Errorf("[pauseInternal] Failed to set paused state in database: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("[pauseInternal] Failed to clear playing state in database: %v", err)
	}

	logger.Debugf("[pauseInternal] Paused at %dms for guild: %s", seekTime, guildID)
	return nil
}

func Seek(guildID string, positionMs int) error {
	if positionMs < 0 {
		return fmt.Errorf("seek position cannot be negative")
	}
	player := GetPlayer(guildID)

	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return fmt.Errorf("no current song")
	}
	song := q.Songs[0]
	if song.IsLive {
		return fmt.Errorf("cannot seek a live stream")
	}

	player.mu.Lock()
	if !player.Playing {
		player.mu.Unlock()
		return fmt.Errorf("not playing")
	}
	fadingOut := player.FadingOut
	player.mu.Unlock()
	if fadingOut {
		logger.Debugf("[Seek] Fade-out in progress, letting it finish for guild: %s", guildID)
		return nil
	}

	if fadeOut, err := queue.GetFadeOut(guildID); err == nil && fadeOut {
		seconds, err := queue.GetFadeOutDuration(guildID)
		if err != nil || seconds <= 0 {
			seconds = 1
		}
		rampVolumeDown(guildID, seconds)
	}

	player.mu.Lock()
	if !player.Playing {
		player.mu.Unlock()
		return fmt.Errorf("not playing")
	}
	song.SeekTime = positionMs
	player.Seeking = true
	player.SeekTargetMs = positionMs
	select {
	case <-player.StopChan:

	default:
		close(player.StopChan)
	}
	player.mu.Unlock()

	if _, err := queue.SaveSeekTime(guildID, song.ID, positionMs); err != nil {
		logger.Errorf("[Seek] Failed to persist seek time: %v", err)
		return err
	}
	logger.Debugf("[Seek] Set position to %dms for guild %s", positionMs, guildID)
	return nil
}

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

	default:
		close(player.StopChan)
	}
	logger.Debugf("[RestartForNormalization] Signaled FFmpeg restart for guild: %s", guildID)
}

func Resume(session *discordgo.Session, guildID string) error {
	player := GetPlayer(guildID)

	done := make(chan error, 1)
	cmd := PlayerCommand{
		Type:    "resume",
		Session: session,
		GuildID: guildID,
		Done:    done,
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[Resume] Recovered from panic (channel likely closed) for guild %s: %v", guildID, r)
		}
	}()

	select {
	case player.CommandChan <- cmd:

		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):

			player.mu.Lock()
			playing := player.Playing
			paused := player.Paused
			player.mu.Unlock()

			if playing && !paused {
				logger.Debugf("[Resume] Command completed successfully after timeout for guild %s", guildID)
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

func resumeInternal(session *discordgo.Session, guildID string) error {

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		song := q.Songs[0]

		if song.IsLive {
			active, err := youtube.IsLiveStreamActive(song.URL)
			if err != nil || !active {
				logger.Warnf("[Resume] Live stream ended or unavailable: %s", song.Title)

				if err := queue.RemoveFirstSong(guildID); err != nil {
					logger.Errorf("[Resume] Failed to remove ended live stream: %v", err)
				}

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
	player.FadeInNext = true
	player.mu.Unlock()

	ClearAutoPauseTimer(guildID)

	logger.Debugf("[Resume] Resuming playback for guild: %s", guildID)
	return playInternal(session, guildID)
}

func Skip(session *discordgo.Session, guildID string) error {
	logger.Debugf("[Skip] Skip called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	fadingOut := player.FadingOut
	player.mu.Unlock()
	if fadingOut {
		logger.Debugf("[Skip] Fade-out in progress, letting it finish for guild: %s", guildID)
		return nil
	}

	rampVolumeBeforeStop(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.PlaybackDone:
		default:
		}

		select {
		case <-player.StopChan:
			logger.Debugf("[Skip] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[Skip] Stop signal sent for guild: %s", guildID)
		}
	}
	player.mu.Unlock()

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[Skip] Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("[Skip] Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		pending := player.PendingStream
		player.PendingStream = nil
		player.mu.Unlock()

		if pending != nil {
			pending.Stream.stop()
		}
	}

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		q.Songs[0].ResetRetry()
		logger.Debugf("[Skip] Removing song: %s", q.Songs[0].Title)
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[Skip] Failed to remove song: %v", err)
		return fmt.Errorf("failed to remove song: %w", err)
	}

	logger.Debugf("[Skip] Skipped song for guild: %s", guildID)

	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		logger.Infof("[Skip] Queue is empty after skip for guild: %s", guildID)
		return ErrQueueEmpty
	}

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

			if err.Error() == "play lock timeout" {
				logger.Debugf("[Skip] Play lock timeout for guild %s (expected during rapid skips)", guildID)
			} else {
				logger.Errorf("[Skip] Failed to play next song: %v", err)
			}
		}
	}()

	return nil
}

func SkipTo(session *discordgo.Session, guildID string) error {
	logger.Debugf("[SkipTo] Called for guild %s", guildID)
	rampVolumeBeforeStop(guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.PlaybackDone:
		default:
		}

		select {
		case <-player.StopChan:
			logger.Debugf("[SkipTo] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[SkipTo] Stop signal sent for guild: %s", guildID)
		}
	}
	player.mu.Unlock()

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

	logger.Debugf("[SkipTo] Starting playback of target song for guild: %s", guildID)

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

func skipInternal(session *discordgo.Session, guildID string) error {
	logger.Debugf("[skipInternal] Called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	fadingOut := player.FadingOut
	player.mu.Unlock()
	if fadingOut {
		logger.Debugf("[skipInternal] Fade-out in progress, letting it finish for guild: %s", guildID)
		return nil
	}

	rampVolumeBeforeStop(guildID)
	player.mu.Lock()

	wasPlaying := player.Playing
	if wasPlaying {

		select {
		case <-player.PlaybackDone:
		default:
		}

		select {
		case <-player.StopChan:
			logger.Debugf("[Skip] Stop signal already pending for guild: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[Skip] Stop signal sent for guild: %s", guildID)
		}
	}

	player.mu.Unlock()

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[Skip] Playback terminated for guild: %s", guildID)
		case <-time.After(2 * time.Second):
			logger.Warnf("[Skip] Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		pending := player.PendingStream
		player.PendingStream = nil
		player.mu.Unlock()

		if pending != nil {
			pending.Stream.stop()
		}
	}

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		q.Songs[0].ResetRetry()
		logger.Debugf("[skipInternal] Removing song: %s", q.Songs[0].Title)
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("[skipInternal] Failed to remove song: %v", err)
		return fmt.Errorf("failed to remove song: %w", err)
	}

	logger.Debugf("[skipInternal] Skipped song for guild: %s", guildID)

	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		logger.Debugf("[skipInternal] Queue is empty after skip for guild: %s", guildID)
		return ErrQueueEmpty
	}

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

			if err.Error() == "play lock timeout" {
				logger.Debugf("[skipInternal] Play lock timeout for guild %s (expected during rapid skips)", guildID)
			} else {
				logger.Errorf("[skipInternal] Failed to play next song: %v", err)
			}
		}
	}()

	return nil
}

func Stop(guildID string) error {
	logger.Debugf("[Stop] Stop called for guild %s", guildID)
	rampVolumeBeforeStop(guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.PlaybackDone:
		default:
		}

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

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[Stop] Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("[Stop] Timeout waiting for playback to terminate for guild: %s", guildID)
		}
	}

	if err := LeaveVoice(guildID); err != nil {
		logger.Errorf("[Stop] Failed to leave voice: %v", err)
	}

	if err := queue.DeleteQueue(guildID); err != nil {
		logger.Errorf("[Stop] Failed to delete queue: %v", err)
	}

	ClearPreCache(guildID)

	DeletePlayer(guildID)

	logger.Debugf("[Stop] Stopped playback for guild: %s", guildID)
	return nil
}

func stopInternal(guildID string) error {
	player := GetPlayer(guildID)
	player.mu.Lock()

	if player.Playing {

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
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		pending.Stream.stop()
	}

	if err := LeaveVoice(guildID); err != nil {
		logger.Errorf("[Stop] Failed to leave voice: %v", err)
	}

	if err := queue.DeleteQueue(guildID); err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	ClearPreCache(guildID)

	DeletePlayer(guildID)
	logger.Debugf("[Stop] Stopped playback for guild: %s", guildID)
	return nil
}

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

	logger.Debugf("[Volume] Set volume to %g%% for guild: %s", volume, guildID)
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

func FormatDuration(ms int) string {
	seconds := ms / 1000
	minutes := seconds / 60
	hours := minutes / 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds%60)
}

func int16ToByte(in []int16) []byte {
	out := make([]byte, len(in)*2)
	for i, v := range in {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

func StopAll() {
	playersMu.RLock()
	guildIDs := make([]string, 0, len(players))
	for guildID := range players {
		guildIDs = append(guildIDs, guildID)
	}
	playersMu.RUnlock()

	logger.Debugf("[StopAll] Cleaning up %d player(s)", len(guildIDs))

	var wg sync.WaitGroup
	for _, guildID := range guildIDs {
		wg.Add(1)
		go func(guildID string) {
			defer wg.Done()
			if err := cleanupForShutdown(guildID); err != nil {
				logger.Errorf("[StopAll] Failed to cleanup guild %s: %v", guildID, err)
			}
			ClearPreCache(guildID)
			logger.Debugf("[StopAll] Cleaned up guild: %s", guildID)
		}(guildID)
	}
	wg.Wait()

	preCacheStoreMu.Lock()
	for key, cache := range preCacheStore {
		if cache.CancelFunc != nil {
			cache.CancelFunc()
		}
		delete(preCacheStore, key)
	}
	preCacheStoreMu.Unlock()

	logger.Debug("[StopAll] All players cleaned up and pre-cache cleared")
}

func cleanupForShutdown(guildID string) error {
	logger.Debugf("[CleanupForShutdown] Cleaning up guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.StopChan:

		default:
			close(player.StopChan)
			logger.Debugf("[CleanupForShutdown] Stop signal sent for guild: %s", guildID)
		}
	}

	player.Playing = false
	player.Paused = false
	player.Loading = false
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		pending.Stream.stop()
	}

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("[CleanupForShutdown] Playback terminated for guild: %s", guildID)
		case <-time.After(2 * time.Second):
			logger.Debugf("[CleanupForShutdown] Timeout waiting for playback for guild: %s", guildID)
		}
	}

	if err := LeaveVoice(guildID); err != nil {
		logger.Debugf("[CleanupForShutdown] Failed to leave voice for guild %s: %v", guildID, err)
	}

	if wasPlaying || wasLoading {
		if err := queue.SetPlaying(guildID, false); err != nil {
			logger.Debugf("[CleanupForShutdown] Failed to clear playing state for guild %s: %v", guildID, err)
		}
		if err := queue.SetLoading(guildID, false); err != nil {
			logger.Debugf("[CleanupForShutdown] Failed to clear loading state for guild %s: %v", guildID, err)
		}
	}

	DeletePlayer(guildID)

	logger.Debugf("[CleanupForShutdown] Cleanup complete for guild: %s (queue preserved)", guildID)
	return nil
}

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
			Description: messages.T(guildID).Player.LeavingEmptyDesc,
			Color:       messages.ColorInfo,
			Footer: &discordgo.MessageEmbedFooter{
				Text: messages.T(guildID).Player.LeavingEmptyFooter,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	case "error":
		embed = &discordgo.MessageEmbed{
			Description: messages.T(guildID).Player.LeavingErrorDesc,
			Color:       messages.ColorError,
			Footer: &discordgo.MessageEmbedFooter{
				Text: messages.T(guildID).Player.LeavingErrorFooter,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	default:
		embed = &discordgo.MessageEmbed{
			Description: messages.T(guildID).Player.LeavingDefaultDesc,
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

func ShutdownWorkerPool() {
	worker.CloseGlobalPool()
}
