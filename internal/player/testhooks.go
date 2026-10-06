//go:build testhooks

package player

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/dependency"
	"noraegaori/internal/queue"
)

const HookAnalysisHeadSecs = analysisHeadSecs
const HookAnalysisBackfillLimit = analysisBackfillLimit
const HookStoredTailMarginSec = storedTailMarginSec
const HookChannels = channels
const HookFallbackSlideFrames = fallbackSlideFrames
const HookFrameRate = frameRate
const HookFrameSize = frameSize
const HookMaxRetries = maxRetries
const HookMinUsableCrossfadeFrames = minUsableCrossfadeFrames
const HookPlayContinue = playContinue
const HookPlayStop = playStop

type HookAudioStream = audioStream
type HookCrossfadeState = crossfadeState
type HookFadeSettings = fadeSettings
type HookFfmpegPin = ffmpegPin
type HookPlayResult = playResult
type HookVoiceConnection = voiceConnection

type HookGuildPlayerFields struct {
	GuildID          string
	Volume           float64
	StopChan         chan struct{}
	PlaybackDone     chan struct{}
	CommandChan      chan PlayerCommand
	QuitChan         chan struct{}
	ProcessorRunning bool
	Dispatch         func(PlayerCommand) error
}

type HookFadeSettingsFields struct {
	FadeIn         bool
	AutoMix        bool
	Crossfade      bool
	TrimSilence    bool
	FadeInSec      float64
	CrossfadeSec   float64
	AutoMixBeats   int
	RepeatMode     int
	StyleOverrides map[string]string
}

var HookAnalysisMaxBytes = analysisReadBytes(analysisHeadSecs)

type HookBackfillItem = backfillItem

var HookAcquireFFmpeg = &acquireFFmpeg
var HookAnalysisBackfillGap = &analysisBackfillGap
var HookAnalysisFailed = analysisFailed
var HookAnalyzeSegment = &analyzeSegment
var HookAnalyzeStreamSegment = analyzeStreamSegment
var HookChooseTailAnalysis = chooseTailAnalysis
var HookClearAnalysisPending = clearAnalysisPending
var HookFetchBackfillStreamURL = &fetchBackfillStreamURL
var HookMarkAnalysisFailed = markAnalysisFailed
var HookMarkAnalysisPending = markAnalysisPending
var HookPlanBackfillWork = planBackfillWork
var HookRunAnalysisBackfillPass = runAnalysisBackfillPass
var HookAdjustEndStateForOffset = adjustEndStateForOffset
var HookAnnounceAutoPause = &announceAutoPause
var HookAnnounceNowPlaying = &announceNowPlaying
var HookAnnouncePlaybackCrash = &announcePlaybackCrash
var HookAnnouncePlaybackEnd = &announcePlaybackEnd
var HookAnnounceRateLimit = &announceRateLimit
var HookAnnounceReconnect = &announceReconnect
var HookAnnounceSongError = &announceSongError
var HookAnnouncedSongs = &announcedSongs
var HookAnnouncedSongsMu = &announcedSongsMu
var HookAutoPauseDelay = &autoPauseDelay
var HookAutoPauseEmbed = autoPauseEmbed
var HookAutoPauseTimers = &autoPauseTimers
var HookAutoPauseTimersMu = &autoPauseTimersMu
var HookAutoPausedChannel = autoPausedChannel
var HookAutoPausedChannels = &autoPausedChannels
var HookAwaitingDiscord = &awaitingDiscord
var HookAwaitingDiscordMu = &awaitingDiscordMu
var HookCancelAutoPauseTimer = cancelAutoPauseTimer
var HookClassifyDrainedStream = classifyDrainedStream
var HookClearAnnounced = clearAnnounced
var HookClearRetryCount = clearRetryCount
var HookDeliverNowPlaying = deliverNowPlaying
var HookDismissLoadingMessage = &dismissLoadingMessage
var HookFadeInGainAt = fadeInGainAt
var HookFadeOutGainAt = fadeOutGainAt
var HookFetchStreamURL = &fetchStreamURL
var HookForgetAutoPause = forgetAutoPause
var HookGetLiveStreamPipe = &getLiveStreamPipe
var HookHandlePlaybackError = handlePlaybackError
var HookIsStreamFetchFailure = isStreamFetchFailure
var HookJoinVoiceChannel = &joinVoiceChannel
var HookLeaveInternal = leaveInternal
var HookLookupVoiceChannelBitrate = &lookupVoiceChannelBitrate
var HookMarkAnnounced = markAnnounced
var HookMarkAwaitingDiscord = markAwaitingDiscord
var HookMarkPlayerLoading = markPlayerLoading
var HookNewAudioStream = &newAudioStream
var HookNewAudioStreamPipe = &newAudioStreamPipe
var HookNewCrossfadeState = newCrossfadeState
var HookNewOutroState = newOutroState
var HookOpenPlaybackStream = openPlaybackStream
var HookPauseForEmptyChannel = pauseForEmptyChannel
var HookPlanOutroWindow = planOutroWindow
var HookPlayAudio = playAudio
var HookPlayCurrentSong = &playCurrentSong
var HookPlayLockWait = &playLockWait
var HookPlayLocks = &playLocks
var HookPlaySingleSong = playSingleSong
var HookPlaybackEndEmbed = playbackEndEmbed
var HookPlaybackRetries = &playbackRetries
var HookPlaybackRetriesMu = &playbackRetriesMu
var HookPlayers = &players
var HookPlayersMu = &playersMu
var HookPostRateLimitNotice = postRateLimitNotice
var HookPreCacheNext = &preCacheNext
var HookPreCacheSong = preCacheSong
var HookPreCacheStore = &preCacheStore
var HookPreCacheStoreMu = &preCacheStoreMu
var HookPrepareVoiceConnection = prepareVoiceConnection
var HookRateLimitCooldown = &rateLimitCooldown
var HookRateLimitNotices = &rateLimitNotices
var HookReadFloat32Samples = readFloat32Samples
var HookRecentCommandWindow = &recentCommandWindow
var HookReconnectNotices = &reconnectNotices
var HookReleaseFFmpeg = &releaseFFmpeg
var HookReleasePlayback = releasePlayback
var HookReportPlaybackFailure = reportPlaybackFailure
var HookReportStreamFailure = &reportStreamFailure
var HookResolveRestartStreamURL = resolveRestartStreamURL
var HookResumeAfterOutage = &resumeAfterOutage
var HookResumeAutoPaused = &resumeAutoPaused
var HookResumePlayback = &resumePlayback
var HookRetryDelay = &retryDelay
var HookRetryKey = retryKey
var HookSendAutoPauseNotification = sendAutoPauseNotification
var HookSettleTailAnalysis = settleTailAnalysis
var HookSendCommandToPlayer = sendCommandToPlayer
var HookSendFrame = sendFrame
var HookSendPlaybackEndMessage = sendPlaybackEndMessage
var HookShouldAutoPause = shouldAutoPause
var HookShouldAutoResume = shouldAutoResume
var HookSnapTransitionToBar = snapTransitionToBar
var HookSnapTransitionToGrid = snapTransitionToGrid
var HookStartPlaybackSession = startPlaybackSession
var HookVoiceRejoinDelay = &voiceRejoinDelay
var HookWaitBeforeRetry = waitBeforeRetry

func HookBuildGuildPlayer(fields HookGuildPlayerFields) *GuildPlayer {
	return &GuildPlayer{
		GuildID:          fields.GuildID,
		Volume:           fields.Volume,
		StopChan:         fields.StopChan,
		PlaybackDone:     fields.PlaybackDone,
		CommandChan:      fields.CommandChan,
		QuitChan:         fields.QuitChan,
		processorRunning: fields.ProcessorRunning,
		dispatch:         fields.Dispatch,
	}
}

func HookBuildFadeSettings(fields HookFadeSettingsFields) *fadeSettings {
	return &fadeSettings{
		fadeIn:         fields.FadeIn,
		autoMix:        fields.AutoMix,
		crossfade:      fields.Crossfade,
		trimSilence:    fields.TrimSilence,
		fadeInSec:      fields.FadeInSec,
		crossfadeSec:   fields.CrossfadeSec,
		autoMixBeats:   fields.AutoMixBeats,
		repeatMode:     fields.RepeatMode,
		styleOverrides: fields.StyleOverrides,
	}
}

func (player *GuildPlayer) HookFfmpeg() *ffmpegPin {
	return &player.ffmpeg
}

func (player *GuildPlayer) HookLastCommand() *string {
	return &player.lastCommand
}

func (player *GuildPlayer) HookLastCommandAt() *time.Time {
	return &player.lastCommandAt
}

func (player *GuildPlayer) HookMu() *sync.Mutex {
	return &player.mu
}

func (player *GuildPlayer) HookProcessorRunning() *bool {
	return &player.processorRunning
}

func (player *GuildPlayer) HookTransitionArmed() *atomic.Bool {
	return &player.transitionArmed
}

func (player *GuildPlayer) HookBeginSession() chan struct{} {
	return player.beginSession()
}

func (player *GuildPlayer) HookCurrentVoice() voiceConnection {
	return player.currentVoice()
}

func (player *GuildPlayer) HookDefaultDispatch(cmd PlayerCommand) error {
	return player.defaultDispatch(cmd)
}

func (player *GuildPlayer) HookEndSession(done chan struct{}) {
	player.endSession(done)
}

func (player *GuildPlayer) HookHaltLocked() {
	player.haltLocked()
}

func (player *GuildPlayer) HookNoteCommand(name string) {
	player.noteCommand(name)
}

func (player *GuildPlayer) HookProcessCommands() {
	player.processCommands()
}

func (player *GuildPlayer) HookRecentCommand() (string, time.Duration) {
	return player.recentCommand()
}

func (player *GuildPlayer) HookSetVoice(conn voiceConnection, channelID string) {
	player.setVoice(conn, channelID)
}

func (player *GuildPlayer) HookStartLiveStream(url string, bitrate int, normalization bool, collectTail bool) (audioStream, error) {
	return player.startLiveStream(url, bitrate, normalization, collectTail)
}

func (player *GuildPlayer) HookStartStream(args []string, collectTail bool) (audioStream, error) {
	return player.startStream(args, collectTail)
}

func (cs *crossfadeState) HookArmed() *bool {
	return &cs.armed
}

func (cs *crossfadeState) HookAutoMix() *bool {
	return &cs.autoMix
}

func (cs *crossfadeState) HookBAborted() *atomic.Bool {
	return &cs.bAborted
}

func (cs *crossfadeState) HookBRefetch() *atomic.Pointer[streamRef] {
	return &cs.bRefetch
}

func (cs *crossfadeState) HookBRefetching() *atomic.Bool {
	return &cs.bRefetching
}

func (cs *crossfadeState) HookBRetried() *bool {
	return &cs.bRetried
}

func (cs *crossfadeState) HookBStream() *audioStream {
	return &cs.bStream
}

func (cs *crossfadeState) HookBeatLoop() **transition.BeatLoop {
	return &cs.beatLoop
}

func (cs *crossfadeState) HookIncomingLoop() **transition.BeatLoop {
	return &cs.bLoop
}

func (cs *crossfadeState) HookNextBFrame() []int16 {
	return cs.nextBFrame()
}

func (cs *crossfadeState) HookBitrate() *int {
	return &cs.bitrate
}

func (cs *crossfadeState) HookCancelled() *bool {
	return &cs.cancelled
}

func (cs *crossfadeState) HookCrossfadeFrames() *int {
	return &cs.crossfadeFrames
}

func (cs *crossfadeState) HookGuildID() *string {
	return &cs.guildID
}

func (cs *crossfadeState) HookMinUsableFrames() *int {
	return &cs.minUsableFrames
}

func (cs *crossfadeState) HookMixBuf() *[]int16 {
	return &cs.mixBuf
}

func (cs *crossfadeState) HookMixedFrames() *int {
	return &cs.mixedFrames
}

func (cs *crossfadeState) HookNextSongID() *int {
	return &cs.nextSongID
}

func (cs *crossfadeState) HookProcessor() **transition.Processor {
	return &cs.processor
}

func (cs *crossfadeState) HookSlideFrames() *int {
	return &cs.slideFrames
}

func HookSessionOriginSec(baseOffsetMs, frameOffset int) float64 {
	return (&playbackSession{baseOffsetMs: baseOffsetMs, frameOffset: frameOffset}).originSec()
}

func (cs *crossfadeState) HookTrimBLead() *bool {
	return &cs.trimBLead
}

func (cs *crossfadeState) HookTotalFrames() *int {
	return &cs.totalFrames
}

func (cs *crossfadeState) HookTransitionFrame() *int {
	return &cs.transitionFrame
}

func (cs *crossfadeState) HookAbort() {
	cs.abort()
}

func (cs *crossfadeState) HookMixAndSend(player *GuildPlayer, conn voiceConnection, stopCh chan struct{}, aFrame []int16, bFrame []int16, volume float64, enc *opus.Encoder) error {
	return cs.mixAndSend(player, conn, stopCh, aFrame, bFrame, volume, enc)
}

func (cs *crossfadeState) HookPlan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, originSec float64, fade *fadeSettings, normalization bool, bitrate int) bool {
	return cs.plan(player, es, sentFrames, originSec, fade, normalization, bitrate)
}

func (cs *crossfadeState) HookSlideTransition(reason string) {
	cs.slideTransition(reason)
}

func (item *backfillItem) HookSong() *queue.Song {
	return item.song
}

func (item *backfillItem) HookSegment() string {
	return item.segment
}

func (item *backfillItem) HookWindow() (float64, int) {
	return item.window()
}

func (pin *ffmpegPin) HookBinary() **dependency.Binary {
	return &pin.binary
}

func (pin *ffmpegPin) HookLiveStreams() *int {
	return &pin.liveStreams
}

func (pin *ffmpegPin) HookClaim() (*dependency.Binary, func()) {
	return pin.claim()
}

func (notices *guildMessages) HookGet(guildID string) *discordgo.Message {
	return notices.get(guildID)
}

func (notices *guildMessages) HookRemove(guildID string) {
	notices.remove(guildID)
}

func (notices *guildMessages) HookSet(guildID string, msg *discordgo.Message) {
	notices.set(guildID, msg)
}

func (os *outroState) HookPlan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, fade *fadeSettings) bool {
	return os.plan(player, es, sentFrames, fade)
}

func (os *outroState) HookCommitted() *bool {
	return &os.committed
}

func (os *outroState) HookProcessor() **transition.Processor {
	return &os.processor
}

func (os *outroState) HookTail() **transition.Tail {
	return &os.tail
}

func (os *outroState) HookFlush(player *GuildPlayer, conn voiceConnection, stopCh chan struct{}, enc *opus.Encoder, sentFrames *int) {
	os.flush(player, conn, stopCh, enc, sentFrames)
}
