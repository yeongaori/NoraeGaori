package player

import (
	"context"
	"errors"
	"io"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"

	"github.com/bwmarrin/discordgo"
)

var ErrQueueEmpty = errors.New("queue is empty after skip")

const (
	channels    = dsp.Channels
	frameRate   = dsp.SampleRate
	frameSize   = dsp.FrameSize
	maxRetries  = 3
	lockTimeout = 30 * time.Second

	healthyPlaybackFrames = 500
	framePacingWarnDelay  = 100 * time.Millisecond
)

type PlayerCommand struct {
	Type    string
	Session *discordgo.Session
	GuildID string
	Done    chan error
}

type GuildPlayer struct {
	GuildID          string
	VoiceConn        voiceConnection
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
	dispatch         func(PlayerCommand) error
	PendingStream    *PendingStream
	FadingOut        bool
	FadingIn         bool
	Ramping          bool
	AutoMixAdvanced  bool
	FadeInNext       bool
	SeekTargetMs     int
	TrimStartMs      int
	TrimEndMs        int
	transitionArmed  atomic.Bool
}

type fadeSettings struct {
	fadeIn         bool
	fadeOut        bool
	autoMix        bool
	crossfade      bool
	trimSilence    bool
	fadeInSec      float64
	fadeOutSec     float64
	crossfadeSec   float64
	autoMixBeats   int
	repeatMode     int
	styleOverrides transition.StyleOverrides
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

	announcedSongs   = make(map[string]int)
	announcedSongsMu sync.Mutex

	onSongStartCallback     func(guildID string)
	onPlaybackEndedCallback func(guildID string)
	callbackMu              sync.RWMutex

	resumePlayback = playInternal

	getLiveStreamPipe = func(url string, sponsorBlock bool, bitrate, seekTime int) (io.ReadCloser, error) {
		return youtube.GetStreamPipe(url, sponsorBlock, bitrate, seekTime)
	}

	fetchStreamURL = youtube.GetStreamURL

	joinVoiceChannel func(session *discordgo.Session, guildID, channelID string) (voiceConnection, error)

	announceNowPlaying        func(session *discordgo.Session, guildID string, song *queue.Song, q *queue.Queue)
	announceLeaving           func(session *discordgo.Session, guildID, reason string)
	announceReconnect         func(session *discordgo.Session, guildID string, song *queue.Song)
	dismissLoadingMessage     func(session *discordgo.Session, guildID string)
	lookupVoiceChannelBitrate func(session *discordgo.Session, channelID string) int
	announceSongError         func(session *discordgo.Session, guildID string, song *queue.Song, reason string)
)

func init() {
	joinVoiceChannel = JoinVoice
	announceNowPlaying = sendNowPlayingMessage
	announceLeaving = sendLeavingMessage
	announceReconnect = sendReconnectMessage
	dismissLoadingMessage = deleteLoadingMessageFor
	lookupVoiceChannelBitrate = readVoiceChannelBitrate
	announceSongError = sendSongErrorMessage
}

func readVoiceChannelBitrate(session *discordgo.Session, channelID string) int {
	if channelID == "" {
		return 0
	}

	channel, err := session.Channel(channelID)
	if err != nil || channel == nil {
		logger.Warnf("Could not get voice channel info for bitrate: %v", err)
		return 0
	}

	logger.Debugf("Voice channel bitrate: %d bps (%d kbps)", channel.Bitrate, channel.Bitrate/1000)
	return channel.Bitrate
}

func deleteLoadingMessageFor(session *discordgo.Session, guildID string) {
	lm := GetLoadingMessage(guildID)
	if lm == nil {
		return
	}

	session.ChannelMessageDelete(lm.ChannelID, lm.ID)
	DeleteLoadingMessage(guildID)
}

type PreCache struct {
	StreamURL  string
	SongID     int
	Timestamp  time.Time
	CancelFunc context.CancelFunc
	Analysis   *analysis.TrackAnalysis
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

			logger.Warnf("Processor not running for guild %s, restarting", guildID)

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

	logger.Debugf("Stopped command processor for guild: %s", guildID)
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

func SetOnPlaybackEndedCallback(callback func(guildID string)) {
	callbackMu.Lock()
	defer callbackMu.Unlock()
	onPlaybackEndedCallback = callback
}

func callOnPlaybackEnded(guildID string) {
	callbackMu.RLock()
	callback := onPlaybackEndedCallback
	callbackMu.RUnlock()

	if callback != nil {
		callback(guildID)
	}
}
